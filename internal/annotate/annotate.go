// Package annotate liest und schreibt Markdown-Dateien, in denen geaenderte
// Passagen mit Annotationsmarken umschlossen sind:
//
//	----Start Annotation [user]
//	... geaenderter Text ...
//	----End Annotation [user]
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
)

const (
	Keyword   = "Annotation"
	StartWord = "Start"
	EndWord   = "End"
	Dashes    = "----"
)

// Aeltere Dateien tragen das Wort doppelt geschrieben und hinter dem Namen
// noch einen Zeitstempel. Beides wird beim Lesen hingenommen und beim
// Schreiben stillschweigend abgelegt.
const (
	word  = `Annott?ation`
	stamp = `(?: *\[[^\]]*\])?`
)

var (
	startRe = regexp.MustCompile(`^` + Dashes + ` *` + StartWord + ` +` + word + ` *\[(.*?)\]` + stamp + ` *$`)
	endRe   = regexp.MustCompile(`^` + Dashes + ` *` + EndWord + ` +` + word + ` *\[(.*?)\]` + stamp + ` *$`)
	fenceRe = regexp.MustCompile("^[ \t]{0,3}(`{3,}|~{3,})")
)

// Region ist ein halboffener Zeilenbereich [Start, End) im sauberen Text.
// Leere Regionen gibt es nicht: eine Marke ohne Inhalt sagt nichts aus.
type Region struct {
	Start int    `json:"start"`
	End   int    `json:"end"`
	User  string `json:"user"`

	// fresh unterscheidet die eben entstandene Region von den schon in der
	// Datei stehenden. Verschmelzen zwei, gehoert die Marke dem, der zuletzt
	// Hand angelegt hat.
	fresh bool
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
			stack = append(stack, open{len(d.Lines), m[1]})
			continue
		}
		if endRe.MatchString(line) && len(stack) > 0 {
			o := stack[len(stack)-1]
			stack = stack[:len(stack)-1]
			d.Regions = append(d.Regions, Region{Start: o.start, End: len(d.Lines), User: o.user})
			continue
		}
		d.Lines = append(d.Lines, line)
	}
	// Nicht geschlossene Marken laufen bis zum Dateiende.
	for _, o := range stack {
		d.Regions = append(d.Regions, Region{Start: o.start, End: len(d.Lines), User: o.user})
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
		byStart[r.Start] = append(byStart[r.Start], r)
		byEnd[r.End] = append(byEnd[r.End], r)
	}

	for i := 0; i <= n; i++ {
		for _, r := range byEnd[i] {
			out = append(out, EndLine(r.User))
		}
		for _, r := range byStart[i] {
			out = append(out, StartLine(r.User))
		}
		if i < n {
			out = append(out, d.Lines[i])
		}
	}
	return join(out, d.CRLF, d.FinalNL)
}

// StartLine und EndLine bauen die Markenzeilen.
func StartLine(user string) string {
	return fmt.Sprintf("%s%s %s [%s]", Dashes, StartWord, Keyword, user)
}

func EndLine(user string) string {
	return fmt.Sprintf("%s%s %s [%s]", Dashes, EndWord, Keyword, user)
}

// IsMarker sagt, ob eine Zeile eine Annotationsmarke ist.
func IsMarker(line string) bool {
	return startRe.MatchString(line) || endRe.MatchString(line)
}

// Apply nimmt den alten Dateiinhalt und den neuen sauberen Text, umschliesst
// jede geaenderte Passage mit Marken und liefert den neuen Dateiinhalt.
func Apply(oldRaw, newClean, user string) Doc {
	return rewrite(oldRaw, newClean, user, true)
}

// ApplyPlain schreibt den neuen Text, ohne die Aenderung zu markieren.
// Bereits vorhandene Marken bleiben erhalten und wandern mit ihren Zeilen mit.
func ApplyPlain(oldRaw, newClean string) Doc {
	return rewrite(oldRaw, newClean, "", false)
}

func rewrite(oldRaw, newClean, user string, mark bool) Doc {
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
		if s < e {
			next.Regions = append(next.Regions, Region{Start: s, End: e, User: r.User})
		}
		// Ist von der Region nichts uebrig, wurde ihr Inhalt geloescht. Eine
		// Marke ohne Inhalt sagt nichts, also faellt sie weg.
	}

	// Neue Regionen fuer jede geaenderte Passage.
	if mark {
		for _, h := range hunks(ops, next.Lines) {
			next.Regions = append(next.Regions, Region{Start: h.start, End: h.end, User: user, fresh: true})
		}
	}

	next.Regions = normalize(next.Regions)
	return next
}

// hunk ist ein zusammenhaengender geaenderter Bereich in neuen Koordinaten.
type hunk struct{ start, end int }

func hunks(ops []op, newLines []string) []hunk {
	var out []hunk
	i := 0
	for i < len(ops) {
		if ops[i].kind == opEqual {
			i++
			continue
		}
		j := i
		insStart, insEnd := -1, -1
		for j < len(ops) && ops[j].kind != opEqual {
			if ops[j].kind == opIns {
				if insStart < 0 {
					insStart = ops[j].b
				}
				insEnd = ops[j].b + 1
			}
			j++
		}
		// Nur Geloeschtes hinterlaesst keine Marke: ein Markenpaar ohne
		// Inhalt zwischen sich waere im Text nur eine leere Stelle.
		if insStart >= 0 {
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
			}
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

// normalize sortiert die Regionen, wirft leere weg und verschmilzt
// ueberlappende oder direkt aneinandergrenzende zu einer. Ergebnis: disjunkt,
// sortiert, nie verschachtelt. Beim Verschmelzen gewinnt, wer zuletzt
// geaendert hat.
func normalize(in []Region) []Region {
	rs := make([]Region, 0, len(in))
	for _, r := range in {
		if !r.empty() {
			rs = append(rs, r)
		}
	}
	if len(rs) == 0 {
		return nil
	}
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
			if r.fresh {
				last.User, last.fresh = r.User, true
			}
			continue
		}
		out = append(out, r)
	}
	return out
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
