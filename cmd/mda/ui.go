package main

import (
	"fmt"
	"os"
	"strings"
	"time"
)

// Farbe nur, wenn wirklich ein Terminal zuhoert.
var colored = func() bool {
	if os.Getenv("NO_COLOR") != "" || os.Getenv("TERM") == "dumb" {
		return false
	}
	info, err := os.Stdout.Stat()
	return err == nil && info.Mode()&os.ModeCharDevice != 0
}()

func paint(code, s string) string {
	if !colored || s == "" {
		return s
	}
	return "\x1b[" + code + "m" + s + "\x1b[0m"
}

func bold(s string) string   { return paint("1", s) }
func dim(s string) string    { return paint("2", s) }
func accent(s string) string { return paint("38;5;104", s) }

// pad fuellt vor dem Einfaerben auf, damit die Steuerzeichen nicht mitzaehlen.
func pad(s string, n int) string {
	if len([]rune(s)) >= n {
		return s
	}
	return s + strings.Repeat(" ", n-len([]rune(s)))
}

func short(path string) string {
	if home, err := os.UserHomeDir(); err == nil && strings.HasPrefix(path, home+string(os.PathSeparator)) {
		return "~" + path[len(home):]
	}
	return path
}

func files(n int) string {
	if n == 1 {
		return "1 Markdown-Datei"
	}
	return fmt.Sprintf("%d Markdown-Dateien", n)
}

func marks(n int) string {
	if n == 1 {
		return "1 Marke"
	}
	return fmt.Sprintf("%d Marken", n)
}

// banner ist das, was beim Start im Terminal steht.
func banner(version, url, root, user string, count int) {
	row := func(label, value string) {
		fmt.Printf("  %s%s\n", dim(pad(label, 10)), value)
	}
	fmt.Println()
	fmt.Printf("  %s %s\n\n", bold("mda"), dim(version))
	row("Adresse", accent(url))
	row("Ordner", short(root)+"  "+dim("· "+files(count)))
	row("Marken", "als "+user)
	fmt.Printf("\n  %s\n\n", dim("Beenden mit Strg-C."))
}

// event ist eine Zeile im laufenden Betrieb.
func event(label, path, note string) {
	line := fmt.Sprintf("  %s  %s%s", dim(time.Now().Format("15:04:05")), pad(label, 17), path)
	if note != "" {
		line += dim("  · " + note)
	}
	fmt.Println(line)
}

func problem(format string, a ...any) {
	fmt.Fprintf(os.Stderr, "  %s %s\n", paint("31", "Fehler"), fmt.Sprintf(format, a...))
}
