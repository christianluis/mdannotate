package history

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestKeyIstLesbarUndEindeutig(t *testing.T) {
	a := Key("/Users/wer/Projekte/Mein Ordner")
	b := Key("/Users/andere/Projekte/Mein Ordner")
	if a == b {
		t.Fatalf("gleiche Kennung fuer verschiedene Pfade: %s", a)
	}
	if !strings.HasPrefix(a, "mein-ordner-") {
		t.Fatalf("Kennung sollte mit dem Ordnernamen anfangen: %s", a)
	}
	if Key("/") == "" {
		t.Fatal("auch die Wurzel braucht eine Kennung")
	}
}

// store legt eine Ablage im Testordner an, ohne den Heimatordner anzufassen.
func store(t *testing.T) *Store {
	t.Helper()
	return &Store{Dir: t.TempDir()}
}

func TestSaveLegtAbUndWiederholtSichNicht(t *testing.T) {
	s := store(t)

	if ok, err := s.Save("notizen/plan.md", "erste Fassung\n", KindStart); err != nil || !ok {
		t.Fatalf("erste Fassung: ok=%v err=%v", ok, err)
	}
	if ok, err := s.Save("notizen/plan.md", "erste Fassung\n", KindMda); err != nil || ok {
		t.Fatalf("gleicher Inhalt darf nicht noch einmal abgelegt werden: ok=%v err=%v", ok, err)
	}
	if ok, err := s.Save("notizen/plan.md", "zweite Fassung\n", KindMda); err != nil || !ok {
		t.Fatalf("zweite Fassung: ok=%v err=%v", ok, err)
	}

	vs := s.Versions("notizen/plan.md")
	if len(vs) != 2 {
		t.Fatalf("zwei Fassungen erwartet, %d bekommen: %+v", len(vs), vs)
	}
	if vs[0].Kind != KindMda || vs[1].Kind != KindStart {
		t.Fatalf("die juengste Fassung gehoert nach vorn: %+v", vs)
	}
	if vs[0].Time == "" || vs[0].Label == "" {
		t.Fatalf("Zeitpunkt und Beschriftung fehlen: %+v", vs[0])
	}

	raw, err := s.Read("notizen/plan.md", strings.TrimPrefix(vs[1].ID, "snap:"))
	if err != nil || raw != "erste Fassung\n" {
		t.Fatalf("zurueckgelesen: %q, %v", raw, err)
	}

	// Der Ordner heisst wie die Datei im Projekt.
	if _, err := os.Stat(filepath.Join(s.Dir, "notizen", "plan.md")); err != nil {
		t.Fatalf("Ablageordner fehlt: %v", err)
	}
}

func TestReadWehrtFremdePfadeAb(t *testing.T) {
	s := store(t)
	s.Save("plan.md", "Inhalt\n", KindMda)

	for _, name := range []string{"../../etc/passwd", "beliebig.md", "20260822-154321.472-mda.txt", ""} {
		if _, err := s.Read("plan.md", name); err == nil {
			t.Fatalf("Name %q haette abgelehnt werden muessen", name)
		}
	}
}

func TestGitFassungenUeberUmbenennungHinweg(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("ohne git kein Archiv")
	}
	dir := t.TempDir()

	git := func(args ...string) {
		t.Helper()
		cmd := exec.Command("git", append([]string{"-C", dir}, args...)...)
		cmd.Env = append(os.Environ(),
			"GIT_CONFIG_GLOBAL=/dev/null", "GIT_CONFIG_SYSTEM=/dev/null",
			"GIT_AUTHOR_NAME=Test", "GIT_AUTHOR_EMAIL=test@example.org",
			"GIT_COMMITTER_NAME=Test", "GIT_COMMITTER_EMAIL=test@example.org")
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
	}
	write := func(rel, text string) {
		t.Helper()
		os.MkdirAll(filepath.Join(dir, filepath.Dir(rel)), 0o755)
		if err := os.WriteFile(filepath.Join(dir, rel), []byte(text), 0o644); err != nil {
			t.Fatal(err)
		}
	}

	git("init", "-q", ".")
	write("plan.md", "# Plan\n\nerster Stand\n")
	git("add", "-A")
	git("commit", "-qm", "Plan angelegt")
	write("plan.md", "# Plan\n\nzweiter Stand\n")
	git("commit", "-qam", "Plan geändert")
	os.MkdirAll(filepath.Join(dir, "notizen"), 0o755)
	git("mv", "plan.md", "notizen/plan.md")
	git("commit", "-qm", "Plan einsortiert")

	repo := FindRepo(dir)
	if repo == nil {
		t.Fatal("das Archiv haette gefunden werden muessen")
	}

	vs := repo.Versions("notizen/plan.md", 50)
	if len(vs) != 3 {
		t.Fatalf("drei Commits erwartet, %d bekommen: %+v", len(vs), vs)
	}
	if vs[0].Label != "Plan einsortiert" || vs[2].Label != "Plan angelegt" {
		t.Fatalf("der juengste Commit gehoert nach vorn: %+v", vs)
	}

	// Der aelteste Commit kennt die Datei nur unter ihrem alten Pfad.
	raw, err := repo.Show("notizen/plan.md", strings.TrimPrefix(vs[2].ID, "git:"))
	if err != nil {
		t.Fatalf("Fassung aus dem Archiv: %v", err)
	}
	if raw != "# Plan\n\nerster Stand\n" {
		t.Fatalf("falscher Inhalt: %q", raw)
	}

	if _, err := repo.Show("notizen/plan.md", "keinhash"); err == nil {
		t.Fatal("ein unsinniger Commit haette abgelehnt werden muessen")
	}
	if vs := repo.Versions("gibtsnicht.md", 50); len(vs) != 0 {
		t.Fatalf("fuer eine unbekannte Datei gibt es nichts: %+v", vs)
	}
}

func TestOhneArchivKeinFehler(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("ohne git kein Archiv")
	}
	// Ein Ordner ohne .git — es sei denn, der Testordner liegt selbst in
	// einem Archiv; dann sagt git zu Recht, dass es eines gibt.
	dir := t.TempDir()
	repo := FindRepo(dir)
	if repo == nil {
		return
	}
	if repo.Top == "" {
		t.Fatal("ein gefundenes Archiv braucht eine Wurzel")
	}
}
