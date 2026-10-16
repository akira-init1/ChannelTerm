[English](README.md) | [Deutsch](README.de.md) | [Español](README.es.md) | [Français](README.fr.md) | [日本語](README.ja.md) | [한국어](README.ko.md) | [Русский](README.ru.md) | [简体中文](README.zh-CN.md) | [繁體中文](README.zh-TW.md)

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

ChannelTerm unterstützt serielle Kommunikation und SSH-Shells mit PTY unter Windows, Linux und macOS. ChannelTerm enthält keine eigene KI; es stellt echte Terminal-Sessions für Menschen und externe KI-Clients über MCP bereit.

## GDB im Benutzerraum über MCP

Unter nativem Linux kann ein MCP-Client mit den `debug_start` / `debug_command` / `debug_events` / `debug_stop`-Tools eine wartende Bash-Session übernehmen, Haltepunkte setzen, schrittweise ausführen, Variablen/Threads/Register prüfen und sich wieder lösen. Dieses erste Backend setzt ChannelTerm, GDB, gdbserver und ELF in derselben Linux-Umgebung voraus. Es unterstützt einen Inferior im All-Stop-Modus; normales Schreiben wird erst nach Wiederherstellung des Terminals und erfolgreicher Shell-Prüfung mit einer eindeutigen Markierung freigegeben.

```text
debug_start -> break_insert -> continue -> debug_events -> next/step/finish -> debug_stop
```

[Ablauf, MCP-Aufrufe, sicheres Attach und lokale PTY-Testabdeckung](docs/getting-started/gdb-debugging.md).

Für ein unabhängig betriebenes GDB stellt `debug_bridge_*` eine bereits vorbereitete serielle Session über eine authentifizierte TCP-Brücke bereit. ChannelTerm startet weder GDB noch gdbserver. GDB verwendet `channelterm rsp-client`; authentifizierte Verbindungen erneuern ihre Leases auch ohne RSP-Verkehr automatisch, bei einer maximalen Laufzeit von 24 Stunden. Nach einer Trennung bleibt die exklusive Lease bis zur bestätigten Shell-/TTY-Wiederherstellung bestehen. Explizites Schließen beendet auch die Session. Der [Workflow für die externe Brücke](docs/getting-started/gdb-debugging.md#external-gdb-over-a-prepared-serial-session) beschreibt Windows/WSL2 sowie die getrennte Prüfung mit PTY und Entwicklungsboard.

Die externe Brücke unterstützt jedes Linux-Entwicklungsboard mit einem kompatiblen gdbserver über die vorbereitete serielle Verbindung und einem zum Ziel passenden GDB. Die Brücke beschränkt weder Boardmodell noch CPU-Architektur des Ziels; Bare-Metal-Debugging gehört nicht zum unterstützten Umfang.

## Dateien über SSH übertragen

Nutzen Sie SFTP für Dateiübertragung und entfernte Dateiverwaltung oder das klassische SCP für kompatible POSIX-Server:

```bash
channelterm sftp user@192.168.1.50 ls /tmp
channelterm sftp user@192.168.1.50 put ./firmware.bin /tmp/firmware.bin
channelterm sftp user@192.168.1.50 get /tmp/log.txt ./log.txt
channelterm scp user@192.168.1.50 put ./release /tmp/release --recursive
```

Diese Befehle verwenden dieselbe Schlüssel-/Passwortauthentifizierung und Hostprüfung wie SSH,
jedoch eine von Terminal-Sessions unabhängige Verbindung. Das Ersetzen vorhandener Dateien erfordert
`--overwrite`. Verzeichnisregeln, Abbruch und Grenzen beschreibt die
[Dateiübertragung](docs/getting-started/file-transfer.md#sftp-and-scp-over-ssh).

Dateiübertragungen über gemeinsame Terminal-Sessions verwenden ein kleines Linux-Shell-Protokoll.
Separate SSH-Dateiverbindungen verwenden SFTP oder das klassische SCP.

## Gemeinsame SSH-Shell

Verbinden Sie sich mit einem autorisierten privaten Schlüssel (verschlüsselte Schlüssel fragen nach einer Passphrase) oder geben Sie auf
Nachfrage Ihr Passwort ein (beides wird nicht gespeichert). Prüfen Sie bei einem unbekannten Server den
angezeigten Fingerabdruck und geben Sie `yes` ein, um seinen öffentlichen Schlüssel in
`~/.ssh/known_hosts` zu speichern; geänderte Schlüssel werden abgelehnt. Die Abfragen benötigen ein interaktives Terminal:

```bash
# Terminal 1: Shell öffnen, dann dort uname -a ausführen.
channelterm ssh user@192.168.1.50

# Terminal 2: dieselbe Shell beobachten und bedienen.
channelterm attach SSH-1
```

Lassen Sie den ersten Befehl laufen: Beim Beenden werden dieser Host und seine Sessions geschlossen. Der lokale Standardport muss frei sein; mit `--listen` und dem ausgegebenen Attach-Endpunkt verwenden Sie einen separaten Host. Der [SSH-Ablauf](docs/getting-started/ssh-terminal.md) beschreibt Schlüsselpfade, Hostprüfung, Ports und aktuelle Grenzen.

## Schnellstart: ein Befehl, ein Terminal

Listen Sie erkannte serielle Geräte, gespeicherte Profile und bei verfügbarem Standard-HTTP-Host
dessen Sessions auf:

```bash
channelterm list
```

Der Host muss nicht laufen. Ist er nicht erreichbar, meldet `list` MCP als offline und zeigt lokale
Geräte und Profile weiterhin an.

| Häufige Option | Wirkung |
| --- | --- |
| `--kind device`, `profile` oder `session` | Nur die ausgewählten Arten anzeigen; mehrere Werte können kommasepariert angegeben werden. |
| `--transport serial` | Nur serielle Ergebnisse anzeigen. |
| `--no-mcp` | Die Abfrage des MCP-Hosts überspringen und nur lokale Quellen anzeigen. |
| `--long`, `-l` | Vollständige undurchsichtige Session-IDs in die Tabelle aufnehmen. |
| `--json` | Strukturiertes JSON für Skripte ausgeben. |
| `--endpoint URL` | Sessions von einem anderen HTTP-Host abfragen. |

Den vollständigen Optionsvertrag finden Sie in der [CLI-Referenz](docs/reference/cli.md#list).

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

Während der Verbindung:

```text
Ctrl+C      0x03 an das entfernte Terminal senden
Ctrl+] q    Dieses CLI verlassen; ein von ihm gestarteter temporärer Host endet ebenfalls
Ctrl+] ?    Lokale Hilfe zu Escape-Befehlen anzeigen
Ctrl+] ]    Ein wörtliches Ctrl+]-Byte an das entfernte Gerät senden
Ctrl+] t    Lokale Zeitstempel für Shell-Prompts umschalten
Ctrl+] f    Lokales Menü zum Senden/Empfangen von Dateien öffnen
Ctrl+] Esc  Lokalen Escape-Modus abbrechen
```

Um einen unterstützten KI-Client mit demselben Host zu verbinden, führen Sie `channelterm init --mcp` aus, wählen HTTP und lassen den Client `SER-1` auflisten oder bedienen. Authentifizierung und Client-Ablauf beschreibt [MCP-Server](docs/getting-started/mcp-server.md).

Installieren Sie für wiederverwendbare Workflow-Anleitungen den mitgelieferten
[`cterm-serial-debug` Agent Skill](docs/getting-started/mcp-server.md#install-the-bundled-agent-skill)
in einem Agent-Skills-kompatiblen Client. `channelterm init --mcp` konfiguriert MCP-Clients,
installiert den Skill jedoch nicht.

Ab v0.2.0 enthalten die [Releases](https://github.com/akira-init1/ChannelTerm/releases)
`channelterm_skills_VERSION.zip`. Entpacken Sie das Archiv und kopieren Sie das vollständige
Verzeichnis `cterm-serial-debug` gemäß der verlinkten Skill-Installationsanleitung.

Die KI kann die gemeinsame Session auch selbst erstellen. Führen Sie zuerst einmal
`channelterm init --mcp` aus und wählen Sie HTTP, um den Client zu konfigurieren. Starten Sie danach
mit `channelterm mcp --transport http` den dauerhaften Host im Vordergrund und geben Sie der KI das
genaue serielle Ziel sowie die Kommunikationsparameter. Sie kann Ports auflisten,
`terminal_open_serial` aufrufen und die erzeugte Referenz `SER-N` melden, damit ein Mensch mit
`channelterm attach SER-N` beitreten kann. Das Starten des Hosts allein öffnet keinen Port: `ask`
erfordert weiterhin eine ausdrückliche Genehmigung. `auto` liefert `connect`, aber die KI muss
weiterhin `terminal_open_serial` aufrufen; der Host öffnet niemals automatisch einen Port. Beenden
Sie den Host normalerweise mit `Ctrl+C`; dabei werden die von ihm verwalteten Sessions geschlossen.

## Ein gemeinsames Terminal in der Praxis

Eine verbundene CLI zeigt einen Agent-Befehl als lokalen `AI`-Aktivitätsblock an. `terminal_exec` hält diesen Befehl aus der History eines inaktiven Bash-Prompts heraus. Die folgenden Zeilen bleiben die rohe Terminalausgabe des Geräts. Sobald die kurze Befehls-Lease freigegeben wird, kann der Mensch wieder tippen:

```bash
root@board:~#

──────── AI ────────
[18:52:31] >> echo agent-check
────────────────────
agent-check
root@board:~# echo human-still-here
human-still-here
root@board:~#
```

Die genaue Befehlsausgabe hängt vom angeschlossenen Gerät ab. Der `AI`-Block ist eine lokale Darstellung aus strukturierten Session-Ereignissen; er wird weder in den Byte-Stream des Geräts noch in die gespeicherte Terminalausgabe eingefügt. Rohe Tasteneingaben und interaktive Programme verwenden weiterhin `terminal_write` und behalten das normale History-Verhalten des Geräts bei.

## Warum ChannelTerm?

Ein herkömmliches serielles Terminal gibt dem Menschen die physische Verbindung:

```text
Human ---- terminal ---- Serial device
```

Ein serielles Werkzeug nur für KI gibt der KI die Verbindung, lässt den Menschen aber außerhalb des laufenden Terminals:

```text
AI ---- MCP / serial ---- Serial device

Human         (not attached)
```

ChannelTerm hält eine physische Verbindung in einer gemeinsamen Session:

```text
                         +---- Human terminal
                         |
Serial device ---- Session
                         |
                         +---- AI / MCP client
```

So bleiben KI-Aktionen beim Hardware-Debugging sichtbar, und der Mensch bleibt beteiligt. Gemeinsame Nutzung bedeutet mehr, als denselben Port erneut zu öffnen: Jeder Client liest die aufbewahrte rohe Ausgabe über einen eigenen Cursor, und alle Schreibvorgänge laufen durch dieselbe Session.

## KI, Dateien und lokale Steuerung

Dieselbe gemeinsame serielle Session kann auch reguläre Dateien und Verzeichnisse übertragen, ohne KI-Client oder separat installierten Übertragungsagenten auf dem Board. Drücken Sie in einem aktiven gemeinsamen `attach` zuerst `Ctrl+]` und dann `f`, um das geführte Sende-/Empfangsmenü zu öffnen. Verzeichnisse verwenden Standard-tar sowie bestätigte, gut abbrechbare Datenblöcke. Dieselben Vorgänge sind als Befehle verfügbar:

```bash
channelterm file send firmware.bin --session SER-1
channelterm file receive /tmp/log.txt ./log.txt --session SER-1
channelterm file send ./build/release /tmp/release --session SER-1
channelterm file receive /var/log/myapp ./myapp --session SER-1
```

Ohne entferntes Ziel speichert der Befehl unter `/tmp/cterm/mcp-files/`; der interaktive Kurzbefehl verwendet `/tmp/cterm/user-files/`. ChannelTerm erstellt fehlende Zielverzeichnisse und verwendet sie bei späteren Übertragungen erneut. Die Linux-Shell benötigt die nativen Werkzeuge `mkdir`, `stty`, `dd`, `wc`, `sha256sum` und `sleep`, für Verzeichnisse zusätzlich `tar`. ChannelTerm überträgt begrenzte Datenblöcke, meldet den Fortschritt und prüft Datei-Bytes sowie tar-Streams von Verzeichnissen durchgängig per SHA-256. Verzeichnisübertragungen verwenden temporäre Archive auf dem Ziel; ein Abbruch stoppt nach dem aktiven Block, ohne den Rest des Verzeichnisses zu übertragen. Eine temporäre Dateiübertragungs-Lease blockiert andere ChannelTerm-Schreiber bis zum Ende des Befehls. Während dieser Lease öffnet `Ctrl+C` in jeder mitgelieferten Terminal-Anbindung eine lokale Abbruchbestätigung mit der Vorauswahl Nein. Ansonsten behält es seine normale Bedeutung für das entfernte Terminal. Voraussetzungen und Grenzen beschreibt der [Dateiübertragungsablauf](docs/getting-started/file-transfer.md).

Damit eine KI dieselbe Session nutzt, konfigurieren Sie ihren MCP-Client für denselben Streamable-HTTP-Endpunkt:

```text
http://127.0.0.1:37099/mcp
```

Die KI kann mit MCP-Session-Werkzeugen `SER-1` auflisten und lesen, mit `terminal_exec` einen Befehl in einer inaktiven Bash ausführen oder mit `terminal_write` ausdrücklich rohe Terminal-Bytes senden. Siehe [MCP-Ablauf](docs/getting-started/mcp-server.md) und [Werkzeugreferenz](docs/reference/mcp-tools.md). `channelterm init --mcp` installiert die Client-Konfiguration; `channelterm init --mcp-show` gibt sie aus. Beide fragen nach HTTP oder stdio und verwenden standardmäßig HTTP. Die ausgegebene HTTP-Konfiguration enthält die lokale Bearer-Zugangsinformation und darf weder geteilt noch committet werden.

`Ctrl+]` aktiviert den lokalen Escape-Modus und zeigt die verfügbaren Befehle lokal an. `Ctrl+] f` startet den geführten Datei-/Verzeichnisversand oder -empfang in der aktuellen Verbindung. Die Standardziele sind `/tmp/cterm/user-files/<basename>` und `./<basename>`; mit `_1`, `_2` werden Namen ohne Überschreiben gewählt. `Ctrl+] t` ist standardmäßig aus und setzt lokale `[HH:MM:SS]`-Zeitstempel nur vor zuverlässig erkannte Shell-Prompts. Gemeinsame Session- oder MCP-Ausgaben bleiben unverändert.

Der HTTP-Host benötigt ein Bearer-Token und lauscht standardmäßig nur auf der Loopback-Adresse. Die
integrierten CLI-Clients lesen das lokale Benutzertoken automatisch. Der Host kann auch
[in einem vertrauenswürdigen LAN lauschen](docs/getting-started/mcp-server.md#listen-on-a-lan).
Entfernte Clients müssen seine erreichbare Adresse verwenden und dasselbe Token im
`Authorization`-Header senden. Stellen Sie den Endpunkt nicht ohne TLS und zusätzliche
Netzwerkzugriffskontrollen in einem nicht vertrauenswürdigen Netzwerk bereit.

## Möglichkeiten

- Eine vom Host gehaltene serielle Verbindung, gemeinsam genutzt von menschlichen CLI- und MCP-Clients.
- Unabhängige Ausgabe- und Aktivitätscursor, damit ein Leser nicht die Daten eines anderen verbraucht.
- Sichtbare Agent-Schreibvorgänge in verbundenen Terminals ohne Änderung der rohen Session-Ausgabe.
- Direkte menschliche Eingabe einschließlich eines entfernten `Ctrl+C`, während die KI verbunden bleibt.
- Trennen eines Clients, ohne die von anderen Clients genutzte Session zu schließen.
- Serielle Geräteerkennung mit nativen Zielnamen unter Windows, Linux und macOS, TOML-Profilen und privaten Direktverbindungen, wenn keine Freigabe gewünscht ist.
- MCP über stdio oder Streamable HTTP; der gemeinsame CLI-Ablauf verwendet den HTTP Session Host.
- CLI-Versand und -Empfang von Dateien und Verzeichnissen in begrenzten Datenblöcken über eine vorhandene serielle Session, mit SHA-256-Prüfung und sicherem Entpacken in einem Zwischenverzeichnis.

## Beispielabläufe

### Embedded-Linux-Debugging

Verbinden Sie sich einmal mit dem Board. Die KI liest das aufbewahrte Bootprotokoll und führt eine gezielte Diagnose aus, während der Entwickler dieselbe Geräteausgabe beobachtet und in der laufenden Shell reagiert.

### Die KI bedient, der Mensch beobachtet

An einem inaktiven Bash-Prompt sendet die KI mit `terminal_exec` einen nicht interaktiven Befehl. Die verbundene CLI zeigt ihn einmal als `AI`-Aktivitätsblock und danach die tatsächlich vom Gerät zurückgegebenen Bytes. Befehl und interne Initialisierung bleiben aus der Bash-History heraus.

### Der Mensch greift ein

Der Mensch kann in die gemeinsame Session tippen oder `Ctrl+C` an das entfernte Terminal senden, danach weiter beobachten oder sich mit `Ctrl+] q` trennen. Eine kurze `terminal_exec`-Lease sperrt vorübergehend menschliche Eingaben. Normale rohe Schreibvorgänge werden auf Byte-Ebene serialisiert, jedoch ohne Abstimmung des Shell-Zustands; Clients müssen daher weiterhin widersprüchliche rohe Befehlsfolgen vermeiden.

## Funktionsweise

```text
Human CLI ---------+
                   |
                   v
            Session Host / Manager
                   |
AI via MCP --------+--> Session --> Serial Transport <--> Device
                        |    |
                        |    `-- serialized writes
                        `------- retained raw output
```

Der Session Host besitzt den physischen Port und verwaltet die Session-Lebensdauer. Ein Serial Transport öffnet einen protokollneutralen Channel. Die Session besitzt dessen einzigen Leser, die begrenzte Aufbewahrung von Ausgabe und Aktivität im Speicher sowie die Schreibserialisierung. CLI- und MCP-Adapter nutzen dasselbe Core-Verhalten. Darstellung wie Hervorhebung und der `AI`-Aktivitätsblock bleibt außerhalb der gespeicherten Terminaldaten.

Gemeinsame Session-Referenzen wie `SER-1` gelten nur innerhalb des besitzenden Host-Prozesses. Das Stoppen dieses Hosts schließt seine Sessions; das Trennen eines einzelnen Clients nicht.

## Installation

Laden Sie unter [Releases](https://github.com/akira-init1/ChannelTerm/releases) das Archiv für Ihr
Betriebssystem und Ihre Architektur herunter und prüfen Sie es anhand der `SHA256SUMS` derselben
Version. Entpacken Sie es und führen Sie im entpackten Verzeichnis `.\channelterm.exe install`
unter Windows oder `./channelterm install` unter Linux/macOS aus. Fertige Binärdateien benötigen kein Go.

Zum Bauen aus dem Quellcode ist Go 1.26 oder neuer erforderlich. Verwenden Sie eine aktuell
unterstützte Patch-Version und erstellen Sie das Programm im Stammverzeichnis des Repositorys:

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
- [SSH-Shell verwenden](docs/getting-started/ssh-terminal.md)
- [Eine Session teilen](docs/getting-started/shared-session.md)
- [Dateiübertragungsablauf](docs/getting-started/file-transfer.md)
- [MCP-Server](docs/getting-started/mcp-server.md)
- [CLI-Referenz](docs/reference/cli.md)
- [MCP-Werkzeugreferenz](docs/reference/mcp-tools.md)
- [Kennungen und Referenzen](docs/reference/identifiers.md)
- [Architekturübersicht](docs/architecture/overview.md)

Für genaue Befehlsoptionen und Standardwerte ist die `--help`-Ausgabe des laufenden Programms maßgeblich.

## Ausblick

Serielle Verbindungen und SSH-PTY-Shells sind implementierte Transports; dedizierte SFTP- und ältere SCP-Dateiclients stehen ebenfalls bereit. Beliebiges SSH exec, Telnet und stärkere Koordination mehrerer Schreiber bleiben zukünftige Richtungen, keine implementierten Funktionen oder Release-Zusagen.

ChannelTerm implementiert derzeit weder JTAG/OpenOCD noch virtuelle serielle Geräte, eine integrierte KI oder Multi-Agent-Orchestrierung.

## Lizenz

ChannelTerm steht unter der [Apache License 2.0](LICENSE).
