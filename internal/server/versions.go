package server

// Fassungen einer Datei: der Stand auf der Platte, was mda waehrend der
// Sitzung abgelegt hat und was Git kennt — alles in einer Zeitleiste.

import (
	"errors"
	"net/http"
	"os"
	"sort"
	"strings"
	"time"

	"github.com/christianluis/mdannotate/internal/annotate"
	"github.com/christianluis/mdannotate/internal/history"
)

// maxCommits begrenzt, wie weit in die Vergangenheit geschaut wird.
const maxCommits = 200

type versionList struct {
	Path     string            `json:"path"`
	Git      bool              `json:"git"`
	Changes  string            `json:"changes"`
	Versions []history.Version `json:"versions"`
}

type diffBody struct {
	Path    string          `json:"path"`
	A       string          `json:"a"`
	B       string          `json:"b"`
	Text    string          `json:"text"`
	Removed []annotate.Span `json:"removed"`
	Added   []annotate.Span `json:"added"`
	Places  int             `json:"places"`
}

type versionBody struct {
	Path    string            `json:"path"`
	ID      string            `json:"id"`
	Text    string            `json:"text"`
	Regions []annotate.Region `json:"regions"`
	Marks   int               `json:"marks"`
}

func (s *Server) handleVersions(w http.ResponseWriter, r *http.Request) {
	rel := r.URL.Query().Get("path")
	abs, err := s.resolve(rel)
	if err != nil || !isMarkdown(abs) {
		http.Error(w, "ungueltiger Pfad", http.StatusBadRequest)
		return
	}

	// Der Arbeitsstand steht immer obenan; ein Commit kann juenger
	// datiert sein als die Datei, gemeint ist er damit nicht.
	out := []history.Version{}
	if info, serr := os.Stat(abs); serr == nil {
		out = append(out, history.Version{
			ID:    "live",
			Kind:  history.KindLive,
			Time:  info.ModTime().Format(time.RFC3339),
			Label: "Arbeitsstand",
		})
	}
	rest := s.Store.Versions(rel)
	if s.Git != nil {
		rest = append(rest, s.Git.Versions(rel, maxCommits)...)
	}
	sortVersions(rest)
	out = append(out, rest...)

	writeJSON(w, versionList{Path: rel, Git: s.Git != nil, Changes: s.Changes(), Versions: out})
}

func (s *Server) handleVersion(w http.ResponseWriter, r *http.Request) {
	rel := r.URL.Query().Get("path")
	abs, err := s.resolve(rel)
	if err != nil || !isMarkdown(abs) {
		http.Error(w, "ungueltiger Pfad", http.StatusBadRequest)
		return
	}
	id := r.URL.Query().Get("id")
	raw, err := s.versionRaw(rel, abs, id)
	if err != nil {
		http.Error(w, err.Error(), http.StatusNotFound)
		return
	}

	doc := annotate.Parse(raw)
	regions := doc.Regions
	if regions == nil {
		regions = []annotate.Region{}
	}
	writeJSON(w, versionBody{Path: rel, ID: id, Text: doc.Text(), Regions: regions, Marks: len(regions)})
}

// handleDiff stellt zwei Fassungen nebeneinander: a ist die aeltere, b die,
// die gerade angesehen wird.
func (s *Server) handleDiff(w http.ResponseWriter, r *http.Request) {
	rel := r.URL.Query().Get("path")
	abs, err := s.resolve(rel)
	if err != nil || !isMarkdown(abs) {
		http.Error(w, "ungueltiger Pfad", http.StatusBadRequest)
		return
	}
	a, err := s.versionRaw(rel, abs, r.URL.Query().Get("a"))
	if err != nil {
		http.Error(w, err.Error(), http.StatusNotFound)
		return
	}
	b, err := s.versionRaw(rel, abs, r.URL.Query().Get("b"))
	if err != nil {
		http.Error(w, err.Error(), http.StatusNotFound)
		return
	}

	c := annotate.Compare(a, b)
	writeJSON(w, diffBody{
		Path:    rel,
		A:       r.URL.Query().Get("a"),
		B:       r.URL.Query().Get("b"),
		Text:    c.Text,
		Removed: c.Removed,
		Added:   c.Added,
		Places:  c.Places,
	})
}

// versionRaw holt den Dateiinhalt einer Fassung, so wie er auf der Platte,
// in der Ablage oder im Archiv steht — mit Marken, das Zerlegen kommt spaeter.
func (s *Server) versionRaw(rel, abs, id string) (string, error) {
	switch {
	case id == "live":
		b, err := os.ReadFile(abs)
		return string(b), err
	case strings.HasPrefix(id, "snap:"):
		return s.Store.Read(rel, strings.TrimPrefix(id, "snap:"))
	case strings.HasPrefix(id, "git:"):
		return s.Git.Show(rel, strings.TrimPrefix(id, "git:"))
	}
	return "", errors.New("unbekannte Fassung")
}

// sortVersions stellt die juengste Fassung nach vorn. Fallen zwei auf
// dieselbe Sekunde, steht die abgelegte vor der aus Git.
func sortVersions(vs []history.Version) {
	rank := func(kind string) int {
		if kind == history.KindGit {
			return 1
		}
		return 0
	}
	when := func(v history.Version) time.Time {
		t, err := time.Parse(time.RFC3339, v.Time)
		if err != nil {
			return time.Time{}
		}
		return t
	}
	sort.SliceStable(vs, func(i, j int) bool {
		a, b := when(vs[i]), when(vs[j])
		if !a.Equal(b) {
			return a.After(b)
		}
		return rank(vs[i].Kind) < rank(vs[j].Kind)
	})
}
