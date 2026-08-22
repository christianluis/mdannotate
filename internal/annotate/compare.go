package annotate

import "strings"

// Span ist ein halboffener Zeilenbereich [Start, End) im Vergleichstext.
type Span struct {
	Start int `json:"start"`
	End   int `json:"end"`
}

// Comparison stellt zwei Fassungen in einem Text nebeneinander: was wegfiel,
// steht direkt vor dem, was an seine Stelle getreten ist. Removed und Added
// sagen, welche Zeilen davon woher stammen.
type Comparison struct {
	Text    string `json:"text"`
	Removed []Span `json:"removed"`
	Added   []Span `json:"added"`
	// Places zaehlt die Stellen, an denen sich etwas tut. Ein Absatz, der
	// durch einen anderen ersetzt wurde, ist eine Stelle, nicht zwei.
	Places int `json:"places"`
}

// Compare vergleicht zwei Fassungen derselben Datei zeilenweise. Verglichen
// wird der saubere Text ohne Annotationsmarken — sonst waere jede gesetzte
// Marke fuer sich schon ein Unterschied.
func Compare(oldRaw, newRaw string) Comparison {
	a := Parse(oldRaw).Lines
	b := Parse(newRaw).Lines

	var out []string
	var c Comparison

	// span haengt die Zeilen an und merkt sich, wo sie stehen. Leerzeilen an
	// den Raendern bleiben ausserhalb der Marke, damit der Balken am Text
	// steht und nicht daneben.
	span := func(lines []string) *Span {
		start := len(out)
		out = append(out, lines...)
		s, e := start, len(out)
		for s < e && strings.TrimSpace(out[s]) == "" {
			s++
		}
		for e > s && strings.TrimSpace(out[e-1]) == "" {
			e--
		}
		if s >= e {
			return nil
		}
		return &Span{s, e}
	}

	// gap setzt eine Leerzeile, wo noch keine steht. Ohne sie liefe die alte
	// Zeile mit der neuen zu einem Absatz zusammen, und der Balken im Rand
	// wuesste nicht mehr, wem er gilt.
	gap := func() {
		if len(out) > 0 && strings.TrimSpace(out[len(out)-1]) != "" {
			out = append(out, "")
		}
	}

	ops := diffLines(a, b)
	i, pending := 0, false
	for i < len(ops) {
		if ops[i].kind == opEqual {
			line := b[ops[i].b]
			if pending {
				if strings.TrimSpace(line) != "" {
					gap()
				}
				pending = false
			}
			out = append(out, line)
			i++
			continue
		}
		var gone, kam []string
		j := i
		for ; j < len(ops) && ops[j].kind != opEqual; j++ {
			if ops[j].kind == opDel {
				gone = append(gone, a[ops[j].a])
			} else {
				kam = append(kam, b[ops[j].b])
			}
		}
		gap()
		hit := false
		if s := span(gone); s != nil {
			c.Removed = append(c.Removed, *s)
			hit = true
		}
		if len(gone) > 0 && len(kam) > 0 {
			gap()
		}
		if s := span(kam); s != nil {
			c.Added = append(c.Added, *s)
			hit = true
		}
		if hit {
			c.Places++
		}
		pending = true
		i = j
	}

	c.Text = join(out, false, true)
	if c.Removed == nil {
		c.Removed = []Span{}
	}
	if c.Added == nil {
		c.Added = []Span{}
	}
	return c
}
