// Package history haelt die Fassungen einer Markdown-Datei bereit: die, die
// Git kennt, und die, die waehrend einer mda-Sitzung entstehen.
//
// Die eigenen Fassungen liegen unterhalb von
//
//	~/.mda/changes/<Ordnername>-<Abdruck des Pfades>/<Pfad der Datei>/
//
// und heissen nach dem Zeitpunkt ihrer Entstehung, also etwa
// 20260822-154321.472-mda.md. Damit ist der Ablageort mit blossem Auge
// lesbar und laesst sich mit den ueblichen Werkzeugen durchsuchen.
package history

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"sync"
	"time"
)

// Herkunft einer Fassung.
const (
	KindStart  = "start"  // so lag die Datei da, bevor mda sie anfasste
	KindMda    = "mda"    // im Editor gesichert
	KindExtern = "extern" // von aussen geschrieben, etwa von einer KI
	KindGit    = "git"    // ein Commit
	KindLive   = "live"   // der Stand auf der Platte
)

// Version ist ein Eintrag in der Fassungsliste.
type Version struct {
	ID    string `json:"id"`
	Kind  string `json:"kind"`
	Time  string `json:"time"`
	Label string `json:"label"`
	Note  string `json:"note,omitempty"`
}

const (
	stampLayout = "20060102-150405.000"
	// keep begrenzt die Fassungen je Datei; die aeltesten fallen weg.
	keep = 200
)

var nameRe = regexp.MustCompile(`^(\d{8}-\d{6}\.\d{3})-(start|mda|extern)\.md$`)

// Store ist die Ablage eines Projektordners unter ~/.mda/changes.
type Store struct {
	Dir string

	mu sync.Mutex
}

// Open legt die Ablage fuer einen Projektordner an.
func Open(root string) (*Store, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return nil, err
	}
	dir := filepath.Join(home, ".mda", "changes", Key(root))
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return nil, err
	}
	// Damit spaeter noch nachvollziehbar ist, wozu die Kennung gehoert.
	os.WriteFile(filepath.Join(dir, ".projekt"), []byte(root+"\n"), 0o600)
	return &Store{Dir: dir}, nil
}

// Key ist die Kennung eines Projektordners: sein Name und ein kurzer Abdruck
// des vollen Pfades, damit zwei gleichnamige Ordner sich nicht ins Gehege
// kommen.
func Key(root string) string {
	sum := sha256.Sum256([]byte(root))
	return slug(filepath.Base(root)) + "-" + hex.EncodeToString(sum[:])[:12]
}

// slug macht aus einem Ordnernamen etwas, das in jedem Dateisystem lebt.
func slug(s string) string {
	var b strings.Builder
	for _, r := range s {
		switch {
		case r >= 'a' && r <= 'z', r >= '0' && r <= '9', r == '-', r == '_':
			b.WriteRune(r)
		case r >= 'A' && r <= 'Z':
			b.WriteRune(r + ('a' - 'A'))
		default:
			b.WriteByte('-')
		}
	}
	out := strings.Trim(b.String(), "-")
	if out == "" {
		return "ordner"
	}
	if len(out) > 40 {
		out = strings.Trim(out[:40], "-")
	}
	return out
}

// Save legt raw als neue Fassung ab. Gleicht der Inhalt der neuesten Fassung,
// passiert nichts; sonst entsteht eine Datei mit dem aktuellen Zeitstempel.
func (s *Store) Save(rel, raw, kind string) (bool, error) {
	if s == nil {
		return false, nil
	}
	dir, err := s.dirFor(rel)
	if err != nil {
		return false, err
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	names := entries(dir)
	if len(names) > 0 {
		if b, err := os.ReadFile(filepath.Join(dir, names[0])); err == nil && string(b) == raw {
			return false, nil
		}
	}
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return false, err
	}
	name := next(names, time.Now()) + "-" + kind + ".md"
	if err := os.WriteFile(filepath.Join(dir, name), []byte(raw), 0o600); err != nil {
		return false, err
	}
	for i := keep - 1; i < len(names); i++ {
		os.Remove(filepath.Join(dir, names[i]))
	}
	return true, nil
}

// next ist der Zeitstempel der neuen Fassung. Zwei Fassungen in derselben
// Millisekunde kommen vor — beim Sichern wird erst der vorgefundene Stand
// abgelegt und gleich darauf der neue. Dann rueckt der Stempel eine
// Millisekunde vor, damit die Namen in der Reihenfolge bleiben, in der die
// Fassungen entstanden sind.
func next(names []string, now time.Time) string {
	stamp := now.Format(stampLayout)
	if len(names) == 0 {
		return stamp
	}
	prev := names[0][:len(stampLayout)]
	if prev < stamp {
		return stamp
	}
	t, err := time.ParseInLocation(stampLayout, prev, time.Local)
	if err != nil {
		return stamp
	}
	return t.Add(time.Millisecond).Format(stampLayout)
}

// Versions listet die abgelegten Fassungen einer Datei, die neueste zuerst.
func (s *Store) Versions(rel string) []Version {
	if s == nil {
		return nil
	}
	dir, err := s.dirFor(rel)
	if err != nil {
		return nil
	}
	s.mu.Lock()
	names := entries(dir)
	s.mu.Unlock()

	out := make([]Version, 0, len(names))
	for _, n := range names {
		m := nameRe.FindStringSubmatch(n)
		t, terr := time.ParseInLocation(stampLayout, m[1], time.Local)
		if terr != nil {
			continue
		}
		out = append(out, Version{
			ID:    "snap:" + n,
			Kind:  m[2],
			Time:  t.Format(time.RFC3339),
			Label: label(m[2]),
		})
	}
	return out
}

// Read holt eine abgelegte Fassung zurueck.
func (s *Store) Read(rel, name string) (string, error) {
	if s == nil {
		return "", errors.New("keine Ablage")
	}
	if !nameRe.MatchString(name) {
		return "", errors.New("ungueltige Fassung")
	}
	dir, err := s.dirFor(rel)
	if err != nil {
		return "", err
	}
	b, err := os.ReadFile(filepath.Join(dir, name))
	if err != nil {
		return "", err
	}
	return string(b), nil
}

func label(kind string) string {
	switch kind {
	case KindStart:
		return "vorgefunden"
	case KindExtern:
		return "von außen geändert"
	default:
		return "gesichert"
	}
}

// dirFor ist der Ablageordner einer Datei; er heisst wie ihr Pfad im Projekt.
func (s *Store) dirFor(rel string) (string, error) {
	clean := path.Clean("/" + filepath.ToSlash(rel))
	if clean == "/" {
		return "", errors.New("kein Pfad angegeben")
	}
	return filepath.Join(s.Dir, filepath.FromSlash(clean)), nil
}

// entries sind die Fassungen eines Ordners, die neueste zuerst. Der
// Zeitstempel im Namen hat feste Breite, also sortiert er sich von selbst.
func entries(dir string) []string {
	des, err := os.ReadDir(dir)
	if err != nil {
		return nil
	}
	var out []string
	for _, d := range des {
		if !d.IsDir() && nameRe.MatchString(d.Name()) {
			out = append(out, d.Name())
		}
	}
	sort.Sort(sort.Reverse(sort.StringSlice(out)))
	return out
}
