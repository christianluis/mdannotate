// Package annotate liest und schreibt Markdown-Dateien, in denen geaenderte
// Passagen mit Annotationsmarken umschlossen sind:
//
//	----Start Annottation [user] [2026-08-22T12:34:56+02:00]
//	... geaenderter Text ...
//	----End Annottation [user] [2026-08-22T12:34:56+02:00]
//
// Der Editor arbeitet nie auf diesen Marken. Er bekommt den "sauberen" Text
// ohne Marken plus eine Liste von Regionen (Zeilenbereiche) und schickt beim
// Speichern wieder nur sauberen Text zurueck. Diese Datei baut daraus die
// Datei auf der Platte neu auf.
package annotate

import (
	"fmt"
	"regexp"
	"sort"
	"strings"
	"time"
)

// Keyword ist bewusst so geschrieben wie in der Spezifikation vorgegeben.
const (
	Keyword   = "Annottation"
	StartWord = "Start"
	EndWord   = "End"
	Dashes    = "----"
	TimeLayot = time.RFC3339
)

var (
	startRe = regexp.MustCompile(`^` + Dashes + ` *` + StartWord + ` +` + Keyword + ` *\[(.*?)\] *\[(.*?)\] *$`)
	endRe   = regexp.MustCompile(`^` + Dashes + ` *` + EndWord + ` +` + Keyword + ` *\[(.*?)\] *\[(.*?)\] *$`)
	fenceRe = regexp.MustCompile("^[ \t]{0,3}(`{3,}|~{3,})")
)

// Region ist ein halboffener Zeilenbereich [Start, End) im sauberen Text.
// Start == End markiert eine Stelle, an der etwas geloescht wurde.
type Region struct {
	Start int    `json:"start"`
	End   int    `json:"end"`
	User  string `json:"user"`
	Time  string `json:"time"`
}

func (r Region) empty() bool { return r.Start >= r.End }

// Doc ist eine Markdown-Datei ohne Marken plus die Regionen.
type Doc struct {
	Lines   []string `json:"-"`
	Regions []Region `json:"regions"`
	CRLF    bool     `json:"-"`
	FinalNL bool     `json:"-"`
}

// Text liefert den sauberen Markdown-Text ohne Annotationsmarken.
func (d Doc) Text() string {
	return join(d.Lines, d.CRLF, d.FinalNL)
}

// Parse zerlegt den Dateiinhalt in sauberen Text und Regionen.
func Parse(raw string) Doc {
	d := Doc{CRLF: strings.Contains(raw, "\r\n")}
	body := strings.ReplaceAll(raw, "\r\n", "\n")
	d.FinalNL = body == "" || strings.HasSuffix(body, "\n")
	body = strings.TrimSuffix(body, "\n")

	var src []string
	if body != "" {
		src = strings.Split(body, "\n")
	}

	type open struct {
		start int
		user  string
		ts    string
	}
	var stack []open

	fence := "" // offener Codezaun; darin ist eine Marke nur Beispieltext

	for _, line := range src {
		if fence != "" {
			if m := fenceRe.FindStringSubmatch(line); m != nil &&
				m[1][0] == fence[0] && len(m[1]) >= len(fence) {
				fence = ""
			}
			d.Lines = append(d.Lines, line)
			continue
		}
		if m := fenceRe.FindStringSubmatch(line); m != nil {
			fence = m[1]
			d.Lines = append(d.Lines, line)
			continue
		}
		if m := startRe.FindStringSubmatch(line); m != nil {
			stack = append(stack, open{len(d.Lines), m[1], m[2]})
			continue
		}
		if endRe.MatchString(line) && len(stack) > 0 {
			o := stack[len(stack)-1]
			stack = stack[:len(stack)-1]
			d.Regions = append(d.Regions, Region{o.start, len(d.Lines), o.user, o.ts})
			continue
		}
		d.Lines = append(d.Lines, line)
	}
	// Nicht geschlossene Marken laufen bis zum Dateiende.
	for _, o := range stack {
		d.Regions = append(d.Regions, Region{o.start, len(d.Lines), o.user, o.ts})
	}
	d.Regions = normalize(d.Regions)
	return d
}

// Raw baut den Dateiinhalt inklusive Marken wieder auf.
func (d Doc) Raw() string {
	regions := normalize(d.Regions)
	n := len(d.Lines)
	out := make([]string, 0, n+2*len(regions))

	byStart := map[int][]Region{}
	byEnd := map[int][]Region{}
	for _, r := range regions {
		if r.empty() {
			byStart[r.Start] = append(byStart[r.Start], r)
			continue
		}
		byStart[r.Start] = append(byStart[r.Start], r)
		byEnd[r.End] = append(byEnd[r.End], r)
	}

	for i := 0; i <= n; i++ {
		for _, r := range byEnd[i] {
			out = append(out, EndLine(r.User, r.Time))
		}
		for _, r := range byStart[i] {
			out = append(out, StartLine(r.User, r.Time))
			if r.empty() {
				out = append(out, EndLine(r.User, r.Time))
			}
		}
		if i < n {
			out = append(out, d.Lines[i])
		}
	}
	return join(out, d.CRLF, d.FinalNL)
}

// StartLine und EndLine bauen die Markenzeilen.
func StartLine(user, ts string) string {
	return fmt.Sprintf("%s%s %s [%s] [%s]", Dashes, StartWord, Keyword, user, ts)
}

func EndLine(user, ts string) string {
	return fmt.Sprintf("%s%s %s [%s] [%s]", Dashes, EndWord, Keyword, user, ts)
}

// IsMarker sagt, ob eine Zeile eine Annotationsmarke ist.
func IsMarker(line string) bool {
	return startRe.MatchString(line) || endRe.MatchString(line)
}

// Apply nimmt den alten Dateiinhalt und den neuen sauberen Text, umschliesst
// jede geaenderte Passage mit Marken und liefert den neuen Dateiinhalt.
func Apply(oldRaw, newClean, user string, now time.Time) Doc {
	return rewrite(oldRaw, newClean, user, now, true)
}

// ApplyPlain schreibt den neuen Text, ohne die Aenderung zu markieren.
// Bereits vorhandene Marken bleiben erhalten und wandern mit ihren Zeilen mit.
func ApplyPlain(oldRaw, newClean string) Doc {
	return rewrite(oldRaw, newClean, "", time.Time{}, false)
}

func rewrite(oldRaw, newClean, user string, now time.Time, mark bool) Doc {
	old := Parse(oldRaw)
	next := Doc{CRLF: old.CRLF, FinalNL: true}

	body := strings.ReplaceAll(newClean, "\r\n", "\n")
	body = strings.TrimSuffix(body, "\n")
	if body != "" {
		next.Lines = strings.Split(body, "\n")
	}

	ops := diffLines(old.Lines, next.Lines)

	// Bestehende Regionen auf die neuen Zeilennummern umrechnen.
	for _, r := range old.Regions {
		s, e := mapRange(ops, r.Start, r.End, len(next.Lines))
		if r.empty() || s < e {
			next.Regions = append(next.Regions, Region{s, e, r.User, r.Time})
		}
		// War die Region vorher nicht leer und ist jetzt leer, wurde ihr
		// Inhalt geloescht. Die Loeschung selbst erzeugt gleich eine
		// eigene Marke, also faellt die alte weg.
	}

	// Neue Regionen fuer jede geaenderte Passage.
	if mark {
		ts := now.Format(TimeLayot)
		for _, h := range hunks(ops, old.Lines, next.Lines) {
			next.Regions = append(next.Regions, Region{h.start, h.end, user, ts})
		}
	}

	next.Regions = normalize(next.Regions)
	return next
}

// hunk ist ein zusammenhaengender geaenderter Bereich in neuen Koordinaten.
type hunk struct{ start, end int }

func hunks(ops []op, oldLines, newLines []string) []hunk {
	var out []hunk
	i := 0
	for i < len(ops) {
		if ops[i].kind == opEqual {
			i++
			continue
		}
		j := i
		insStart, insEnd := -1, -1
		delText := false
		at := ops[i].b
		for j < len(ops) && ops[j].kind != opEqual {
			if ops[j].kind == opIns {
				if insStart < 0 {
					insStart = ops[j].b
				}
				insEnd = ops[j].b + 1
			} else if strings.TrimSpace(oldLines[ops[j].a]) != "" {
				delText = true
			}
			j++
		}
		switch {
		case insStart >= 0:
			// Leerzeilen an den Raendern gehoeren nicht in die Marke, sonst
			// steht der Text nicht zwischen den Marken, sondern daneben.
			for insStart < insEnd && strings.TrimSpace(newLines[insStart]) == "" {
				insStart++
			}
			for insEnd > insStart && strings.TrimSpace(newLines[insEnd-1]) == "" {
				insEnd--
			}
			if insStart < insEnd {
				out = append(out, hunk{insStart, insEnd})
			} else if delText {
				out = append(out, hunk{at, at})
			}
		case delText:
			// Reine Loeschung: Nullbreite-Marke an der Fundstelle.
			out = append(out, hunk{at, at})
		}
		i = j
	}
	return out
}

// mapRange rechnet einen Zeilenbereich aus alten in neue Koordinaten um.
func mapRange(ops []op, s, e, newLen int) (int, int) {
	ns, ne := -1, -1
	for _, o := range ops {
		if o.kind != opEqual {
			continue
		}
		if ns < 0 && o.a >= s {
			ns = o.b
		}
		if o.a < e {
			ne = o.b + 1
		}
	}
	if ns < 0 {
		ns = newLen
	}
	if ne < ns {
		ne = ns
	}
	if ns > newLen {
		ns = newLen
	}
	if ne > newLen {
		ne = newLen
	}
	return ns, ne
}

// normalize sortiert die Regionen und verschmilzt ueberlappende oder direkt
// aneinandergrenzende zu einer. Ergebnis: disjunkt, sortiert, nie verschachtelt.
// Beim Verschmelzen gewinnt der juengste Zeitstempel.
func normalize(in []Region) []Region {
	if len(in) == 0 {
		return nil
	}
	rs := make([]Region, len(in))
	copy(rs, in)
	sort.SliceStable(rs, func(i, j int) bool {
		if rs[i].Start != rs[j].Start {
			return rs[i].Start < rs[j].Start
		}
		return rs[i].End > rs[j].End
	})

	out := []Region{rs[0]}
	for _, r := range rs[1:] {
		last := &out[len(out)-1]
		if r.Start <= last.End {
			if r.End > last.End {
				last.End = r.End
			}
			if newer(r.Time, last.Time) {
				last.User, last.Time = r.User, r.Time
			}
			continue
		}
		out = append(out, r)
	}
	return out
}

func newer(a, b string) bool {
	ta, ea := time.Parse(TimeLayot, a)
	tb, eb := time.Parse(TimeLayot, b)
	if ea == nil && eb == nil {
		return ta.After(tb)
	}
	return a > b
}

func join(lines []string, crlf, finalNL bool) string {
	eol := "\n"
	if crlf {
		eol = "\r\n"
	}
	s := strings.Join(lines, eol)
	if finalNL && s != "" {
		s += eol
	}
	return s
}
