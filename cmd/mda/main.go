// Command mda oeffnet einen Markdown-Ordner in einem lokalen Editor, der jede
// Aenderung mit Annotationsmarken versieht.
//
//	mda .            aktuellen Ordner oeffnen
//	mda docs -port 7333
package main

import (
	"flag"
	"fmt"
	"net/http"
	"os"
	"os/exec"
	"os/user"
	"path/filepath"
	"runtime"
	"time"

	"github.com/christianluis/mdannotate/internal/server"
)

var version = "dev"

func main() {
	var (
		port    = flag.Int("port", 0, "Port; 0 waehlt einen freien Port")
		host    = flag.String("host", "127.0.0.1", "Adresse, auf der gelauscht wird")
		who     = flag.String("user", "", "Name in den Annotationsmarken (Standard: $USER)")
		noOpen  = flag.Bool("no-open", false, "Browser nicht automatisch oeffnen")
		showVer = flag.Bool("version", false, "Version ausgeben")
	)
	flag.Usage = func() {
		fmt.Fprintf(os.Stderr, "mda öffnet einen Ordner mit Markdown-Dateien im Browser und\n")
		fmt.Fprintf(os.Stderr, "versieht jede Änderung mit Annotationsmarken.\n\n")
		fmt.Fprintf(os.Stderr, "  mda [Optionen] [Ordner]\n\n")
		flag.PrintDefaults()
	}
	flag.Parse()

	if *showVer {
		fmt.Println("mda", version)
		return
	}

	root := "."
	if flag.NArg() > 0 {
		root = flag.Arg(0)
	}

	srv, err := server.New(root, resolveUser(*who))
	if err != nil {
		fail(err)
	}

	ln, err := server.Listen(*host, *port)
	if err != nil {
		fail(err)
	}
	url := srv.URL(ln.Addr())

	srv.OnSave = func(path string, n int, marked bool) {
		note := marks(n)
		if !marked {
			note += " · ohne neue Marke"
		}
		event("gesichert", path, note)
	}
	srv.OnExternal = func(path string) { event("extern geändert", path, "") }

	go srv.Watch(700 * time.Millisecond)

	banner(version, url, srv.Root, srv.User, srv.FileCount())

	if !*noOpen {
		go openBrowser(url)
	}

	httpSrv := &http.Server{
		Handler:           srv,
		ReadHeaderTimeout: 10 * time.Second,
	}
	if err := httpSrv.Serve(ln); err != nil {
		fail(err)
	}
}

// resolveUser bestimmt den Namen, der in die Marken geschrieben wird.
func resolveUser(override string) string {
	if override != "" {
		return override
	}
	if u := os.Getenv("USER"); u != "" {
		return u
	}
	if u, err := user.Current(); err == nil && u.Username != "" {
		return filepath.Base(u.Username)
	}
	return "unbekannt"
}

func openBrowser(url string) {
	var cmd *exec.Cmd
	switch runtime.GOOS {
	case "darwin":
		cmd = exec.Command("open", url)
	case "windows":
		cmd = exec.Command("rundll32", "url.dll,FileProtocolHandler", url)
	default:
		cmd = exec.Command("xdg-open", url)
	}
	cmd.Run()
}

func fail(err error) {
	problem("%v", err)
	os.Exit(1)
}
