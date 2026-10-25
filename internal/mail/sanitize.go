package mail

import (
	"bytes"
	"regexp"
	"strings"
	"sync"
	"unicode/utf8"

	"github.com/microcosm-cc/bluemonday"
	"golang.org/x/net/html"
	"golang.org/x/net/html/atom"
)

var (
	policyOnce sync.Once
	policy     *bluemonday.Policy
)

// cssProps are the inline styles kept: enough for email layouts, nothing
// that positions content over the app or loads anything.
var cssProps = []string{
	"color", "background-color", "font", "font-family", "font-size", "font-style", "font-weight", "font-variant",
	"text-align", "text-decoration", "text-indent", "text-transform", "line-height", "letter-spacing", "word-spacing",
	"white-space", "word-break", "word-wrap", "overflow-wrap", "vertical-align", "direction",
	"margin", "margin-top", "margin-right", "margin-bottom", "margin-left",
	"padding", "padding-top", "padding-right", "padding-bottom", "padding-left",
	"border", "border-top", "border-right", "border-bottom", "border-left", "border-color", "border-style", "border-width",
	"border-top-color", "border-bottom-color", "border-left-color", "border-right-color",
	"border-top-width", "border-bottom-width", "border-left-width", "border-right-width",
	"border-top-style", "border-bottom-style", "border-left-style", "border-right-style",
	"border-radius", "border-collapse", "border-spacing",
	"width", "height", "max-width", "min-width", "max-height", "min-height",
	"display", "float", "clear", "table-layout", "list-style", "list-style-type", "list-style-position",
	"opacity", "box-sizing", "overflow", "text-overflow", "visibility",
}

func mailPolicy() *bluemonday.Policy {
	policyOnce.Do(func() {
		p := bluemonday.UGCPolicy()
		p.AllowElements("table", "thead", "tbody", "tfoot", "tr", "td", "th", "caption", "col", "colgroup",
			"center", "font", "span", "div", "p", "br", "hr", "img", "a", "b", "i", "u", "s", "strike", "strong", "em",
			"small", "big", "sub", "sup", "h1", "h2", "h3", "h4", "h5", "h6", "ul", "ol", "li", "dl", "dt", "dd",
			"blockquote", "pre", "code", "section", "article", "header", "footer", "main", "figure", "figcaption", "mark", "abbr", "address")
		p.AllowAttrs("align", "valign", "width", "height", "bgcolor", "border", "cellpadding", "cellspacing",
			"colspan", "rowspan", "dir", "nowrap", "title", "lang").Globally()
		p.AllowAttrs("color", "face", "size").OnElements("font")
		p.AllowAttrs("src", "alt", "width", "height").OnElements("img")
		p.AllowAttrs("class").Globally()
		p.AllowStyles(cssProps...).Globally()
		p.AllowURLSchemes("http", "https", "mailto", "tel", "cid")
		p.AllowDataURIImages()
		p.RequireParseableURLs(true)
		p.AllowRelativeURLs(false)
		p.RequireNoFollowOnLinks(false)
		p.RequireNoReferrerOnLinks(true)
		p.AddTargetBlankToFullyQualifiedLinks(true)
		policy = p
	})
	return policy
}

var (
	styleBlockRe = regexp.MustCompile(`(?is)<style[^>]*>(.*?)</style>`)
	cssImportRe  = regexp.MustCompile(`(?i)@import[^;]*;?`)
	cssURLRe     = regexp.MustCompile(`(?i)url\(\s*['"]?\s*(https?:)?//[^)]*\)`)
	cssBadRe     = regexp.MustCompile(`(?i)(expression\s*\(|javascript:|behavior\s*:|-moz-binding|</)`)
	cssPosRe     = regexp.MustCompile(`(?i)position\s*:\s*(fixed|sticky)`)
)

// cleanCSS keeps a message's own stylesheet without anything that loads or runs.
func cleanCSS(css string, allowRemote bool) (string, int) {
	n := 0
	css = cssImportRe.ReplaceAllString(css, "")
	css = cssBadRe.ReplaceAllString(css, "")
	css = cssPosRe.ReplaceAllString(css, "position:static")
	if !allowRemote {
		css = cssURLRe.ReplaceAllStringFunc(css, func(string) string { n++; return "none" })
	}
	return css, n
}

// Sanitize makes a message's HTML safe to show: no scripts, no forms, no
// event handlers, links open in a new tab, remote images blocked unless
// allowed (the second value counts the blocked ones), cid: images resolved.
func Sanitize(src string, o SanitizeOptions) (string, int) {
	if strings.TrimSpace(src) == "" {
		return "", 0
	}
	blocked := 0
	var styles []string
	for _, m := range styleBlockRe.FindAllStringSubmatch(src, 20) {
		css, n := cleanCSS(m[1], o.AllowRemote)
		blocked += n
		if strings.TrimSpace(css) != "" {
			styles = append(styles, css)
		}
	}
	src = styleBlockRe.ReplaceAllString(src, "")
	clean := mailPolicy().Sanitize(src)

	nodes, err := html.ParseFragment(strings.NewReader(clean), &html.Node{Type: html.ElementNode, Data: "div", DataAtom: atom.Div})
	if err != nil {
		return clean, blocked
	}
	var walk func(n *html.Node)
	walk = func(n *html.Node) {
		if n.Type == html.ElementNode {
			switch n.DataAtom {
			case atom.Img:
				for i := 0; i < len(n.Attr); i++ {
					a := &n.Attr[i]
					if a.Key != "src" {
						continue
					}
					v := strings.TrimSpace(a.Val)
					low := strings.ToLower(v)
					switch {
					case strings.HasPrefix(low, "cid:"):
						cid := strings.Trim(v[4:], "<> ")
						if u, ok := o.Inline[cid]; ok && u != "" {
							a.Val = u
						} else {
							a.Val = ""
						}
					case strings.HasPrefix(low, "http:"), strings.HasPrefix(low, "https:"), strings.HasPrefix(low, "//"):
						if !o.AllowRemote {
							blocked++
							a.Key, a.Val = "data-blocked-src", ""
							addClass(n, "pm-blocked")
						}
					}
				}
			case atom.A:
				hasHref := false
				for _, a := range n.Attr {
					if a.Key == "href" {
						hasHref = true
					}
				}
				if hasHref {
					setAttr(n, "target", "_blank")
					setAttr(n, "rel", "noopener noreferrer")
				}
			}
			for i := range n.Attr {
				if n.Attr[i].Key == "style" && !o.AllowRemote && cssURLRe.MatchString(n.Attr[i].Val) {
					n.Attr[i].Val = cssURLRe.ReplaceAllString(n.Attr[i].Val, "none")
					blocked++
				}
			}
		}
		for c := n.FirstChild; c != nil; c = c.NextSibling {
			walk(c)
		}
	}
	var buf bytes.Buffer
	if len(styles) > 0 {
		buf.WriteString("<style>")
		buf.WriteString(strings.Join(styles, "\n"))
		buf.WriteString("</style>")
	}
	for _, n := range nodes {
		walk(n)
		_ = html.Render(&buf, n)
	}
	return buf.String(), blocked
}

func setAttr(n *html.Node, key, val string) {
	for i := range n.Attr {
		if n.Attr[i].Key == key {
			n.Attr[i].Val = val
			return
		}
	}
	n.Attr = append(n.Attr, html.Attribute{Key: key, Val: val})
}

func addClass(n *html.Node, class string) {
	for i := range n.Attr {
		if n.Attr[i].Key == "class" {
			n.Attr[i].Val = strings.TrimSpace(n.Attr[i].Val + " " + class)
			return
		}
	}
	n.Attr = append(n.Attr, html.Attribute{Key: "class", Val: class})
}

var blockTags = map[atom.Atom]bool{
	atom.P: true, atom.Div: true, atom.Table: true, atom.Tr: true, atom.Ul: true, atom.Ol: true, atom.Li: true,
	atom.H1: true, atom.H2: true, atom.H3: true, atom.H4: true, atom.H5: true, atom.H6: true, atom.Blockquote: true,
	atom.Pre: true, atom.Section: true, atom.Article: true, atom.Header: true, atom.Footer: true, atom.Hr: true,
	atom.Dl: true, atom.Dt: true, atom.Dd: true, atom.Center: true, atom.Address: true, atom.Figure: true,
}

var (
	spaceRe = regexp.MustCompile(`[ \t\x{00a0}\x{200b}\x{200c}\x{200d}\x{feff}]+`)
	nlRe    = regexp.MustCompile(`\n[ \t]+`)
	manyNL  = regexp.MustCompile(`\n{3,}`)
)

// HTMLToText is the readable text of an HTML body (search, snippets, AI).
func HTMLToText(src string) string {
	if strings.TrimSpace(src) == "" {
		return ""
	}
	doc, err := html.Parse(strings.NewReader(src))
	if err != nil {
		return src
	}
	var b strings.Builder
	var walk func(n *html.Node)
	walk = func(n *html.Node) {
		switch n.Type {
		case html.TextNode:
			b.WriteString(spaceRe.ReplaceAllString(strings.ReplaceAll(n.Data, "\n", " "), " "))
			return
		case html.ElementNode:
			switch n.DataAtom {
			case atom.Script, atom.Style, atom.Head, atom.Title, atom.Noscript:
				return
			case atom.Br:
				b.WriteString("\n")
				return
			case atom.Li:
				b.WriteString("\n- ")
			case atom.Td, atom.Th:
				b.WriteString(" ")
			}
			if blockTags[n.DataAtom] && n.DataAtom != atom.Li {
				b.WriteString("\n")
			}
		}
		for c := n.FirstChild; c != nil; c = c.NextSibling {
			walk(c)
		}
		if n.Type == html.ElementNode {
			if n.DataAtom == atom.A {
				href := ""
				for _, a := range n.Attr {
					if a.Key == "href" {
						href = strings.TrimSpace(a.Val)
					}
				}
				text := strings.TrimSpace(textOf(n))
				if (strings.HasPrefix(href, "http://") || strings.HasPrefix(href, "https://")) && text != "" && !strings.Contains(text, href) && len(href) < 300 {
					b.WriteString(" (" + href + ")")
				}
			}
			if blockTags[n.DataAtom] {
				b.WriteString("\n")
			}
		}
	}
	walk(doc)
	out := nlRe.ReplaceAllString(b.String(), "\n")
	out = manyNL.ReplaceAllString(out, "\n\n")
	lines := strings.Split(out, "\n")
	for i, l := range lines {
		lines[i] = strings.TrimSpace(l)
	}
	return strings.TrimSpace(manyNL.ReplaceAllString(strings.Join(lines, "\n"), "\n\n"))
}

func textOf(n *html.Node) string {
	if n.Type == html.TextNode {
		return n.Data
	}
	var b strings.Builder
	for c := n.FirstChild; c != nil; c = c.NextSibling {
		b.WriteString(textOf(c))
	}
	return b.String()
}

var wroteRe = regexp.MustCompile(`(?i)^(on .{4,120} wrote:|-{2,} ?original message ?-{2,}|am .{4,80} schrieb .*:|le .{4,80} a écrit ?:)$`)

// Snippet is the first ~200 characters of a text, on one line.
func Snippet(text string) string {
	var parts []string
	for _, l := range strings.Split(text, "\n") {
		l = strings.TrimSpace(l)
		if l == "" || strings.HasPrefix(l, ">") {
			continue
		}
		if wroteRe.MatchString(l) {
			break
		}
		parts = append(parts, l)
		if len(strings.Join(parts, " ")) > 400 {
			break
		}
	}
	s := strings.TrimSpace(spaceRe.ReplaceAllString(strings.Join(parts, " "), " "))
	if utf8.RuneCountInString(s) > 200 {
		r := []rune(s)
		s = strings.TrimSpace(string(r[:200])) + "…"
	}
	return s
}
