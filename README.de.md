[English](README.md) | [Deutsch](README.de.md) | [Español](README.es.md) | [Français](README.fr.md) | [日本語](README.ja.md) | [한국어](README.ko.md) | [Русский](README.ru.md) | [简体中文](README.zh-CN.md) | [繁體中文](README.zh-TW.md)

# ChannelTerm

> Dieses Dokument ist der deutschsprachige Einstieg. Für Verhalten, Optionen und Kompatibilität
> sind das englische [README](README.md), der aktuelle Quellcode und `channelterm --help`
> maßgeblich.

Mit ChannelTerm teilen Menschen und KI dieselbe Hardware-Terminal-Session. Die KI ersetzt Ihr
Terminal nicht: Sie kann das Gerät lesen und bedienen, während Sie verbunden bleiben, ihre Aktionen
sehen, selbst Eingaben machen und bei Bedarf eingreifen.

ChannelTerm unterstützt serielle Kommunikation und SSH-Shells mit PTY unter Windows, Linux und macOS. ChannelTerm enthält keine eigene KI; es stellt echte Terminal-Sessions für Menschen und externe KI-Clients über MCP bereit.

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

## Möglichkeiten

- Eine vom Host gehaltene serielle Verbindung, gemeinsam genutzt von menschlichen CLI- und MCP-Clients.
- Unabhängige Ausgabe- und Aktivitätscursor, damit ein Leser nicht die Daten eines anderen verbraucht.
- Sichtbare Agent-Schreibvorgänge in verbundenen Terminals ohne Änderung der rohen Session-Ausgabe.
- Direkte menschliche Eingabe einschließlich eines entfernten `Ctrl+C`, während die KI verbunden bleibt.
- Einen zusätzlich verbundenen Client trennen, ohne die gemeinsame Session zu schließen; beim Beenden des Attachments, das einen temporären Host besitzt, werden dieser Host und seine Sessions geschlossen.
- Serielle Geräteerkennung mit nativen Zielnamen unter Windows, Linux und macOS, TOML-Profilen und privaten Direktverbindungen, wenn keine Freigabe gewünscht ist.
- MCP über stdio oder Streamable HTTP; der gemeinsame CLI-Ablauf verwendet den HTTP Session Host.
- CLI-Versand und -Empfang von Dateien und Verzeichnissen in begrenzten Datenblöcken über eine vorhandene serielle Session, mit SHA-256-Prüfung und sicherem Entpacken in einem Zwischenverzeichnis.

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

Unterstützte Desktop-Ziele sind Windows, Linux und macOS auf amd64/arm64. Weitere Informationen:
[Aus dem Quellcode bauen und installieren](docs/getting-started/build.md), vollständiger Vertrag für
[`install` / `uninstall`](docs/reference/cli.md#install-and-uninstall),
[Konfigurationspfade](docs/reference/configuration.md) sowie
[Build und Tests](docs/development/building-and-testing.md).

## Schnellstart: ein Befehl, ein Terminal

Listen Sie erkannte serielle Geräte, gespeicherte Profile und bei verfügbarem Standard-HTTP-Host
dessen Sessions auf:

```bash
channelterm list
```

Der Host muss nicht laufen. Ist er nicht erreichbar, meldet `list` MCP als offline und zeigt lokale
Geräte und Profile weiterhin an.

Den vollständigen Optionsvertrag finden Sie in der [CLI-Referenz](docs/reference/cli.md#list).

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
```

Alle lokalen Steuerbefehle stehen in der [CLI-Referenz](docs/reference/cli.md#attach); Client-Einrichtung und durch KI erstellte Sessions beschreibt der [MCP-Workflow](docs/getting-started/mcp-server.md).

Installieren Sie für wiederverwendbare Workflow-Anleitungen den mitgelieferten
[`cterm-serial-debug` Agent Skill](docs/getting-started/mcp-server.md#install-the-bundled-agent-skill)
in einem Agent-Skills-kompatiblen Client. `channelterm init --mcp` konfiguriert MCP-Clients,
installiert den Skill jedoch nicht.

Der HTTP-Host benötigt ein Bearer-Token und lauscht standardmäßig nur auf der Loopback-Adresse. Die
integrierten CLI-Clients lesen das lokale Benutzertoken automatisch. Der Host kann auch
[in einem vertrauenswürdigen LAN lauschen](docs/getting-started/mcp-server.md#listen-on-a-lan).
Entfernte Clients müssen seine erreichbare Adresse verwenden und dasselbe Token im
`Authorization`-Header senden. Stellen Sie den Endpunkt nicht ohne TLS und zusätzliche
Netzwerkzugriffskontrollen in einem nicht vertrauenswürdigen Netzwerk bereit.

<a id="beispielabläufe"></a>
<a id="embedded-linux-debugging"></a>
<a id="die-ki-bedient-der-mensch-beobachtet"></a>
<a id="der-mensch-greift-ein"></a>

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

<a id="ki-dateien-und-lokale-steuerung"></a>

## Dateien über eine gemeinsame Session übertragen

Dieselbe gemeinsame serielle Session kann auch reguläre Dateien und Verzeichnisse übertragen, ohne KI-Client oder separat installierten Übertragungsagenten auf dem Board. Drücken Sie in einem aktiven gemeinsamen `attach` zuerst `Ctrl+]` und dann `f`, um das geführte Sende-/Empfangsmenü zu öffnen. Verzeichnisse verwenden Standard-tar sowie bestätigte, gut abbrechbare Datenblöcke. Dieselben Vorgänge sind als Befehle verfügbar:

```bash
channelterm file send firmware.bin --session SER-1
channelterm file receive /tmp/log.txt ./log.txt --session SER-1
channelterm file send ./build/release /tmp/release --session SER-1
channelterm file receive /var/log/myapp ./myapp --session SER-1
```

Übertragungen benötigen eine wartende Linux-Shell und die im [Dateitransfer-Workflow](docs/getting-started/file-transfer.md) genannten Programme. Sie prüfen SHA-256 und wählen einen neuen Namen, statt ein vorhandenes Ziel zu überschreiben. Während einer Übertragung fragt `Ctrl+C` in einem gemeinsamen Attachment nach einer Abbruchbestätigung; andere Schreibzugriffe bleiben bis zur Bereinigung und Lease-Freigabe gesperrt.

## GDB im Benutzerraum über MCP

Unter nativem Linux kann ein MCP-Client mit den `debug_start` / `debug_command` / `debug_events` / `debug_stop`-Tools eine wartende Bash-Session übernehmen, Haltepunkte setzen, schrittweise ausführen, Variablen/Threads/Register prüfen und sich wieder lösen. Dieses erste Backend setzt ChannelTerm, GDB, gdbserver und ELF in derselben Linux-Umgebung voraus. Es unterstützt einen Inferior im All-Stop-Modus; normales Schreiben wird erst nach Wiederherstellung des Terminals und erfolgreicher Shell-Prüfung mit einer eindeutigen Markierung freigegeben.

```text
debug_start -> break_insert -> continue -> debug_events -> next/step/finish -> debug_stop
```

[Ablauf, MCP-Aufrufe, sicheres Attach und lokale PTY-Testabdeckung](docs/getting-started/gdb-debugging.md).

Für ein unabhängig betriebenes GDB stellt `debug_bridge_*` eine vorbereitete serielle Session über eine authentifizierte TLS/TCP-Brücke bereit. Der Betreiber bereitet einen kompatiblen Linux-gdbserver vor und führt GDB unabhängig aus; `channelterm rsp-client` übernimmt die Verbindung. Boardmodell und Ziel-CPU-Architektur sind nicht eingeschränkt. Nach einer Trennung bleibt der exklusive Zugriff während einer begrenzten Wiederherstellungsfrist reserviert. Eine geprüfte Wiederherstellung erhält die Session; explizites Schließen oder Ablauf beendet sie. Der [Workflow für die externe Brücke](docs/getting-started/gdb-debugging.md#external-gdb-over-a-prepared-serial-session) beschreibt Windows/WSL2, Verlängerung und Wiederherstellung. Bare-Metal-Debugging gehört nicht zum unterstützten Umfang.

## Funktionsweise

```text
Human CLI -----+
               +--> Session Host --> Session <--> Channel <--> Serial / SSH endpoint
AI via MCP ----+                         |
                                        +-- retained output, activity, events
                                        `-- serialized writes
```

Der Session Host besitzt die Verbindungen und verwaltet die Session-Lebensdauer. Ein Serial oder SSH Transport stellt einen protokollneutralen Channel her; die Session liest diesen Channel, hält Ausgabe, Aktivität und Ereignisse begrenzt im Speicher und serialisiert Schreibzugriffe. CLI- und MCP-Adapter nutzen dasselbe Core-Verhalten. Darstellung wie Hervorhebung und der `AI`-Aktivitätsblock bleibt außerhalb der gespeicherten Terminaldaten.

Leser haben unabhängige Cursor. Rohe Schreibzugriffe werden serialisiert, koordinieren aber nicht die Bedeutung von Shell-Befehlen. Application-Leases sperren andere Schreiber vorübergehend während Befehlsausführung, Übertragungen und Debugging. Siehe [gemeinsame Sessions](docs/getting-started/shared-session.md) für Besitz und Nebenläufigkeit.

Gemeinsame Referenzen wie `SER-1` gelten nur innerhalb des besitzenden Host-Prozesses. Das Stoppen dieses Hosts schließt seine Sessions. Ein zusätzliches Attachment kann unabhängig beendet werden; beim Beenden des Attachments mit eigenem temporären Host oder des besitzenden SSH-Befehls werden auch deren Sessions geschlossen. Die Schichtgrenzen beschreibt die [Architekturübersicht](docs/architecture/overview.md).

## Konfiguration

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
