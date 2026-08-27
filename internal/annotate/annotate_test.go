package annotate

import (
	"strings"
	"testing"
)

func apply(t *testing.T, raw, clean string) string {
	t.Helper()
	return Apply(raw, clean, "chris").Raw()
}

func TestRoundTripWithoutChanges(t *testing.T) {
	raw := "# Titel\n\nEin Absatz.\n"
	if got := apply(t, raw, "# Titel\n\nEin Absatz.\n"); got != raw {
		t.Fatalf("unveraenderter Text darf nicht angefasst werden:\n%q", got)
	}
}

func TestWrapChangedLine(t *testing.T) {
	raw := "# Titel\n\nEin Absatz.\n"
	got := apply(t, raw, "# Titel\n\nEin geaenderter Absatz.\n")
	want := "# Titel\n\n" +
		StartLine("chris") + "\n" +
		"Ein geaenderter Absatz.\n" +
		EndLine("chris") + "\n"
	if got != want {
		t.Fatalf("got:\n%s\nwant:\n%s", got, want)
	}
}

func TestParseStripsMarkers(t *testing.T) {
	raw := "a\n" + StartLine("chris") + "\nb\n" + EndLine("chris") + "\nc\n"
	d := Parse(raw)
	if d.Text() != "a\nb\nc\n" {
		t.Fatalf("clean text falsch: %q", d.Text())
	}
	if len(d.Regions) != 1 || d.Regions[0].Start != 1 || d.Regions[0].End != 2 {
		t.Fatalf("regions falsch: %+v", d.Regions)
	}
	if d.Raw() != raw {
		t.Fatalf("Raw() ist nicht idempotent:\n%q\n%q", d.Raw(), raw)
	}
}

func TestEditInsideExistingRegionDoesNotNest(t *testing.T) {
	raw := apply(t, "a\nb\nc\n", "a\nB\nc\n")
	got := apply(t, raw, "a\nB!\nc\n")
	if strings.Count(got, StartWord+" "+Keyword) != 1 {
		t.Fatalf("es darf nur eine Startmarke geben:\n%s", got)
	}
}

func TestAdjacentRegionsMerge(t *testing.T) {
	raw := apply(t, "a\nb\nc\n", "A\nb\nc\n")
	got := apply(t, raw, Parse(raw).Text()[:0]+"A\nB\nc\n")
	if strings.Count(got, StartWord+" "+Keyword) != 1 {
		t.Fatalf("angrenzende Regionen muessen verschmelzen:\n%s", got)
	}
}

func TestDeletionLeavesNoMarker(t *testing.T) {
	got := apply(t, "a\nweg\nc\n", "a\nc\n")
	if strings.Contains(got, Keyword) {
		t.Fatalf("eine Loeschung hinterlaesst keine leere Marke:\n%s", got)
	}
	if got != "a\nc\n" {
		t.Fatalf("got %q", got)
	}
}

func TestGeleerteRegionFaelltWeg(t *testing.T) {
	raw := apply(t, "a\nb\nc\n", "a\nB\nc\n")
	got := apply(t, raw, "a\nc\n")
	if strings.Contains(got, Keyword) {
		t.Fatalf("von der geleerten Region darf nichts stehen bleiben:\n%s", got)
	}
}

func TestErsetzenMarkiertDasNeue(t *testing.T) {
	got := apply(t, "a\nalt\nc\n", "a\nneu\nc\n")
	want := "a\n" + StartLine("chris") + "\nneu\n" + EndLine("chris") + "\nc\n"
	if got != want {
		t.Fatalf("got:\n%s\nwant:\n%s", got, want)
	}
}

func TestLeeresMarkenpaarWirdBeimSchreibenAufgeloest(t *testing.T) {
	raw := "a\n" + StartLine("chris") + "\n" + EndLine("chris") + "\nb\n"
	d := Parse(raw)
	if len(d.Regions) != 0 {
		t.Fatalf("ein Markenpaar ohne Inhalt ist keine Region: %+v", d.Regions)
	}
	if d.Raw() != "a\nb\n" {
		t.Fatalf("leere Marken muessen verschwinden: %q", d.Raw())
	}
}

func TestAltschreibweiseUndZeitstempelWerdenGelesen(t *testing.T) {
	raw := "a\n----Start Annottation [chris] [2026-08-22T12:00:00+02:00]\nb\n" +
		"----End Annottation [chris] [2026-08-22T12:00:00+02:00]\nc\n"
	d := Parse(raw)
	if len(d.Regions) != 1 || d.Regions[0].User != "chris" {
		t.Fatalf("alte Marke nicht erkannt: %+v", d.Regions)
	}
	want := "a\n" + StartLine("chris") + "\nb\n" + EndLine("chris") + "\nc\n"
	if d.Raw() != want {
		t.Fatalf("beim Schreiben gilt die neue Form:\n%s", d.Raw())
	}
}

func TestBlankLineDeletionIsIgnored(t *testing.T) {
	got := apply(t, "a\n\nb\n", "a\nb\n")
	if strings.Contains(got, Keyword) {
		t.Fatalf("reine Leerzeilen sollen keine Marke erzeugen:\n%s", got)
	}
}

func TestInsertAfterRegionMerges(t *testing.T) {
	raw := apply(t, "a\n", "A\n")
	got := apply(t, raw, "A\nneu\n")
	if strings.Count(got, StartWord+" "+Keyword) != 1 {
		t.Fatalf("neue Zeile direkt danach soll verschmelzen:\n%s", got)
	}
}

func TestRegionSurvivesUnrelatedEdit(t *testing.T) {
	raw := apply(t, "a\nb\nc\nd\n", "a\nB\nc\nd\n")
	got := apply(t, raw, "a\nB\nc\nD\n")
	d := Parse(got)
	if len(d.Regions) != 2 {
		t.Fatalf("erwarte zwei getrennte Regionen, habe %d:\n%s", len(d.Regions), got)
	}
	if d.Regions[0].Start != 1 || d.Regions[0].End != 2 {
		t.Fatalf("die alte Region muss an ihrer Stelle bleiben: %+v", d.Regions[0])
	}
}

func TestEmptyFile(t *testing.T) {
	got := apply(t, "", "Hallo\n")
	want := StartLine("chris") + "\nHallo\n" + EndLine("chris") + "\n"
	if got != want {
		t.Fatalf("got %q want %q", got, want)
	}
}

func TestCRLFPreserved(t *testing.T) {
	got := apply(t, "a\r\nb\r\n", "a\nB\n")
	if !strings.Contains(got, "\r\n") || strings.Contains(strings.ReplaceAll(got, "\r\n", ""), "\n") {
		t.Fatalf("CRLF muss erhalten bleiben: %q", got)
	}
}

func TestMultiLineInsert(t *testing.T) {
	got := apply(t, "a\nz\n", "a\neins\nzwei\ndrei\nz\n")
	d := Parse(got)
	if len(d.Regions) != 1 || d.Regions[0].Start != 1 || d.Regions[0].End != 4 {
		t.Fatalf("regions falsch: %+v\n%s", d.Regions, got)
	}
}

func TestMarkersInsideCodeFenceAreText(t *testing.T) {
	raw := "Beispiel:\n\n```\n" + StartLine("chris") + "\ntext\n" + EndLine("chris") + "\n```\n\nEnde.\n"
	d := Parse(raw)
	if len(d.Regions) != 0 {
		t.Fatalf("im Codeblock darf keine Marke erkannt werden: %+v", d.Regions)
	}
	if d.Text() != raw {
		t.Fatalf("Codeblock muss unveraendert bleiben:\n%q", d.Text())
	}
	if d.Raw() != raw {
		t.Fatalf("Raw() muss unveraendert bleiben:\n%q", d.Raw())
	}
}

func TestUnclosedFenceDoesNotSwallowMarkers(t *testing.T) {
	// Ein Zaun, der wieder geschlossen wird, darf die Marke danach nicht verdecken.
	raw := "```\ncode\n```\n" + StartLine("chris") + "\nneu\n" + EndLine("chris") + "\n"
	d := Parse(raw)
	if len(d.Regions) != 1 {
		t.Fatalf("Marke nach dem Codeblock fehlt: %+v", d.Regions)
	}
	if d.Raw() != raw {
		t.Fatalf("Raw() weicht ab:\n%q", d.Raw())
	}
}

func TestMarkersHugTheText(t *testing.T) {
	// Ein neuer Absatz am Ende: die Marke darf die Trennleerzeile nicht einschliessen.
	got := apply(t, "# Titel\n\nText.\n", "# Titel\n\nText.\n\nNeuer Absatz.\n")
	want := "# Titel\n\nText.\n\n" +
		StartLine("chris") + "\nNeuer Absatz.\n" +
		EndLine("chris") + "\n"
	if got != want {
		t.Fatalf("got:\n%s\nwant:\n%s", got, want)
	}
}

func TestMarkersHugTextInTheMiddle(t *testing.T) {
	got := apply(t, "A\n\nC\n", "A\n\nB\n\nC\n")
	want := "A\n\n" +
		StartLine("chris") + "\nB\n" + EndLine("chris") +
		"\n\nC\n"
	if got != want {
		t.Fatalf("got:\n%s\nwant:\n%s", got, want)
	}
}

func TestPureBlankInsertGetsNoMarker(t *testing.T) {
	got := apply(t, "A\n\nB\n", "A\n\n\n\nB\n")
	if strings.Contains(got, Keyword) {
		t.Fatalf("eingefuegte Leerzeilen brauchen keine Marke:\n%s", got)
	}
}

func TestApplyPlainSetztKeineMarke(t *testing.T) {
	got := ApplyPlain("# Titel\n\nEin Absatz.\n", "# Titel\n\nEin geaenderter Absatz.\n").Raw()
	want := "# Titel\n\nEin geaenderter Absatz.\n"
	if got != want {
		t.Fatalf("got:\n%q\nwant:\n%q", got, want)
	}
}

func TestApplyPlainLaesstBestehendeMarkeStehen(t *testing.T) {
	raw := apply(t, "a\nb\nc\n", "a\nB\nc\n")
	got := ApplyPlain(raw, "vorher\na\nB\nc\n").Raw()
	want := "vorher\na\n" +
		StartLine("chris") + "\n" +
		"B\n" +
		EndLine("chris") + "\n" +
		"c\n"
	if got != want {
		t.Fatalf("got:\n%s\nwant:\n%s", got, want)
	}
}

func TestCompareStelltBeideFassungenNebeneinander(t *testing.T) {
	alt := "# Plan\n\nEin Absatz, der bleibt.\n\nDer alte Schluss.\n"
	neu := "# Plan\n\nEin Absatz, der bleibt.\n\nDer neue Schluss.\n\nUnd noch ein Satz.\n"

	c := Compare(alt, neu)
	lines := strings.Split(strings.TrimSuffix(c.Text, "\n"), "\n")

	if len(c.Removed) != 1 || len(c.Added) != 1 {
		t.Fatalf("eine Streichung und eine Ergaenzung erwartet: %+v", c)
	}
	if got := lines[c.Removed[0].Start:c.Removed[0].End]; len(got) != 1 || got[0] != "Der alte Schluss." {
		t.Fatalf("falsche Streichung: %q", got)
	}
	got := lines[c.Added[0].Start:c.Added[0].End]
	if len(got) != 3 || got[0] != "Der neue Schluss." || got[2] != "Und noch ein Satz." {
		t.Fatalf("falsche Ergaenzung: %q", got)
	}
	if !strings.Contains(c.Text, "Ein Absatz, der bleibt.") {
		t.Fatal("der unveraenderte Teil fehlt im Vergleich")
	}
}

func TestCompareUebergehtDieMarken(t *testing.T) {
	alt := "Ein Satz.\n"
	neu := StartLine("wer") + "\nEin Satz.\n" +
		EndLine("wer") + "\n"

	c := Compare(alt, neu)
	if len(c.Removed) != 0 || len(c.Added) != 0 {
		t.Fatalf("eine gesetzte Marke ist kein Unterschied: %+v", c)
	}
	if c.Text != "Ein Satz.\n" {
		t.Fatalf("der Vergleichstext traegt keine Marken: %q", c.Text)
	}
}

func TestCompareOhneUnterschied(t *testing.T) {
	c := Compare("gleich\n", "gleich\n")
	if len(c.Removed) != 0 || len(c.Added) != 0 {
		t.Fatalf("kein Unterschied erwartet: %+v", c)
	}
}
