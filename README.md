# mda — Markdown mit Randmarken

`mda` öffnet einen Ordner voller Markdown-Dateien in einem lokalen Editor im
Browser. Alles, was du darin änderst, landet in der Datei zwischen zwei Marken:

```
----Start Annottation [christianluis] [2026-08-22T13:24:37+02:00]
Ein Absatz zur Einordnung. Er enthält **fett**, *kursiv* und `code`. Ergänzung.
----End Annottation [christianluis] [2026-08-22T13:24:37+02:00]
```

Damit sieht eine KI, mit der du dich über das Dokument abstimmst, auf den
ersten Blick, welche Passagen von dir stammen und wann du sie angefasst hast —
ohne dass du erklären musst, was du geändert hast.

Im Editor selbst tauchen die Markenzeilen nicht auf. Dort steht stattdessen
ein Balken im Seitenrand, daneben Kürzel und Zeitpunkt, so wie eine
Änderungsmarke in einer Korrekturfahne.

## Installation

Über Homebrew:

```sh
brew tap christianluis/mda https://github.com/christianluis/mdannotate
brew install --HEAD christianluis/mda/mda
```

Ohne Homebrew:

```sh
make install            # nach /usr/local/bin
make install PREFIX=~/.local
```

## Aktualisieren

Es gibt noch keine festen Versionen, `mda` wird aus dem Git-Stand gebaut. Für
solche Installationen prüft Homebrew nur auf ausdrückliche Aufforderung, ob es
neue Commits gibt — `brew upgrade` allein meldet nichts:

```sh
brew update                       # holt den Stand des Taps
brew upgrade --fetch-HEAD mda     # baut neu, wenn es neuere Commits gibt
```

Ob überhaupt etwas anliegt, sagt:

```sh
brew outdated --fetch-HEAD mda
# christianluis/mda/mda (HEAD-7d24d94) < HEAD-71285ee
```

`mda -version` nennt den Commit, aus dem die installierte Fassung gebaut wurde.

Sobald es Versionen mit Tag gibt, entfällt `--fetch-HEAD`. Dafür in
`Formula/mda.rb` die beiden auskommentierten Zeilen für `url` und `sha256`
füllen; den Prüfwert liefert `curl -sL <url> | shasum -a 256`. Danach genügt
`brew upgrade mda`.

## Benutzung

```sh
mda .                   # aktuellen Ordner öffnen
mda docs                # einen bestimmten Ordner
mda . -port 7333        # fester Port statt eines freien
mda . -no-open          # Browser nicht selbst öffnen
mda . -user "cl"        # anderer Name in den Marken
```

```
  mda 0.1.0

  Adresse   http://localhost:56825/?t=09d65a935c6f54153b35900d64283f520adcf5b94b281d54
  Ordner    ~/Projekte/mdannotate/beispiel  · 2 Markdown-Dateien
  Marken    als christianluis

  Beenden mit Strg-C.

  15:38:21  gesichert        konzept.md  · 2 Marken
  15:38:22  extern geändert  notizen/termin.md
  15:38:40  gesichert        konzept.md  · 2 Marken · ohne neue Marke
```

Der Browser öffnet sich von selbst. Links stehen alle `.md`-Dateien unterhalb
des Ordners, rechts die aufgerufene Datei — gesetzt, nicht als Quelltext.
Die Zahl hinter einem Dateinamen sagt, wie viele Passagen darin markiert sind.

Gesichert wird von allein, kurz nachdem du aufhörst zu tippen, oder sofort mit
`⌘S`. Ändert eine KI die Datei währenddessen von außen, lädt `mda` sie nach;
hast du selbst ungesicherte Änderungen, fragt es vorher nach.

Oben rechts steht ein Schalter, **Marken an** oder **Marken aus**. Bei „aus“
geht dein Text ohne Start- und Endmarke in die Datei — für Tippfehler und
Kleinkram, den niemand nachvollziehen muss. Marken, die schon in der Datei
stehen, bleiben dabei unangetastet und wandern mit ihren Zeilen mit. Die
Einstellung gilt für den nächsten Speichervorgang und merkt sich der Browser.

`mda` lauscht nur auf `127.0.0.1` und verlangt für jeden Zugriff ein Token,
das beim Start erzeugt und in der geöffneten Adresse mitgegeben wird.

## Schreiben

Auszeichnungen entstehen beim Tippen, die Markdown-Zeichen verschwinden dabei:

| Eingabe | Ergebnis |
| --- | --- |
| `#` … `######` + Leertaste | Überschrift, sechs Ebenen |
| `>` + Leertaste | Zitat; im Zitat noch einmal für eine Ebene tiefer |
| `-`, `*`, `+` + Leertaste | Aufzählung |
| `1.` + Leertaste | nummerierte Liste |
| `[ ]` oder `[x]` + Leertaste | Aufgabe mit Kästchen |
| ` ```go ` + Enter | Codeblock |
| `---` + Enter | Trennlinie |
| `**fett**`, `*kursiv*`, `` `code` ``, `~~weg~~` | beim Schließen |
| `[Text](adresse)` | Verweis |

Die Blockauslöser greifen am Anfang eines Absatzes. Enter teilt den Block,
Enter in einem leeren Listenpunkt oder Zitat geht eine Ebene heraus, Rücktaste
am Blockanfang nimmt die Auszeichnung zurück. `Tab` rückt Listenpunkte ein.

| Taste | |
| --- | --- |
| `⌘S` | sofort sichern |
| `⌘Z` / `⇧⌘Z` | rückgängig / wiederherstellen |
| `⌘B` `⌘I` `⌘K` | fett, kursiv, Verweis |
| `⌘P` | Dateien filtern |
| `⇧Enter` | Zeilenumbruch, im Codeblock: Block verlassen |

## Wie die Marken gesetzt werden

Beim Sichern vergleicht `mda` die Datei zeilenweise mit dem, was im Editor
steht, und fasst jede geänderte Passage ein. Dabei gilt:

* Unberührte Bereiche werden Zeichen für Zeichen so zurückgeschrieben, wie sie
  hereinkamen — keine Umformatierung, keine verschobenen Umbrüche.
* Die Marken sitzen direkt am Text; Leerzeilen bleiben außerhalb.
* Bearbeitest du eine bereits markierte Passage erneut, entsteht keine
  Verschachtelung; die vorhandene Marke bekommt den neuen Zeitstempel.
* Direkt aneinandergrenzende Marken verschmelzen zu einer.
* Löschst du etwas, bleibt ein Markenpaar ohne Inhalt an der Fundstelle
  zurück. Reine Leerzeilen erzeugen keine Marke.
* Marken innerhalb eines Codeblocks sind Beispieltext und bleiben es.
* Steht der Schalter auf „Marken aus“, entfällt nur das Einfassen der neuen
  Änderung; alles andere gilt unverändert.

Das Wort `Annottation` ist genau so geschrieben wie in der Aufgabenstellung.
Es steht als Konstante in `internal/annotate/annotate.go`.

## Gestaltung

Das Dokument steht in der Serife des Systems, der Rahmen drumherum in der
Grotesk des Systems — beides ohne Schriftdateien von außen. Hell ist der
Standard; oben links schaltest du auf dunkel um.

Alle Schriftgrade stehen im Verhältnis des Goldenen Schnitts. Der Schritt ist
√φ ≈ 1,272, jeder zweite Schritt also φ ≈ 1,618:

| | Größe | zur Grundschrift |
| --- | ---: | --- |
| h1 | 44,5 px | φ² |
| h2 | 35,0 px | φ^1,5 |
| h3 | 27,5 px | φ |
| h4 | 21,6 px | √φ |
| h5 / Fließtext | 17,0 px | 1 |
| h6 / Kleintext | 13,4 px | 1 / √φ |

Zeilenabstand 1,618, Überschriften 1,618 / √φ. Die Abstände laufen in
φ-Schritten (0,236 · 0,382 · 0,618 · 1 · 1,618 · 2,618 · 4,236 rem), und die
Spalten stehen 21 zu 34 — auch das wieder φ.

## Aufbau

```
cmd/mda/            Programmstart, Kommandozeile, Terminalausgabe
internal/annotate/  Marken lesen, Diff, Datei neu schreiben
internal/server/    JSON-API, Dateibaum, Live-Aktualisierung
web/                eingebettete Oberfläche (HTML, CSS, zwei Module)
```

Die Oberfläche liegt per `go:embed` in der Binary; es gibt keine Abhängigkeiten
außerhalb der Standardbibliothek. `mda.ed.serialize().text` in der
Browserkonsole zeigt, was beim nächsten Sichern in der Datei stehen wird.

```sh
make test     # Tests für das Markenmodell
make dist     # Binaries für macOS und Linux
```
