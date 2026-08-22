package annotate

import (
	"strings"
	"testing"
	"time"
)

var t0 = time.Date(2026, 8, 22, 12, 0, 0, 0, time.UTC)
var t1 = time.Date(2026, 8, 22, 13, 0, 0, 0, time.UTC)

func apply(t *testing.T, raw, clean string, at time.Time) string {
	t.Helper()
	return Apply(raw, clean, "chris", at).Raw()
}

func TestRoundTripWithoutChanges(t *testing.T) {
	raw := "# Titel\n\nEin Absatz.\n"
	if got := apply(t, raw, "# Titel\n\nEin Absatz.\n", t0); got != raw {
		t.Fatalf("unveraenderter Text darf nicht angefasst werden:\n%q", got)
	}
}

func TestWrapChangedLine(t *testing.T) {
	raw := "# Titel\n\nEin Absatz.\n"
	got := apply(t, raw, "# Titel\n\nEin geaenderter Absatz.\n", t0)
	want := "# Titel\n\n" +
		StartLine("chris", t0.Format(TimeLayot)) + "\n" +
		"Ein geaenderter Absatz.\n" +
		EndLine("chris", t0.Format(TimeLayot)) + "\n"
	if got != want {
		t.Fatalf("got:\n%s\nwant:\n%s", got, want)
	}
}

func TestParseStripsMarkers(t *testing.T) {
	raw := "a\n" + StartLine("chris", "T") + "\nb\n" + EndLine("chris", "T") + "\nc\n"
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
	raw := apply(t, "a\nb\nc\n", "a\nB\nc\n", t0)
	got := apply(t, raw, "a\nB!\nc\n", t1)
	if strings.Count(got, StartWord+" "+Keyword) != 1 {
		t.Fatalf("es darf nur eine Startmarke geben:\n%s", got)
	}
	if !strings.Contains(got, t1.Format(TimeLayot)) {
		t.Fatalf("Zeitstempel muss aktualisiert werden:\n%s", got)
	}
}

func TestAdjacentRegionsMerge(t *testing.T) {
	raw := apply(t, "a\nb\nc\n", "A\nb\nc\n", t0)
	got := apply(t, raw, Parse(raw).Text()[:0]+"A\nB\nc\n", t1)
	if strings.Count(got, StartWord+" "+Keyword) != 1 {
		t.Fatalf("angrenzende Regionen muessen verschmelzen:\n%s", got)
	}
}

func TestDeletionLeavesMarker(t *testing.T) {
	got := apply(t, "a\nweg\nc\n", "a\nc\n", t0)
	if !strings.Contains(got, StartLine("chris", t0.Format(TimeLayot))+"\n"+EndLine("chris", t0.Format(TimeLayot))) {
		t.Fatalf("Loeschung braucht eine Nullbreite-Marke:\n%s", got)
	}
}

func TestBlankLineDeletionIsIgnored(t *testing.T) {
	got := apply(t, "a\n\nb\n", "a\nb\n", t0)
	if strings.Contains(got, Keyword) {
		t.Fatalf("reine Leerzeilen sollen keine Marke erzeugen:\n%s", got)
	}
}

func TestInsertAfterRegionMerges(t *testing.T) {
	raw := apply(t, "a\n", "A\n", t0)
	got := apply(t, raw, "A\nneu\n", t1)
	if strings.Count(got, StartWord+" "+Keyword) != 1 {
		t.Fatalf("neue Zeile direkt danach soll verschmelzen:\n%s", got)
	}
}

func TestRegionSurvivesUnrelatedEdit(t *testing.T) {
	raw := apply(t, "a\nb\nc\nd\n", "a\nB\nc\nd\n", t0)
	got := apply(t, raw, "a\nB\nc\nD\n", t1)
	d := Parse(got)
	if len(d.Regions) != 2 {
		t.Fatalf("erwarte zwei getrennte Regionen, habe %d:\n%s", len(d.Regions), got)
	}
	if d.Regions[0].Time != t0.Format(TimeLayot) {
		t.Fatalf("alte Region darf ihren Zeitstempel behalten: %+v", d.Regions[0])
	}
}

func TestEmptyFile(t *testing.T) {
	got := apply(t, "", "Hallo\n", t0)
	want := StartLine("chris", t0.Format(TimeLayot)) + "\nHallo\n" + EndLine("chris", t0.Format(TimeLayot)) + "\n"
	if got != want {
		t.Fatalf("got %q want %q", got, want)
	}
}

func TestCRLFPreserved(t *testing.T) {
	got := apply(t, "a\r\nb\r\n", "a\nB\n", t0)
	if !strings.Contains(got, "\r\n") || strings.Contains(strings.ReplaceAll(got, "\r\n", ""), "\n") {
		t.Fatalf("CRLF muss erhalten bleiben: %q", got)
	}
}

func TestMultiLineInsert(t *testing.T) {
	got := apply(t, "a\nz\n", "a\neins\nzwei\ndrei\nz\n", t0)
	d := Parse(got)
	if len(d.Regions) != 1 || d.Regions[0].Start != 1 || d.Regions[0].End != 4 {
		t.Fatalf("regions falsch: %+v\n%s", d.Regions, got)
	}
}

func TestMarkersInsideCodeFenceAreText(t *testing.T) {
	raw := "Beispiel:\n\n```\n" + StartLine("chris", "T") + "\ntext\n" + EndLine("chris", "T") + "\n```\n\nEnde.\n"
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
	raw := "```\ncode\n```\n" + StartLine("chris", "T") + "\nneu\n" + EndLine("chris", "T") + "\n"
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
	got := apply(t, "# Titel\n\nText.\n", "# Titel\n\nText.\n\nNeuer Absatz.\n", t0)
	want := "# Titel\n\nText.\n\n" +
		StartLine("chris", t0.Format(TimeLayot)) + "\nNeuer Absatz.\n" +
		EndLine("chris", t0.Format(TimeLayot)) + "\n"
	if got != want {
		t.Fatalf("got:\n%s\nwant:\n%s", got, want)
	}
}

func TestMarkersHugTextInTheMiddle(t *testing.T) {
	got := apply(t, "A\n\nC\n", "A\n\nB\n\nC\n", t0)
	want := "A\n\n" +
		StartLine("chris", t0.Format(TimeLayot)) + "\nB\n" + EndLine("chris", t0.Format(TimeLayot)) +
		"\n\nC\n"
	if got != want {
		t.Fatalf("got:\n%s\nwant:\n%s", got, want)
	}
}

func TestPureBlankInsertGetsNoMarker(t *testing.T) {
	got := apply(t, "A\n\nB\n", "A\n\n\n\nB\n", t0)
	if strings.Contains(got, Keyword) {
		t.Fatalf("eingefuegte Leerzeilen brauchen keine Marke:\n%s", got)
	}
}
