---
titel: Relaunch Kundenportal
status: Entwurf der KI
---

# Relaunch Kundenportal

Das bestehende Portal stammt aus 2019 und wird von rund 400 Kunden genutzt.
Die Absprungrate im Anmeldevorgang liegt bei 34 Prozent. Dieser Entwurf
schlägt vor, den Relaunch in drei Stufen zu fahren.

## Ausgangslage

- Anmeldung über Benutzername statt E-Mail
- Keine Zwei-Faktor-Anmeldung
- Rechnungsarchiv nur als Sammel-PDF
- Mobil unbenutzbar unterhalb von 480 Pixeln

## Vorschlag

| Stufe | Inhalt | Aufwand |
| --- | --- | ---: |
| 1 | Anmeldung und Zwei-Faktor | 4 Wochen |
| 2 | Rechnungsarchiv, einzeln abrufbar | 6 Wochen |
| 3 | Oberfläche neu, mobil zuerst | 10 Wochen |

Stufe 1 lässt sich ohne Eingriff in die Bestandsdaten umsetzen. Für Stufe 2
brauchen wir einen Migrationslauf über das Archiv:

```sql
SELECT kunde_id, rechnung_nr, pdf_blob
FROM sammelrechnung
WHERE erstellt_am >= '2019-01-01';
```

## Offene Punkte

- [ ] Budget für Stufe 3 klären
- [ ] Datenschutzprüfung für die Zwei-Faktor-Anmeldung
- [x] Grober Zeitrahmen mit dem Kunden abgestimmt

> Wir halten Stufe 1 für unstrittig und würden nächste Woche beginnen.

## Risiken

Der Migrationslauf ist der kritische Pfad. Wenn das Archiv größer ist als
angenommen, verschiebt sich Stufe 2 um mindestens zwei Wochen.
