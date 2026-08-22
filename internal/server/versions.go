package server

// Fassungen einer Datei: der Stand auf der Platte, was mda waehrend der
// Sitzung abgelegt hat und was Git kennt — alles in einer Zeitleiste.

import (
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

	var raw string
	switch {
	case id == "live":
		b, rerr := os.ReadFile(abs)
		if rerr != nil {
			http.Error(w, rerr.Error(), http.StatusNotFound)
			return
		}
		raw = string(b)
	case strings.HasPrefix(id, "snap:"):
		raw, err = s.Store.Read(rel, strings.TrimPrefix(id, "snap:"))
	case strings.HasPrefix(id, "git:"):
		raw, err = s.Git.Show(rel, strings.TrimPrefix(id, "git:"))
	default:
		http.Error(w, "unbekannte Fassung", http.StatusBadRequest)
		return
	}
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
