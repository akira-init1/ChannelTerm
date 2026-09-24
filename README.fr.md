[English](README.md) | [Deutsch](README.de.md) | [Español](README.es.md) | Français | [日本語](README.ja.md) | [한국어](README.ko.md) | [Русский](README.ru.md) | [简体中文](README.zh-CN.md) | [繁體中文](README.zh-TW.md)

# ChannelTerm

> Ce document est la page d’entrée en français. Le [README anglais](README.md), le code source
> actuel et `channelterm --help` font autorité pour le comportement, les options et la compatibilité.

ChannelTerm permet aux humains et à l’IA de partager la même Session de terminal matériel. L’IA ne
remplace pas votre terminal : elle peut lire et piloter l’appareil pendant que vous restez connecté,
voyez ses actions, saisissez directement des commandes et intervenez si nécessaire.

```text
                 Périphérique série physique
                            |
                     Serial Transport
                      (une connexion)
                            |
                            v
                ChannelTerm Session SER-1
                   /                 \
                  /                   \
         Terminal humain          Client IA / MCP
      channelterm attach         outils de lecture/écriture
```

ChannelTerm implémente actuellement la communication série sous Windows, Linux et macOS. SSH et
Telnet sont des orientations futures, pas des fonctions actuelles. ChannelTerm n’intègre pas d’IA ;
il expose de vraies Sessions de terminal à des clients IA externes via MCP.

## Démarrage rapide : une commande, un terminal

Listez les périphériques série détectés, les Profile enregistrés et, lorsque le Host HTTP par défaut
est disponible, ses Sessions :

```bash
channelterm list
```

Le Host n’a pas besoin d’être actif. S’il est inaccessible, `list` signale MCP comme offline tout en
continuant d’afficher les périphériques et Profile locaux.

| Option courante | Effet |
| --- | --- |
| `--kind device`, `profile` ou `session` | Afficher uniquement les types choisis ; plusieurs valeurs peuvent être séparées par des virgules. |
| `--transport serial` | Afficher uniquement les résultats série. |
| `--no-mcp` | Ignorer la requête au Host MCP et n’afficher que les sources locales. |
| `--long`, `-l` | Ajouter les ID opaques complets des Sessions au tableau. |
| `--json` | Produire du JSON structuré pour les scripts. |
| `--endpoint URL` | Interroger les Sessions d’un Host HTTP non standard. |

Consultez la [référence CLI](docs/reference/cli.md#list) pour le contrat complet des options.

Ignorez cette étape si vous connaissez déjà la cible native, par exemple `COM50`.

Si un client IA doit rejoindre la Session, configurez une fois un client pris en charge avant la
connexion :

```bash
channelterm init --mcp
```

Choisissez HTTP, ou appuyez sur Entrée pour accepter HTTP par défaut. La configuration contient
l’identifiant Bearer local. Rechargez ou redémarrez le client IA s’il ne détecte pas immédiatement
la nouvelle configuration.

Exécutez ensuite uniquement la commande correspondant à votre plateforme :

```bash
# Windows
channelterm attach COM8 --baud 115200 --label board

# Linux
channelterm attach /dev/ttyUSB0 --baud 115200 --label board

# macOS
channelterm attach /dev/cu.usbserial-110 --baud 115200 --label board
```

Cette seule commande `attach` ouvre le périphérique série et connecte le terminal courant. Si aucun
Host compatible n’écoute sur le point de terminaison par défaut, elle démarre automatiquement un
Host de Session MCP HTTP local temporaire. Une sortie typique est :

```text
Shared Session created: SER-1 (0123456789abcdef0123456789abcdef)
[ChannelTerm] Temporary Session Host started; it and all shared Sessions stop when this attachment exits. Run 'channelterm mcp --transport http' separately for a persistent Host.
```

Aucun terminal distinct n’est nécessaire pour le serveur MCP dans ce parcours rapide. Tant que
l’attachement reste ouvert, un client MCP HTTP configuré peut utiliser
`http://127.0.0.1:37099/mcp` et piloter la même Session `SER-1`. Le Host temporaire et toutes ses
Sessions s’arrêtent lorsque l’attachement qui l’a créé se termine. Utilisez un Host persistant
seulement si les Sessions doivent survivre à ce terminal.

`--label board` est un nom d’affichage facultatif, pas un identifiant. Les autres clients doivent
utiliser `SER-1` ou l’ID opaque complet de la Session.

### Utilisations courantes

| Objectif | Commande |
| --- | --- |
| Ouvrir une Session série partagée et démarrer automatiquement un Host temporaire | `channelterm attach COM8 --baud 115200` |
| Configurer un client IA pris en charge pour le Host HTTP partagé | `channelterm init --mcp` |
| Maintenir le Host et les Sessions indépendamment du terminal | `channelterm mcp --transport http` |
| Lister ou rejoindre une Session existante | `channelterm list --kind session`, puis `channelterm attach SER-1` |
| Ouvrir une connexion série privée sans partage MCP | `channelterm attach COM8 --private --baud 115200` |
| Observer les événements structurés d’une Session | `channelterm events SER-1` |
| Ouvrir le menu guidé de transfert dans l’attachement partagé courant | Appuyez sur `Ctrl+]`, puis sur `f` |
| Envoyer ou recevoir via une Session partagée | `channelterm file send firmware.bin --session SER-1` ou `channelterm file receive /tmp/log.txt ./log.txt --session SER-1` |

Un Host persistant est un processus distinct de longue durée. Après l’affichage de
`MCP Streamable HTTP listening on http://127.0.0.1:37099/mcp`, d’autres shells et les clients IA
configurés peuvent créer ou rejoindre des Sessions. L’absence de sortie après ce message est
normale. Appuyez sur `Ctrl+C` pour arrêter le Host et toutes ses Sessions.

## Commandes interactives

```text
Ctrl+C      Envoyer 0x03 au terminal distant
Ctrl+] q    Quitter ce CLI ; un Host temporaire détenu par cet attachement s’arrête aussi
Ctrl+] ?    Afficher l’aide locale des séquences d’échappement
Ctrl+] ]    Envoyer un octet Ctrl+] littéral au distant
Ctrl+] t    Activer ou désactiver l’horodatage local des invites shell
Ctrl+] f    Ouvrir le menu local d’envoi/réception de fichiers
Ctrl+] Esc  Annuler le mode d’échappement local
```

## Connecter une IA à la même Session

Exécutez :

```bash
channelterm init --mcp
```

Après avoir choisi HTTP, les clients Codex, Claude Code, OpenCode ou Zoo Code pris en charge utilisent
le point de terminaison partagé local `http://127.0.0.1:37099/mcp`. L’IA peut lister, lire et piloter
`SER-1`. À une invite Bash dont l’inactivité est connue, préférez `terminal_exec` ; utilisez
`terminal_write` pour les touches brutes et les programmes interactifs.

Pour bénéficier d’un workflow guidé réutilisable, installez l’
[Agent Skill `cterm-debug`](docs/getting-started/mcp-server.md#install-the-bundled-agent-skill)
inclus dans un client compatible avec Agent Skills. `channelterm init --mcp` configure les clients
MCP, mais n’installe pas le Skill.

L’IA peut aussi créer elle-même la Session partagée. Exécutez d’abord une fois
`channelterm init --mcp` et choisissez HTTP pour configurer le client. Démarrez ensuite le Host
persistant au premier plan avec `channelterm mcp --transport http`, puis indiquez à l’IA la cible
série et les paramètres exacts. Elle peut lister les ports, appeler `terminal_open_serial` et
communiquer la référence `SER-N` obtenue afin qu’un humain la rejoigne avec
`channelterm attach SER-N`. Le simple démarrage du Host n’ouvre aucun port : une décision `ask`
exige toujours une approbation explicite. `auto` renvoie `connect`, mais l’IA doit encore appeler
`terminal_open_serial` ; le Host n’ouvre jamais un port automatiquement. Arrêtez normalement le
Host avec `Ctrl+C` ; cela ferme les Sessions qu’il possède.

Le Host HTTP exige un token Bearer et n’écoute par défaut que sur l’interface de bouclage. Les CLI
intégrés lisent automatiquement le token de l’utilisateur local. Le Host peut aussi
[écouter sur un réseau local de confiance](docs/getting-started/mcp-server.md#listen-on-a-lan) ; les
clients distants doivent employer son adresse joignable et envoyer le même token dans l’en-tête
`Authorization`. N’exposez pas ce point de terminaison à un réseau non fiable sans TLS et contrôles
d’accès réseau supplémentaires.

## Transfert de fichiers

Une Session partagée peut envoyer et recevoir des fichiers ou des répertoires. Dans un `attach`
partagé actif, appuyez sur `Ctrl+]`, puis sur `f`, pour ouvrir le menu guidé d’envoi/réception. Les
mêmes opérations sont également disponibles en ligne de commande :

```bash
channelterm file send firmware.bin --session SER-1
channelterm file receive /tmp/log.txt ./log.txt --session SER-1
channelterm file send ./build/release /tmp/release --session SER-1
```

Si la destination distante est omise lors d’un envoi, le fichier ou le répertoire est enregistré
par défaut sous `/tmp/cterm/mcp-files/`. Le raccourci interactif utilise
`/tmp/cterm/user-files/`. ChannelTerm crée les répertoires nécessaires et choisit des suffixes
`_1`, `_2`, etc. pour ne pas écraser un contenu existant.

La cible doit être un shell Linux disposant des commandes standard nécessaires. Consultez le
[guide de transfert](docs/getting-started/file-transfer.md) pour les prérequis, la protection contre
l’écrasement, l’annulation et la vérification SHA-256.

## Installation et compilation

ChannelTerm nécessite Go 1.25 ou une version ultérieure. Utilisez une version corrigée encore prise
en charge, puis compilez depuis la racine du dépôt :

```bash
git clone https://github.com/akira-init1/ChannelTerm.git
cd ChannelTerm
go build ./cmd/channelterm
./channelterm install
```

`install` copie le binaire en cours d’exécution dans l’emplacement standard du programme pour
l’utilisateur courant, crée les commandes `channelterm` et `cterm`, ajoute leur répertoire au PATH
utilisateur seulement si nécessaire et initialise une configuration minimale absente. Ouvrez un
nouveau terminal si le PATH a changé. `channelterm uninstall` retire les commandes et la modification
du PATH appartenant à l’installateur, tout en conservant les données utilisateur.
`channelterm uninstall --purge` exige une confirmation explicite avant de supprimer la configuration,
l’état des périphériques et l’identifiant HTTP local.

Emplacements par défaut ; l’installateur affiche aussi les chemins effectifs après chaque exécution :

| Plateforme | Répertoire des commandes | État d’installation | Configuration par défaut |
| --- | --- | --- | --- |
| Windows | `%LOCALAPPDATA%\Programs\ChannelTerm\bin` | `%LOCALAPPDATA%\ChannelTerm\install.json` | `%APPDATA%\channelterm\config.toml` |
| Linux | `~/.local/bin` | `$XDG_STATE_HOME/channelterm/install.json`, ou `~/.local/state/channelterm/install.json` | `$XDG_CONFIG_HOME/channelterm/config.toml`, ou `~/.config/channelterm/config.toml` |
| macOS | `~/.local/bin` | `~/Library/Application Support/channelterm/install.json` | `~/Library/Application Support/channelterm/config.toml` |

Les cibles de bureau prises en charge sont Windows, Linux et macOS sur amd64/arm64. Consultez
[Compiler et installer depuis les sources](docs/getting-started/build.md), le contrat complet
[`install` / `uninstall`](docs/reference/cli.md#install-and-uninstall), les
[emplacements de configuration](docs/reference/configuration.md) et le guide
[Compilation et tests](docs/development/building-and-testing.md).

## Configuration

ChannelTerm conserve ses fichiers locaux dans le répertoire de configuration utilisateur de la
plateforme :

| Plateforme | Répertoire par défaut |
| --- | --- |
| Windows | `%AppData%\channelterm\` |
| Linux | `$XDG_CONFIG_HOME/channelterm/`, ou `~/.config/channelterm/` si non défini |
| macOS | `~/Library/Application Support/channelterm/` |

`config.toml` contient les profils série et la stratégie de connexion gérés par l’utilisateur.
`state.json` contient l’état d’identité des périphériques géré par ChannelTerm. `http-auth-token`
est le secret d’authentification des clients MCP HTTP ; ne le partagez pas et ne le validez pas dans
le dépôt.

```powershell
# Enregistrer et réutiliser un profil série.
channelterm serial --port COM8 --baud 115200 --save board
channelterm serial --profile board

# Utiliser un autre config.toml pour cette opération.
channelterm serial --config ./channelterm.toml --profile board
```

Les options courantes sont `--profile`, `--save`, `--config`, `--port` et `--baud`. Le Host MCP
accepte aussi `--connection-policy ask|auto|deny`. `--config` ne change que le `config.toml`
sélectionné, pas les fichiers `state.json` ou `http-auth-token` par défaut. Consultez les
[profils série](docs/getting-started/serial-profiles.md) et la
[référence de configuration](docs/reference/configuration.md).

## Documentation

- [Index de la documentation](docs/README.md)
- [Utiliser le terminal série](docs/getting-started/serial-terminal.md)
- [Partager une Session](docs/getting-started/shared-session.md)
- [Transférer des fichiers](docs/getting-started/file-transfer.md)
- [Serveur MCP](docs/getting-started/mcp-server.md)
- [Référence CLI](docs/reference/cli.md)
- [Référence des outils MCP](docs/reference/mcp-tools.md)
- [Identifiants et références](docs/reference/identifiers.md)

## Licence

ChannelTerm est distribué sous [licence Apache 2.0](LICENSE).
