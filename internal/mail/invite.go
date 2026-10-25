package mail

import (
	"bufio"
	"bytes"
	"errors"
	"regexp"
	"strings"
	"time"
	_ "time/tzdata" // invitation time zones work on any image
)

type icsProp struct {
	name   string
	params map[string]string
	value  string
}

// unfoldICS joins folded lines (a line starting with a space or tab continues the previous one).
func unfoldICS(b []byte) []string {
	var lines []string
	sc := bufio.NewScanner(bytes.NewReader(b))
	sc.Buffer(make([]byte, 64<<10), 4<<20)
	for sc.Scan() {
		l := strings.TrimRight(sc.Text(), "\r")
		if (strings.HasPrefix(l, " ") || strings.HasPrefix(l, "\t")) && len(lines) > 0 {
			lines[len(lines)-1] += l[1:]
			continue
		}
		lines = append(lines, l)
	}
	return lines
}

func parseICSLine(l string) (icsProp, bool) {
	// NAME;PARAM=VALUE;PARAM="QUOTED:VALUE":VALUE — the first ':' outside quotes
	inQ := false
	cut := -1
	for i, r := range l {
		if r == '"' {
			inQ = !inQ
		}
		if r == ':' && !inQ {
			cut = i
			break
		}
	}
	if cut < 0 {
		return icsProp{}, false
	}
	head, val := l[:cut], l[cut+1:]
	parts := strings.Split(head, ";")
	p := icsProp{name: strings.ToUpper(parts[0]), params: map[string]string{}, value: val}
	for _, pp := range parts[1:] {
		if k, v, ok := strings.Cut(pp, "="); ok {
			p.params[strings.ToUpper(k)] = strings.Trim(v, `"`)
		}
	}
	return p, true
}

func unescapeICS(s string) string {
	r := strings.NewReplacer(`\n`, "\n", `\N`, "\n", `\,`, ",", `\;`, ";", `\\`, `\`)
	return r.Replace(s)
}

// icsTime reads a DTSTART/DTEND value; allDay for VALUE=DATE.
func icsTime(p icsProp) (time.Time, bool, error) {
	v := strings.TrimSpace(p.value)
	if p.params["VALUE"] == "DATE" || (len(v) == 8 && !strings.Contains(v, "T")) {
		t, err := time.ParseInLocation("20060102", v, time.UTC)
		return t, true, err
	}
	if strings.HasSuffix(v, "Z") {
		t, err := time.Parse("20060102T150405Z", v)
		return t, false, err
	}
	loc := time.Local
	if tz := p.params["TZID"]; tz != "" {
		if l, err := time.LoadLocation(windowsZone(tz)); err == nil {
			loc = l
		}
	}
	t, err := time.ParseInLocation("20060102T150405", v, loc)
	if err != nil {
		t, err = time.ParseInLocation("20060102T1504", v, loc)
	}
	return t, false, err
}

// windowsZone maps the most common Outlook (Windows) time zone names.
func windowsZone(tz string) string {
	switch tz {
	case "Central Europe Standard Time", "Central European Standard Time", "W. Europe Standard Time", "Romance Standard Time":
		return "Europe/Belgrade"
	case "GMT Standard Time":
		return "Europe/London"
	case "Eastern Standard Time":
		return "America/New_York"
	case "Pacific Standard Time":
		return "America/Los_Angeles"
	case "Central Standard Time":
		return "America/Chicago"
	case "E. Europe Standard Time", "GTB Standard Time", "FLE Standard Time":
		return "Europe/Bucharest"
	case "UTC", "Coordinated Universal Time":
		return "UTC"
	}
	return tz
}

var durRe = regexp.MustCompile(`^([+-])?P(?:(\d+)W)?(?:(\d+)D)?(?:T(?:(\d+)H)?(?:(\d+)M)?(?:(\d+)S)?)?$`)

func icsDuration(s string) time.Duration {
	m := durRe.FindStringSubmatch(strings.TrimSpace(s))
	if m == nil {
		return 0
	}
	n := func(i int) time.Duration {
		var v int
		for _, c := range m[i] {
			v = v*10 + int(c-'0')
		}
		return time.Duration(v)
	}
	d := n(2)*7*24*time.Hour + n(3)*24*time.Hour + n(4)*time.Hour + n(5)*time.Minute + n(6)*time.Second
	if m[1] == "-" {
		d = -d
	}
	return d
}

func mailtoAddr(v string) string {
	v = strings.TrimSpace(v)
	if strings.HasPrefix(strings.ToLower(v), "mailto:") {
		v = v[7:]
	}
	return v
}

// ParseInvite reads the first event of a text/calendar part. Start and End
// are RFC 3339 times, or plain dates ("2026-10-30", End exclusive) for all-day events.
func ParseInvite(ics []byte) (*Invite, error) {
	lines := unfoldICS(ics)
	inv := &Invite{ICS: string(ics), Attendees: []string{}}
	inEvent, done := false, false
	var start, end time.Time
	var allDay, haveEnd bool
	var dur time.Duration
	for _, l := range lines {
		p, ok := parseICSLine(l)
		if !ok {
			continue
		}
		switch {
		case p.name == "METHOD" && !inEvent:
			inv.Method = strings.ToUpper(strings.TrimSpace(p.value))
		case p.name == "BEGIN" && strings.EqualFold(p.value, "VEVENT"):
			if done {
				continue
			}
			inEvent = true
		case p.name == "END" && strings.EqualFold(p.value, "VEVENT"):
			if inEvent {
				inEvent, done = false, true
			}
		case !inEvent:
			continue
		case p.name == "UID":
			inv.UID = strings.TrimSpace(p.value)
		case p.name == "SUMMARY":
			inv.Summary = unescapeICS(p.value)
		case p.name == "LOCATION":
			inv.Location = unescapeICS(p.value)
		case p.name == "DTSTART":
			t, ad, err := icsTime(p)
			if err == nil {
				start, allDay = t, ad
			}
		case p.name == "DTEND":
			if t, _, err := icsTime(p); err == nil {
				end, haveEnd = t, true
			}
		case p.name == "DURATION":
			dur = icsDuration(p.value)
		case p.name == "ORGANIZER":
			inv.Organizer = Address{Name: p.params["CN"], Addr: mailtoAddr(p.value)}
		case p.name == "ATTENDEE":
			if a := mailtoAddr(p.value); a != "" {
				inv.Attendees = append(inv.Attendees, a)
			}
		}
	}
	if start.IsZero() {
		return nil, errors.New("the invitation has no event")
	}
	if !haveEnd {
		switch {
		case dur > 0:
			end = start.Add(dur)
		case allDay:
			end = start.AddDate(0, 0, 1)
		default:
			end = start.Add(time.Hour)
		}
	}
	inv.AllDay = allDay
	if allDay {
		inv.Start, inv.End = start.Format("2006-01-02"), end.Format("2006-01-02")
	} else {
		inv.Start, inv.End = start.Format(time.RFC3339), end.Format(time.RFC3339)
	}
	if inv.Method == "" {
		inv.Method = "PUBLISH"
	}
	return inv, nil
}
