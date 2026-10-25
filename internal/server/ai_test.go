package server

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
)

// fakeAI is an OpenAI-compatible server answering by what the prompt asks.
func fakeAI(t *testing.T, calls *atomic.Int64) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var in struct {
			Messages []struct {
				Content string `json:"content"`
			} `json:"messages"`
		}
		_ = json.NewDecoder(r.Body).Decode(&in)
		calls.Add(1)
		prompt := in.Messages[len(in.Messages)-1].Content
		var content string
		switch {
		case strings.Contains(prompt, "Summarize"):
			content = `{"summary":"Ana sent the October invoice.","points":["Due Friday"],"actions":["Pay the invoice by Friday"]}`
		case strings.Contains(prompt, "Find:"):
			content = "```json\n" + `{"tasks":[{"title":"Pay the invoice","due":"2026-10-24"},{"title":""}],"events":[{"title":"Bad","start":"someday"},{"title":"Call","start":"2026-10-22T10:00"}],"expense":{"amount":49.9,"currency":"eur","date":"2026-10-20","merchant":"Shop"}}` + "\n```"
		case strings.Contains(prompt, "three different short replies"):
			content = `{"replies":["Thanks, I will pay on Friday.","<b>Got it</b>","Could you resend it?","A fourth one"]}`
		case strings.Contains(prompt, "Write a reply"):
			content = `{"body_html":"<p>Hi Ana,</p><script>alert(1)</script><img src=x onerror=alert(1)><p onclick=\"x()\">Paid. <a href=\"javascript:alert(1)\">x</a></p>"}`
		case strings.Contains(prompt, "unread emails from people"):
			content = `{"digest":"One invoice waits.","items":[{"message_id":999999,"why":"made up"},{"message_id":MSGID,"why":"pay it"}]}`
		default:
			content = `{"body_html":"plain line one\n\nline two","subject":"<i>Hello</i>"}`
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"choices": []map[string]any{{"message": map[string]string{"content": content}}}})
	}))
	t.Cleanup(srv.Close)
	return srv
}

func (e *testEnv) useAI(t *testing.T, url string) {
	t.Helper()
	ctx := context.Background()
	st, _ := e.s.store.GetSettings(ctx, e.alice.ID)
	st.AIProvider, st.OpenAIKey, st.OpenAIBaseURL, st.OpenAIModel = "openai", "sk-test", url, "test"
	if err := e.s.store.SaveUserSettings(ctx, e.alice.ID, st); err != nil {
		t.Fatal(err)
	}
}

func TestAISummarizeExtractSuggest(t *testing.T) {
	e := newTestServer(t)
	var calls atomic.Int64
	e.useAI(t, fakeAI(t, &calls).URL)
	id := e.msg.ID

	code, out := e.do(t, e.aliceTok, "POST", "/api/v1/ai/summarize", map[string]any{"message_id": id})
	if code != 200 || out["summary"] != "Ana sent the October invoice." || len(out["actions"].([]any)) != 1 {
		t.Fatalf("summarize: %d %v", code, out)
	}
	before := calls.Load()
	if code, _ := e.do(t, e.aliceTok, "POST", "/api/v1/ai/summarize", map[string]any{"message_id": id}); code != 200 || calls.Load() != before {
		t.Fatal("the summary was not cached")
	}

	code, out = e.do(t, e.aliceTok, "POST", "/api/v1/ai/extract", map[string]any{"message_id": id})
	if code != 200 {
		t.Fatalf("extract: %d %v", code, out)
	}
	tasks, events := out["tasks"].([]any), out["events"].([]any)
	exp := out["expense"].(map[string]any)
	if len(tasks) != 1 || len(events) != 1 || events[0].(map[string]any)["start"] != "2026-10-22T10:00" || exp["currency"] != "EUR" || exp["amount"].(float64) != 49.9 {
		t.Fatalf("extract: %v", out)
	}

	code, out = e.do(t, e.aliceTok, "POST", "/api/v1/ai/suggest", map[string]any{"message_id": id})
	replies := out["replies"].([]any)
	if code != 200 || len(replies) != 3 || replies[1] != "Got it" {
		t.Fatalf("suggest: %d %v", code, out)
	}
}

func TestAIReplyIsSanitized(t *testing.T) {
	e := newTestServer(t)
	var calls atomic.Int64
	e.useAI(t, fakeAI(t, &calls).URL)
	code, out := e.do(t, e.aliceTok, "POST", "/api/v1/ai/reply", map[string]any{"message_id": e.msg.ID, "instruction": "say it is paid"})
	if code != 200 {
		t.Fatalf("reply: %d %v", code, out)
	}
	h := out["body_html"].(string)
	for _, bad := range []string{"<script", "onerror", "onclick", "javascript:", "<img"} {
		if strings.Contains(h, bad) {
			t.Fatalf("unsafe HTML kept (%s): %s", bad, h)
		}
	}
	if !strings.Contains(h, "<p>Hi Ana,</p>") {
		t.Fatalf("lost the reply: %s", h)
	}
	code, out = e.do(t, e.aliceTok, "POST", "/api/v1/ai/write", map[string]any{"mode": "write", "prompt": "ask Ana for the receipt"})
	if code != 200 || out["body_html"] != "<p>plain line one</p><p>line two</p>" || out["subject"] != "Hello" {
		t.Fatalf("write: %d %v", code, out)
	}
	if code, _ := e.do(t, e.aliceTok, "POST", "/api/v1/ai/write", map[string]any{"mode": "shorten"}); code != http.StatusBadRequest {
		t.Fatalf("shorten nothing: %d", code)
	}
}

func TestAIDigestKeepsOnlyRealMessages(t *testing.T) {
	e := newTestServer(t)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		content := strings.ReplaceAll(`{"digest":"One invoice waits.","items":[{"message_id":999999,"why":"made up"},{"message_id":MSGID,"why":"pay it"}]}`, "MSGID", idPath(e.msg.ID))
		_ = json.NewEncoder(w).Encode(map[string]any{"choices": []map[string]any{{"message": map[string]string{"content": content}}}})
	}))
	defer srv.Close()
	e.useAI(t, srv.URL)
	// the message is old: nothing to catch up on, and no AI call needed
	code, out := e.do(t, e.aliceTok, "POST", "/api/v1/ai/digest", nil)
	if code != 200 || len(out["items"].([]any)) != 0 {
		t.Fatalf("digest old: %d %v", code, out)
	}
	if _, err := e.s.store.DB.Exec(`UPDATE messages SET date = strftime('%Y-%m-%dT%H:%M:%fZ','now','-1 hour') WHERE id = ?`, e.msg.ID); err != nil {
		t.Fatal(err)
	}
	code, out = e.do(t, e.aliceTok, "POST", "/api/v1/ai/digest", nil)
	items := out["items"].([]any)
	if code != 200 || len(items) != 1 || items[0].(map[string]any)["subject"] != "Your invoice for October" || out["digest"] != "One invoice waits." {
		t.Fatalf("digest: %d %v", code, out)
	}
}

func TestAINotReadyAndCrossUser(t *testing.T) {
	e := newTestServer(t)
	code, out := e.do(t, e.aliceTok, "POST", "/api/v1/ai/summarize", map[string]any{"message_id": e.msg.ID})
	if code != http.StatusConflict || out["error"] == "" {
		t.Fatalf("no AI: %d %v", code, out)
	}
	if code, out := e.do(t, e.aliceTok, "POST", "/api/v1/ai/ask", map[string]any{"question": "anything?"}); code != 200 || out["ok"] != false {
		t.Fatalf("ask without AI: %d %v", code, out)
	}
	var calls atomic.Int64
	srv := fakeAI(t, &calls)
	e.useAI(t, srv.URL)
	for _, p := range []string{"summarize", "reply", "suggest", "extract"} {
		if code, _ := e.do(t, e.bobTok, "POST", "/api/v1/ai/"+p, map[string]any{"message_id": e.msg.ID}); code != http.StatusNotFound && code != http.StatusConflict {
			t.Fatalf("bob %s alice's message: %d", p, code)
		}
	}
	if calls.Load() != 0 {
		t.Fatal("alice's mail went to the AI for bob")
	}
}

func TestAISafeHTML(t *testing.T) {
	if got := aiSafeHTML("a & b\nc\n\nd"); got != "<p>a &amp; b<br>c</p><p>d</p>" {
		t.Fatalf("plain: %q", got)
	}
	if got := aiSafeHTML(`<p>x<iframe src="y"></iframe></p>`); strings.Contains(got, "iframe") {
		t.Fatalf("iframe kept: %q", got)
	}
}
