// Package server stellt die eingebettete Web-App und eine kleine JSON-API
// bereit, ueber die der Editor Markdown-Dateien unterhalb eines Wurzelordners
// liest und schreibt.
package server

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"mime"
	"net"
	"net/http"
	"os"
	"path"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/christianluis/mdannotate/internal/annotate"
	"github.com/christianluis/mdannotate/internal/history"
	"github.com/christianluis/mdannotate/web"
)

// Server bedient genau einen Wurzelordner.
type Server struct {
	Root  string
	User  string
	Token string

	// Store legt jede Fassung unter ~/.mda/changes ab, Git liefert die
	// Fassungen aus dem Archiv. Beide duerfen fehlen.
	Store *history.Store
	Git   *history.Repo

	// OnSave und OnExternal melden dem Programm, was passiert ist.
	OnSave     func(path string, marks int, marked bool)
	OnExternal func(path string)

	mux   *http.ServeMux
	marks *markCache

	mu      sync.Mutex
	clients map[chan string]struct{}
	// selfWrite haelt fest, was wir selbst geschrieben haben, damit der
	// Watcher die eigene Speicherung nicht als Fremdaenderung meldet.
	selfWrite map[string]int64
}

// New baut den Server fuer einen Wurzelordner.
func New(root, user string) (*Server, error) {
	abs, err := filepath.Abs(root)
	if err != nil {
		return nil, err
	}
	abs, err = filepath.EvalSymlinks(abs)
	if err != nil {
		return nil, err
	}
	info, err := os.Stat(abs)
	if err != nil {
		return nil, err
	}
	if !info.IsDir() {
		return nil, fmt.Errorf("%s ist kein Ordner", abs)
	}

	buf := make([]byte, 24)
	if _, err := rand.Read(buf); err != nil {
		return nil, err
	}

	s := &Server{
		Root:      abs,
		User:      user,
		Token:     hex.EncodeToString(buf),
		mux:       http.NewServeMux(),
		marks:     newMarkCache(),
		clients:   map[chan string]struct{}{},
		selfWrite: map[string]int64{},
	}
	// Beides ist Beiwerk: ohne Heimatordner gibt es keine Ablage, ohne
	// Archiv keine Commits, und mda laeuft trotzdem.
	s.Store, _ = history.Open(abs)
	s.Git = history.FindRepo(abs)

	s.routes()
	return s, nil
}

// Changes ist der Ordner, in dem die Fassungen dieser Sitzung liegen.
func (s *Server) Changes() string {
	if s.Store == nil {
		return ""
	}
	return s.Store.Dir
}

func (s *Server) routes() {
	s.mux.HandleFunc("/api/config", s.guard(s.handleConfig))
	s.mux.HandleFunc("/api/tree", s.guard(s.handleTree))
	s.mux.HandleFunc("/api/file", s.guard(s.handleFile))
	s.mux.HandleFunc("/api/versions", s.guard(s.handleVersions))
	s.mux.HandleFunc("/api/version", s.guard(s.handleVersion))
	s.mux.HandleFunc("/api/events", s.guard(s.handleEvents))
	s.mux.HandleFunc("/asset", s.guard(s.handleAsset))
	s.mux.HandleFunc("/", s.handleStatic)
}

func (s *Server) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	s.mux.ServeHTTP(w, r)
}

// guard laesst nur Anfragen mit dem Sitzungstoken durch und weist
// fremde Origins ab, damit keine Webseite im Browser mitlesen kann.
func (s *Server) guard(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		tok := r.Header.Get("X-Mda-Token")
		if tok == "" {
			tok = r.URL.Query().Get("t")
		}
		if tok != s.Token {
			http.Error(w, "ungueltiges Token", http.StatusForbidden)
			return
		}
		if o := r.Header.Get("Origin"); o != "" && !strings.HasSuffix(o, "//"+r.Host) {
			http.Error(w, "fremder Origin", http.StatusForbidden)
			return
		}
		next(w, r)
	}
}

// --- statische Dateien ---------------------------------------------------

func (s *Server) handleStatic(w http.ResponseWriter, r *http.Request) {
	name := strings.TrimPrefix(r.URL.Path, "/")
	if name == "" {
		name = "index.html"
	}
	b, err := fs.ReadFile(web.FS, name)
	if err != nil {
		http.NotFound(w, r)
		return
	}
	if ct := mime.TypeByExtension(filepath.Ext(name)); ct != "" {
		w.Header().Set("Content-Type", ct)
	}
	w.Write(b)
}

// --- API -----------------------------------------------------------------

func writeJSON(w http.ResponseWriter, v any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	json.NewEncoder(w).Encode(v)
}

func (s *Server) handleConfig(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, map[string]any{
		"root": s.Root,
		"name": filepath.Base(s.Root),
		"user": s.User,
	})
}

func (s *Server) handleTree(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, s.buildTree())
}

// resolve prueft einen relativen Pfad und liefert den absoluten Pfad zurueck.
func (s *Server) resolve(rel string) (string, error) {
	if rel == "" {
		return "", errors.New("kein Pfad angegeben")
	}
	clean := path.Clean("/" + filepath.ToSlash(rel))
	abs := filepath.Join(s.Root, filepath.FromSlash(clean))
	if abs != s.Root && !strings.HasPrefix(abs, s.Root+string(os.PathSeparator)) {
		return "", errors.New("Pfad liegt ausserhalb des Wurzelordners")
	}
	// Symlinks duerfen nicht aus dem Wurzelordner herausfuehren.
	if real, err := filepath.EvalSymlinks(abs); err == nil {
		if real != s.Root && !strings.HasPrefix(real, s.Root+string(os.PathSeparator)) {
			return "", errors.New("Symlink zeigt aus dem Wurzelordner heraus")
		}
	}
	return abs, nil
}

type fileResponse struct {
	Path    string            `json:"path"`
	Text    string            `json:"text"`
	Regions []annotate.Region `json:"regions"`
	Mod     int64             `json:"mod"`
	Marks   int               `json:"marks"`
}

func (s *Server) handleFile(w http.ResponseWriter, r *http.Request) {
	rel := r.URL.Query().Get("path")
	abs, err := s.resolve(rel)
	if err != nil || !isMarkdown(abs) {
		http.Error(w, "ungueltiger Pfad", http.StatusBadRequest)
		return
	}

	switch r.Method {
	case http.MethodGet:
		raw, err := os.ReadFile(abs)
		if err != nil {
			http.Error(w, err.Error(), http.StatusNotFound)
			return
		}
		writeJSON(w, s.respond(rel, abs, annotate.Parse(string(raw))))

	case http.MethodPut:
		var body struct {
			Text string `json:"text"`
			Mod  int64  `json:"mod"`
			// Annotate schaltet die Marken ab, wenn es ausdruecklich
			// false ist. Fehlt das Feld, wird wie immer markiert.
			Annotate *bool `json:"annotate"`
		}
		if err := json.NewDecoder(io.LimitReader(r.Body, 32<<20)).Decode(&body); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}

		s.mu.Lock()
		defer s.mu.Unlock()

		old, err := os.ReadFile(abs)
		if err != nil && !os.IsNotExist(err) {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		if info, serr := os.Stat(abs); serr == nil && body.Mod != 0 && info.ModTime().UnixMilli() != body.Mod {
			// Die Datei wurde zwischendurch von aussen geaendert.
			w.WriteHeader(http.StatusConflict)
			writeJSON(w, s.respond(rel, abs, annotate.Parse(string(old))))
			return
		}

		marked := body.Annotate == nil || *body.Annotate
		var doc annotate.Doc
		if marked {
			doc = annotate.Apply(string(old), body.Text, s.User, time.Now())
		} else {
			doc = annotate.ApplyPlain(string(old), body.Text)
		}
		// Erst den Stand festhalten, wie mda ihn vorfand, dann schreiben und
		// die neue Fassung ablegen. Gleiches legt der Store kein zweites Mal ab.
		if err == nil {
			s.Store.Save(rel, string(old), history.KindStart)
		}
		if err := writeAtomic(abs, doc.Raw()); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		s.Store.Save(rel, doc.Raw(), history.KindMda)
		if info, serr := os.Stat(abs); serr == nil {
			s.selfWrite[rel] = info.ModTime().UnixMilli()*997 + info.Size()
		}
		if s.OnSave != nil {
			s.OnSave(rel, len(doc.Regions), marked)
		}
		writeJSON(w, s.respond(rel, abs, doc))

	default:
		http.Error(w, "Methode nicht erlaubt", http.StatusMethodNotAllowed)
	}
}

func (s *Server) respond(rel, abs string, d annotate.Doc) fileResponse {
	var mod int64
	if info, err := os.Stat(abs); err == nil {
		mod = info.ModTime().UnixMilli()
	}
	regions := d.Regions
	if regions == nil {
		regions = []annotate.Region{}
	}
	return fileResponse{Path: rel, Text: d.Text(), Regions: regions, Mod: mod, Marks: len(regions)}
}

// writeAtomic schreibt ueber eine temporaere Datei im selben Ordner, damit
// bei einem Absturz nie eine halbe Datei zurueckbleibt.
func writeAtomic(abs, content string) error {
	dir := filepath.Dir(abs)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	mode := os.FileMode(0o644)
	if info, err := os.Stat(abs); err == nil {
		mode = info.Mode().Perm()
	}
	tmp, err := os.CreateTemp(dir, ".mda-*.tmp")
	if err != nil {
		return err
	}
	name := tmp.Name()
	defer os.Remove(name)

	if _, err := tmp.WriteString(content); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Sync(); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	if err := os.Chmod(name, mode); err != nil {
		return err
	}
	return os.Rename(name, abs)
}

// handleAsset liefert Bilder, auf die eine Markdown-Datei relativ verweist.
func (s *Server) handleAsset(w http.ResponseWriter, r *http.Request) {
	abs, err := s.resolve(r.URL.Query().Get("path"))
	if err != nil {
		http.Error(w, "ungueltiger Pfad", http.StatusBadRequest)
		return
	}
	http.ServeFile(w, r, abs)
}

// --- Live-Aktualisierung -------------------------------------------------

func (s *Server) handleEvents(w http.ResponseWriter, r *http.Request) {
	flusher, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "Streaming nicht unterstuetzt", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Connection", "keep-alive")

	ch := make(chan string, 16)
	s.mu.Lock()
	s.clients[ch] = struct{}{}
	s.mu.Unlock()
	defer func() {
		s.mu.Lock()
		delete(s.clients, ch)
		s.mu.Unlock()
	}()

	fmt.Fprint(w, "retry: 1000\n\n")
	flusher.Flush()

	ping := time.NewTicker(25 * time.Second)
	defer ping.Stop()
	for {
		select {
		case <-r.Context().Done():
			return
		case msg := <-ch:
			fmt.Fprintf(w, "data: %s\n\n", msg)
			flusher.Flush()
		case <-ping.C:
			fmt.Fprint(w, ": ping\n\n")
			flusher.Flush()
		}
	}
}

func (s *Server) broadcast(msg string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for ch := range s.clients {
		select {
		case ch <- msg:
		default:
		}
	}
}

// Watch meldet Aenderungen, die ausserhalb des Editors passieren, damit der
// Browser eine von der KI geschriebene Datei sofort nachlaedt.
func (s *Server) Watch(interval time.Duration) {
	prev := s.snapshot()
	for range time.Tick(interval) {
		cur := s.snapshot()

		var changed []string
		structural := len(cur) != len(prev)
		for p, v := range cur {
			old, ok := prev[p]
			if !ok {
				structural = true
				continue
			}
			if old != v {
				changed = append(changed, p)
			}
		}
		for p := range prev {
			if _, ok := cur[p]; !ok {
				structural = true
			}
		}

		s.mu.Lock()
		var external []string
		for _, p := range changed {
			if s.selfWrite[p] == cur[p] {
				continue // das waren wir selbst
			}
			external = append(external, p)
		}
		s.mu.Unlock()

		if structural {
			s.broadcast(`{"type":"tree"}`)
		}
		for _, p := range external {
			if s.OnExternal != nil {
				s.OnExternal(p)
			}
			// Auch was von aussen kommt, gehoert in den Verlauf: sonst ist
			// die Fassung weg, sobald die naechste Hand darueber geht.
			if abs, rerr := s.resolve(p); rerr == nil {
				if b, ferr := os.ReadFile(abs); ferr == nil {
					s.Store.Save(p, string(b), history.KindExtern)
				}
			}
			b, _ := json.Marshal(map[string]string{"type": "file", "path": p})
			s.broadcast(string(b))
		}
		prev = cur
	}
}

// FileCount sagt, wie viele Markdown-Dateien unter dem Wurzelordner liegen.
func (s *Server) FileCount() int { return len(s.snapshot()) }

// Listen bindet auf localhost und liefert die URL inklusive Token.
func Listen(host string, port int) (net.Listener, error) {
	return net.Listen("tcp", net.JoinHostPort(host, fmt.Sprint(port)))
}

// URL ist die Adresse, die im Browser geoeffnet wird.
func (s *Server) URL(addr net.Addr) string {
	tcp, ok := addr.(*net.TCPAddr)
	if !ok {
		return ""
	}
	host := tcp.IP.String()
	if tcp.IP.IsUnspecified() || tcp.IP.IsLoopback() {
		host = "localhost"
	}
	return fmt.Sprintf("http://%s:%d/?t=%s", host, tcp.Port, s.Token)
}
