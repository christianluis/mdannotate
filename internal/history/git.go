package history

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"time"
)

// maxBlob begrenzt, was aus einem Commit geholt wird.
const maxBlob = 8 << 20

// recSep trennt die Commits im Log. Ein Nullbyte ginge nicht: es beendet
// jedes Argument, das an ein Programm geht.
const recSep = "\x1e"

var hashRe = regexp.MustCompile(`^[0-9a-f]{7,40}$`)

// Repo ist das Git-Archiv, in dem der geoeffnete Ordner liegt.
type Repo struct {
	Dir string // der geoeffnete Ordner; git findet das Archiv von dort aus
	Top string // die Wurzel des Archivs
}

// FindRepo sagt, ob der Ordner in einem Git-Archiv liegt. Ohne Git oder
// ohne Archiv ist das Ergebnis nil, und mda kommt auch damit zurecht.
func FindRepo(root string) *Repo {
	if _, err := exec.LookPath("git"); err != nil {
		return nil
	}
	out, err := run(root, "rev-parse", "--show-toplevel")
	if err != nil {
		return nil
	}
	top := strings.TrimSpace(out)
	if top == "" {
		return nil
	}
	return &Repo{Dir: root, Top: top}
}

// commit ist ein Eintrag aus dem Git-Log samt dem Pfad, unter dem die Datei
// in diesem Commit steht — bei Umbenennungen ist das nicht immer derselbe.
type commit struct {
	hash, file, when, author, subject string
}

// Versions listet die Commits, die diese Datei angefasst haben, den
// juengsten zuerst.
func (r *Repo) Versions(rel string, limit int) []Version {
	cs := r.commits(rel, limit)
	out := make([]Version, 0, len(cs))
	for _, c := range cs {
		note := c.hash[:7]
		if c.author != "" {
			note += " · " + c.author
		}
		out = append(out, Version{
			ID:    "git:" + c.hash,
			Kind:  KindGit,
			Time:  c.when,
			Label: c.subject,
			Note:  note,
		})
	}
	return out
}

// Show holt den Inhalt der Datei aus einem Commit.
func (r *Repo) Show(rel, hash string) (string, error) {
	if r == nil {
		return "", errors.New("kein Git-Archiv")
	}
	if !hashRe.MatchString(hash) {
		return "", errors.New("ungueltiger Commit")
	}
	file := ""
	for _, c := range r.commits(rel, 400) {
		if c.hash == hash {
			file = c.file
			break
		}
	}
	if file == "" {
		// Nicht im Log gefunden: unter demselben Pfad versuchen. Der Punkt
		// sagt Git, dass der Pfad vom geoeffneten Ordner aus gilt.
		file = "./" + filepath.ToSlash(rel)
	}
	out, err := run(r.Dir, "show", "--no-color", hash+":"+file)
	if err != nil {
		return "", err
	}
	return out, nil
}

// commits liest das Log einer einzelnen Datei. --follow verfolgt sie ueber
// Umbenennungen hinweg, --name-status verraet den Pfad je Commit.
func (r *Repo) commits(rel string, limit int) []commit {
	if r == nil || rel == "" {
		return nil
	}
	out, err := run(r.Dir,
		"log", "--follow", "--no-color", "-M",
		fmt.Sprintf("-n%d", limit),
		"--format="+recSep+"%H\x1f%aI\x1f%an\x1f%s",
		"--name-status", "--", rel)
	if err != nil {
		return nil
	}

	var cs []commit
	for _, line := range strings.Split(out, "\n") {
		if strings.HasPrefix(line, recSep) {
			f := strings.Split(strings.TrimPrefix(line, recSep), "\x1f")
			if len(f) < 4 {
				continue
			}
			cs = append(cs, commit{hash: f[0], when: f[1], author: f[2], subject: f[3]})
			continue
		}
		if len(cs) == 0 || cs[len(cs)-1].file != "" || !strings.Contains(line, "\t") {
			continue
		}
		// "A\tpfad" oder bei einer Umbenennung "R096\talt\tneu" — gemeint
		// ist immer der Pfad, unter dem die Datei in diesem Commit steht.
		f := strings.Split(line, "\t")
		cs[len(cs)-1].file = f[len(f)-1]
	}
	for i := range cs {
		if cs[i].subject == "" {
			cs[i].subject = "ohne Betreff"
		}
	}
	return cs
}

// run ruft git auf; nach fuenf Sekunden ist Schluss.
func run(dir string, args ...string) (string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	// quotepath aus, sonst kommen Umlaute in Pfaden als Oktalfolgen zurueck.
	full := append([]string{"-C", dir, "-c", "core.quotepath=false"}, args...)
	cmd := exec.CommandContext(ctx, "git", full...)
	cmd.Env = append(os.Environ(), "GIT_OPTIONAL_LOCKS=0", "GIT_TERMINAL_PROMPT=0", "GIT_PAGER=cat")

	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		msg := strings.TrimSpace(stderr.String())
		if msg == "" {
			msg = err.Error()
		}
		return "", errors.New(msg)
	}
	if stdout.Len() > maxBlob {
		return "", errors.New("die Fassung ist zu groß")
	}
	return stdout.String(), nil
}
