package server

import (
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"

	"github.com/christianluis/mdannotate/internal/annotate"
)

// maxFiles begrenzt den Baum, damit ein versehentliches `mda ~` nicht das
// halbe Dateisystem einliest.
const maxFiles = 8000

var skipDirs = map[string]bool{
	"node_modules": true, "vendor": true, "dist": true, "build": true,
	"target": true, "__pycache__": true, ".venv": true, "venv": true,
	"Pods": true, ".terraform": true,
}

// Node ist ein Eintrag im Dateibaum.
type Node struct {
	Name     string  `json:"name"`
	Path     string  `json:"path"`
	Dir      bool    `json:"dir"`
	Children []*Node `json:"children,omitempty"`
	Marks    int     `json:"marks,omitempty"`
	Mod      int64   `json:"mod,omitempty"`
}

func isMarkdown(name string) bool {
	ext := strings.ToLower(filepath.Ext(name))
	return ext == ".md" || ext == ".markdown" || ext == ".mdx"
}

// markCache zaehlt Annotationen pro Datei und merkt sich das Ergebnis,
// solange sich Groesse und Zeitstempel nicht aendern.
type markCache struct {
	mu sync.Mutex
	m  map[string]markEntry
}

type markEntry struct {
	mod, size int64
	count     int
}

func newMarkCache() *markCache { return &markCache{m: map[string]markEntry{}} }

func (c *markCache) count(abs string, info fs.FileInfo) int {
	mod, size := info.ModTime().UnixMilli(), info.Size()
	c.mu.Lock()
	if e, ok := c.m[abs]; ok && e.mod == mod && e.size == size {
		c.mu.Unlock()
		return e.count
	}
	c.mu.Unlock()

	n := 0
	if size < 4<<20 {
		if b, err := os.ReadFile(abs); err == nil {
			n = len(annotate.Parse(string(b)).Regions)
		}
	}
	c.mu.Lock()
	c.m[abs] = markEntry{mod, size, n}
	c.mu.Unlock()
	return n
}

// buildTree liest den Markdown-Baum unterhalb von root ein. Ordner ohne
// Markdown-Dateien fallen weg.
func (s *Server) buildTree() *Node {
	root := &Node{Name: filepath.Base(s.Root), Path: "", Dir: true}
	dirs := map[string]*Node{"": root}
	count := 0

	filepath.WalkDir(s.Root, func(abs string, d fs.DirEntry, err error) error {
		if err != nil || count >= maxFiles {
			if d != nil && d.IsDir() && err != nil {
				return fs.SkipDir
			}
			return nil
		}
		rel, rerr := filepath.Rel(s.Root, abs)
		if rerr != nil {
			return nil
		}
		rel = filepath.ToSlash(rel)
		if rel == "." {
			return nil
		}
		name := d.Name()

		if d.IsDir() {
			if strings.HasPrefix(name, ".") || skipDirs[name] {
				return fs.SkipDir
			}
			return nil
		}
		if !isMarkdown(name) || strings.HasPrefix(name, ".") {
			return nil
		}
		info, ierr := d.Info()
		if ierr != nil {
			return nil
		}
		parent := ensureDir(dirs, root, filepath.ToSlash(filepath.Dir(rel)))
		parent.Children = append(parent.Children, &Node{
			Name:  name,
			Path:  rel,
			Marks: s.marks.count(abs, info),
			Mod:   info.ModTime().UnixMilli(),
		})
		count++
		return nil
	})

	prune(root)
	sortTree(root)
	return root
}

// ensureDir legt fehlende Zwischenordner an.
func ensureDir(dirs map[string]*Node, root *Node, rel string) *Node {
	if rel == "." || rel == "" {
		return root
	}
	if n, ok := dirs[rel]; ok {
		return n
	}
	parent := ensureDir(dirs, root, filepath.ToSlash(filepath.Dir(rel)))
	n := &Node{Name: filepath.Base(rel), Path: rel, Dir: true}
	parent.Children = append(parent.Children, n)
	dirs[rel] = n
	return n
}

// prune entfernt Ordner, in denen keine Markdown-Datei liegt.
func prune(n *Node) bool {
	keep := n.Children[:0]
	for _, c := range n.Children {
		if !c.Dir || prune(c) {
			keep = append(keep, c)
		}
	}
	n.Children = keep
	return len(n.Children) > 0
}

func sortTree(n *Node) {
	sort.SliceStable(n.Children, func(i, j int) bool {
		a, b := n.Children[i], n.Children[j]
		if a.Dir != b.Dir {
			return a.Dir
		}
		return strings.ToLower(a.Name) < strings.ToLower(b.Name)
	})
	for _, c := range n.Children {
		sortTree(c)
	}
}

// snapshot bildet den Zustand aller Markdown-Dateien ab, um Aenderungen von
// aussen (etwa durch eine KI, die die Datei schreibt) zu erkennen.
func (s *Server) snapshot() map[string]int64 {
	out := map[string]int64{}
	count := 0
	filepath.WalkDir(s.Root, func(abs string, d fs.DirEntry, err error) error {
		if err != nil || count >= maxFiles {
			return nil
		}
		if d.IsDir() {
			name := d.Name()
			if abs != s.Root && (strings.HasPrefix(name, ".") || skipDirs[name]) {
				return fs.SkipDir
			}
			return nil
		}
		if !isMarkdown(d.Name()) {
			return nil
		}
		info, ierr := d.Info()
		if ierr != nil {
			return nil
		}
		rel, _ := filepath.Rel(s.Root, abs)
		out[filepath.ToSlash(rel)] = info.ModTime().UnixMilli()*997 + info.Size()
		count++
		return nil
	})
	return out
}
