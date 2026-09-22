[English](README.md) | Deutsch | [Español](README.es.md) | [Français](README.fr.md) | [日本語](README.ja.md) | [한국어](README.ko.md) | [Русский](README.ru.md) | [简体中文](README.zh-CN.md) | [繁體中文](README.zh-TW.md)

# ChannelTerm

> Dieses Dokument ist der deutschsprachige Einstieg. Für Verhalten, Optionen und Kompatibilität
> sind das englische [README](README.md), der aktuelle Quellcode und `channelterm --help`
> maßgeblich.

Mit ChannelTerm teilen Menschen und KI dieselbe Hardware-Terminal-Session. Die KI ersetzt Ihr
Terminal nicht: Sie kann das Gerät lesen und bedienen, während Sie verbunden bleiben, ihre Aktionen
sehen, selbst Eingaben machen und bei Bedarf eingreifen.

```text
                    Physisches serielles Gerät
                              |
                       Serial Transport
                        (eine Verbindung)
                              |
                              v
                  ChannelTerm Session SER-1
                     /                 \
                    /                   \
          Menschliches Terminal      KI-/MCP-Client
          channelterm attach         Lese-/Schreibwerkzeuge
```

ChannelTerm implementiert derzeit serielle Kommunikation unter Windows, Linux und macOS. SSH und
Telnet sind zukünftige Richtungen, keine aktuellen Funktionen. ChannelTerm enthält keine eigene KI;
es stellt externen KI-Clients echte Terminal-Sessions über MCP bereit.

## Schnellstart: ein Befehl, ein Terminal

Listen Sie native serielle Ziele auf, ohne dass bereits ein MCP-Server laufen muss:

```bash
channelterm list --kind device --transport serial --no-mcp
```

Überspringen Sie diesen Schritt, wenn das native Ziel, zum Beispiel `COM50`, bereits bekannt ist.

Soll ein KI-Client der Session beitreten, konfigurieren Sie vor dem Verbinden einmalig einen
unterstützten Client:

```bash
channelterm init --mcp
```

Wählen Sie HTTP oder drücken Sie Enter für die HTTP-Vorgabe. Die Konfiguration enthält den lokalen
Bearer-Zugang. Laden oder starten Sie den KI-Client neu, falls er die Änderung nicht sofort erkennt.

Führen Sie anschließend nur den Befehl für Ihre Plattform aus:

```bash
# Windows
channelterm attach COM8 --baud 115200 --label board

# Linux
channelterm attach /dev/ttyUSB0 --baud 115200 --label board

# macOS
channelterm attach /dev/cu.usbserial-110 --baud 115200 --label board
```

Dieser einzelne `attach`-Befehl öffnet das serielle Gerät und verbindet das aktuelle Terminal. Wenn
am Standard-Endpunkt kein kompatibler Host läuft, startet er automatisch einen temporären lokalen
HTTP-MCP-Session-Host. Eine typische Ausgabe lautet:

```text
Shared Session created: SER-1 (0123456789abcdef0123456789abcdef)
[ChannelTerm] Temporary Session Host started; it and all shared Sessions stop when this attachment exits. Run 'channelterm mcp --transport http' separately for a persistent Host.
```

Für diesen schnellen Ablauf ist kein separates MCP-Server-Terminal nötig. Solange die Verbindung
offen bleibt, kann ein konfigurierter HTTP-MCP-Client `http://127.0.0.1:37099/mcp` verwenden und
dieselbe Session `SER-1` bedienen. Der temporäre Host und alle von ihm verwalteten Sessions enden,
wenn die erzeugende Verbindung beendet wird. Verwenden Sie einen dauerhaften Host nur dann, wenn
Sessions unabhängig von diesem Terminal weiterlaufen müssen.

`--label board` ist ein optionaler Anzeigename, keine Kennung. Andere Clients müssen `SER-1` oder die
vollständige undurchsichtige Session-ID verwenden.

### Häufige Abläufe

| Ziel | Befehl |
| --- | --- |
| Eine gemeinsam genutzte serielle Session öffnen und automatisch einen temporären Host starten | `channelterm attach COM8 --baud 115200` |
| Einen unterstützten KI-Client für den gemeinsamen HTTP-Host konfigurieren | `channelterm init --mcp` |
| Host und Sessions unabhängig vom Terminal aktiv halten | `channelterm mcp --transport http` |
| Vorhandene Sessions auflisten oder ihnen beitreten | `channelterm list --kind session`, danach `channelterm attach SER-1` |
| Eine private serielle Verbindung ohne MCP-Freigabe öffnen | `channelterm attach COM8 --private --baud 115200` |
| Strukturierte Session-Ereignisse beobachten | `channelterm events SER-1` |
| Das geführte Dateiübertragungsmenü in der aktuellen gemeinsamen Verbindung öffnen | `Ctrl+]` und danach `f` drücken |
| Über eine gemeinsame Session senden oder empfangen | `channelterm file send firmware.bin --session SER-1` oder `channelterm file receive /tmp/log.txt ./log.txt --session SER-1` |

Ein dauerhafter Host ist ein separater, lang laufender Prozess. Nachdem
`MCP Streamable HTTP listening on http://127.0.0.1:37099/mcp` erscheint, können andere Shells und
konfigurierte KI-Clients Sessions erstellen oder ihnen beitreten. Keine weitere Ausgabe nach dieser
Bereitschaftsmeldung ist normal. Beenden Sie den Host und alle seine Sessions mit `Ctrl+C`.

## Interaktive Tasten

```text
Ctrl+C      0x03 an das entfernte Terminal senden
Ctrl+] q    Dieses CLI verlassen; ein von ihm gestarteter temporärer Host endet ebenfalls
Ctrl+] ?    Lokale Hilfe zu Escape-Befehlen anzeigen
Ctrl+] ]    Ein wörtliches Ctrl+]-Byte an das entfernte Gerät senden
Ctrl+] t    Lokale Zeitstempel für Shell-Prompts umschalten
Ctrl+] f    Lokales Menü zum Senden/Empfangen von Dateien öffnen
Ctrl+] Esc  Lokalen Escape-Modus abbrechen
```

## Eine KI mit derselben Session verbinden

Führen Sie aus:

```bash
channelterm init --mcp
```

Nach Auswahl von HTTP verwenden unterstützte Codex-, Claude-Code-, OpenCode- oder Zoo-Code-Clients
den lokalen gemeinsamen Endpunkt `http://127.0.0.1:37099/mcp`. Die KI kann `SER-1` auflisten, lesen
und bedienen. Verwenden Sie an einem nachweislich inaktiven Bash-Prompt bevorzugt `terminal_exec`;
für rohe Tasten und interaktive Programme ist `terminal_write` vorgesehen.

Der HTTP-Host benötigt ein Bearer-Token und lauscht standardmäßig nur auf der Loopback-Adresse. Die
integrierten CLI-Clients lesen das lokale Benutzertoken automatisch. Der Host kann auch
[in einem vertrauenswürdigen LAN lauschen](docs/getting-started/mcp-server.md#listen-on-a-lan).
Entfernte Clients müssen seine erreichbare Adresse verwenden und dasselbe Token im
`Authorization`-Header senden. Stellen Sie den Endpunkt nicht ohne TLS und zusätzliche
Netzwerkzugriffskontrollen in einem nicht vertrauenswürdigen Netzwerk bereit.

## Dateiübertragung

Eine gemeinsame Session kann Dateien und Verzeichnisse senden oder empfangen. Drücken Sie in einem
aktiven gemeinsamen `attach` zuerst `Ctrl+]` und danach `f`, um das geführte Sende-/Empfangsmenü zu
öffnen. Dieselben Vorgänge stehen auch als Befehle zur Verfügung:

```bash
channelterm file send firmware.bin --session SER-1
channelterm file receive /tmp/log.txt ./log.txt --session SER-1
channelterm file send ./build/release /tmp/release --session SER-1
```

Wird beim Senden kein entferntes Ziel angegeben, landet die Datei oder das Verzeichnis standardmäßig
unter `/tmp/cterm/mcp-files/`. Der interaktive Kurzbefehl verwendet
`/tmp/cterm/user-files/`. ChannelTerm erstellt fehlende Verzeichnisse und wählt Suffixe wie `_1`
oder `_2`, damit vorhandene Inhalte nicht überschrieben werden.

Das Ziel muss eine Linux-Shell mit den erforderlichen Standardbefehlen sein. Voraussetzungen,
Überschreibschutz, Abbruch und SHA-256-Prüfung beschreibt der
[Ablauf zur Dateiübertragung](docs/getting-started/file-transfer.md).

## Installation und Build

ChannelTerm benötigt Go 1.25 oder neuer. Verwenden Sie eine aktuell unterstützte Patch-Version und
erstellen Sie das Programm im Stammverzeichnis des Repositorys:

```bash
git clone https://github.com/akira-init1/ChannelTerm.git
cd ChannelTerm
go build ./cmd/channelterm
./channelterm install
```

`install` kopiert die laufende Binärdatei in den Standard-Programmpfad des aktuellen Benutzers,
erstellt die Befehle `channelterm` und `cterm`, ergänzt den Benutzer-PATH nur bei Bedarf und erzeugt
eine fehlende minimale Konfiguration. Öffnen Sie nach einer PATH-Änderung ein neues Terminal.
`channelterm uninstall` entfernt die vom Installer verwalteten Befehle und die PATH-Änderung, behält
aber Benutzerdaten. `channelterm uninstall --purge` verlangt eine ausdrückliche Bestätigung, bevor
Konfiguration, Gerätestatus und lokale HTTP-Anmeldedaten gelöscht werden.

Standardpfade; der Installer zeigt nach jedem Lauf auch die tatsächlich verwendeten Pfade an:

| Plattform | Befehlsverzeichnis | Installationsstatus | Standardkonfiguration |
| --- | --- | --- | --- |
| Windows | `%LOCALAPPDATA%\Programs\ChannelTerm\bin` | `%LOCALAPPDATA%\ChannelTerm\install.json` | `%APPDATA%\channelterm\config.toml` |
| Linux | `~/.local/bin` | `$XDG_STATE_HOME/channelterm/install.json` oder `~/.local/state/channelterm/install.json` | `$XDG_CONFIG_HOME/channelterm/config.toml` oder `~/.config/channelterm/config.toml` |
| macOS | `~/.local/bin` | `~/Library/Application Support/channelterm/install.json` | `~/Library/Application Support/channelterm/config.toml` |

Unterstützte Desktop-Ziele sind Windows, Linux und macOS auf amd64/arm64. Weitere Informationen:
[Aus dem Quellcode bauen und installieren](docs/getting-started/build.md), vollständiger Vertrag für
[`install` / `uninstall`](docs/reference/cli.md#install-and-uninstall),
[Konfigurationspfade](docs/reference/configuration.md) sowie
[Build und Tests](docs/development/building-and-testing.md).

## Konfiguration

ChannelTerm speichert lokale Dateien im Benutzer-Konfigurationsverzeichnis der jeweiligen Plattform:

| Plattform | Standardverzeichnis |
| --- | --- |
| Windows | `%AppData%\channelterm\` |
| Linux | `$XDG_CONFIG_HOME/channelterm/` oder `~/.config/channelterm/`, wenn nicht gesetzt |
| macOS | `~/Library/Application Support/channelterm/` |

`config.toml` enthält benutzerverwaltete serielle Profile und die Verbindungsrichtlinie.
`state.json` ist der von ChannelTerm verwaltete Gerätestatus. `http-auth-token` ist das Geheimnis zur
Authentifizierung von MCP-HTTP-Clients; geben Sie es nicht weiter und committen Sie es nicht.

```powershell
# Ein serielles Profil speichern und wiederverwenden.
channelterm serial --port COM8 --baud 115200 --save board
channelterm serial --profile board

# Für diesen Vorgang eine andere config.toml verwenden.
channelterm serial --config ./channelterm.toml --profile board
```

Häufige Optionen sind `--profile`, `--save`, `--config`, `--port` und `--baud`. Der MCP-Host
unterstützt außerdem `--connection-policy ask|auto|deny`. `--config` ändert nur die ausgewählte
`config.toml`, nicht die standardmäßigen Dateien `state.json` oder `http-auth-token`. Siehe
[Serielle Profile](docs/getting-started/serial-profiles.md) und
[Konfigurationsreferenz](docs/reference/configuration.md).

## Dokumentation

- [Dokumentationsindex](docs/README.md)
- [Seriellen Terminalablauf verwenden](docs/getting-started/serial-terminal.md)
- [Eine Session teilen](docs/getting-started/shared-session.md)
- [Dateien übertragen](docs/getting-started/file-transfer.md)
- [MCP-Server](docs/getting-started/mcp-server.md)
- [CLI-Referenz](docs/reference/cli.md)
- [MCP-Werkzeugreferenz](docs/reference/mcp-tools.md)
- [Kennungen und Referenzen](docs/reference/identifiers.md)

## Lizenz

ChannelTerm steht unter der [Apache License 2.0](LICENSE).
