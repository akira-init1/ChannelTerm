[English](README.md) | [Deutsch](README.de.md) | [Español](README.es.md) | [Français](README.fr.md) | [日本語](README.ja.md) | [한국어](README.ko.md) | [Русский](README.ru.md) | [简体中文](README.zh-CN.md) | [繁體中文](README.zh-TW.md)

# ChannelTerm

> Ce document est la page d’entrée en français. Le [README anglais](README.md), le code source
> actuel et `channelterm --help` font autorité pour le comportement, les options et la compatibilité.

ChannelTerm permet aux humains et à l’IA de partager la même Session de terminal matériel. L’IA ne
remplace pas votre terminal : elle peut lire et piloter l’appareil pendant que vous restez connecté,
voyez ses actions, saisissez directement des commandes et intervenez si nécessaire.

ChannelTerm prend en charge la communication série et les shells SSH avec PTY sous Windows, Linux et macOS. Il n’intègre pas d’IA ; il expose de vraies Sessions de terminal aux utilisateurs et aux clients IA externes via MCP.

## Pourquoi ChannelTerm ?

Un terminal série traditionnel donne la connexion physique à l’utilisateur :

```text
Human ---- terminal ---- Serial device
```

Un outil série réservé à l’IA donne la connexion à l’IA, mais laisse l’utilisateur en dehors du terminal actif :

```text
AI ---- MCP / serial ---- Serial device

Human         (not attached)
```

ChannelTerm conserve une connexion physique dans une Session partagée :

```text
                         +---- Human terminal
                         |
Serial device ---- Session
                         |
                         +---- AI / MCP client
```

Les actions de l’IA restent ainsi observables pendant le débogage matériel, et l’utilisateur reste présent. Le partage ne consiste pas simplement à rouvrir le même port : chaque client lit la sortie brute conservée avec un curseur indépendant, et toutes les écritures passent par la même Session.

## Ce que permet ChannelTerm

- Une connexion série détenue par le Host et partagée entre les clients CLI humains et les clients MCP.
- Des curseurs de sortie et d’activité indépendants, afin qu’un lecteur ne consomme pas les données d’un autre.
- Des écritures de l’agent visibles dans les terminaux connectés, sans modification de la sortie brute de Session.
- La saisie humaine directe, y compris un `Ctrl+C` distant, pendant que l’IA reste connectée.
- Déconnecter un client supplémentaire sans fermer la Session partagée ; quitter la connexion CLI propriétaire d’un Host temporaire ferme ce Host et ses Sessions.
- La découverte série avec les noms de cibles natifs de Windows, Linux et macOS, des profils TOML et des connexions directes privées lorsque le partage n’est pas souhaité.
- MCP sur stdio ou Streamable HTTP ; le workflow CLI partagé utilise le HTTP Session Host.
- L’envoi et la réception CLI de fichiers/répertoires par blocs limités sur une Session série existante, avec vérification SHA-256 et extraction sûre des répertoires dans une zone temporaire.

## Installation

Téléchargez depuis [Releases](https://github.com/akira-init1/ChannelTerm/releases) l’archive adaptée
à votre système et à votre architecture, puis vérifiez-la avec le fichier `SHA256SUMS` de la même
version. Décompressez-la et exécutez `.\channelterm.exe install` sous Windows ou
`./channelterm install` sous Linux/macOS depuis le répertoire extrait. Les binaires précompilés
ne nécessitent pas Go.

La compilation depuis les sources nécessite Go 1.26 ou une version ultérieure. Utilisez une version
corrigée encore prise en charge, puis compilez depuis la racine du dépôt :

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

Les cibles de bureau prises en charge sont Windows, Linux et macOS sur amd64/arm64. Consultez
[Compiler et installer depuis les sources](docs/getting-started/build.md), le contrat complet
[`install` / `uninstall`](docs/reference/cli.md#install-and-uninstall), les
[emplacements de configuration](docs/reference/configuration.md) et le guide
[Compilation et tests](docs/development/building-and-testing.md).

## Démarrage rapide : une commande, un terminal

Listez les périphériques série détectés, les Profile enregistrés et, lorsque le Host HTTP par défaut
est disponible, ses Sessions :

```bash
channelterm list
```

Le Host n’a pas besoin d’être actif. S’il est inaccessible, `list` signale MCP comme offline tout en
continuant d’afficher les périphériques et Profile locaux.

Consultez la [référence CLI](docs/reference/cli.md#list) pour le contrat complet des options.

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

Pendant la connexion :

```text
Ctrl+C      Envoyer 0x03 au terminal distant
Ctrl+] q    Quitter ce CLI ; un Host temporaire détenu par cet attachement s’arrête aussi
Ctrl+] ?    Afficher l’aide locale des séquences d’échappement
```

Consultez la [référence CLI](docs/reference/cli.md#attach) pour toutes les commandes locales et le [guide MCP](docs/getting-started/mcp-server.md) pour la configuration des clients et la création de Sessions par l’IA.

Pour bénéficier d’un workflow guidé réutilisable, installez l’
[Agent Skill `cterm-serial-debug`](docs/getting-started/mcp-server.md#install-the-bundled-agent-skill)
inclus dans un client compatible avec Agent Skills. `channelterm init --mcp` configure les clients
MCP, mais n’installe pas le Skill.

Le Host HTTP exige un token Bearer et n’écoute par défaut que sur l’interface de bouclage. Les CLI
intégrés lisent automatiquement le token de l’utilisateur local. Le Host peut aussi
[écouter sur un réseau local de confiance](docs/getting-started/mcp-server.md#listen-on-a-lan) ; les
clients distants doivent employer son adresse joignable et envoyer le même token dans l’en-tête
`Authorization`. N’exposez pas ce point de terminaison à un réseau non fiable sans TLS et contrôles
d’accès réseau supplémentaires.

<a id="exemples-de-workflows"></a>
<a id="débogage-linux-embarqué"></a>
<a id="lia-agit-lutilisateur-observe"></a>
<a id="lutilisateur-intervient"></a>

## Un terminal partagé en pratique

Un CLI connecté affiche une commande de l’agent dans un bloc local d’activité `AI`. `terminal_exec` évite d’inscrire cette commande dans l’historique d’une invite Bash inactive. Les lignes suivantes restent la sortie brute du terminal de l’appareil, et l’utilisateur peut de nouveau saisir du texte une fois le court bail de commande libéré :

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

La sortie exacte dépend de l’appareil connecté. Le bloc `AI` est une présentation locale issue des événements structurés de Session ; il n’est inséré ni dans le flux d’octets de l’appareil ni dans la sortie de terminal stockée. Les touches brutes et les programmes interactifs utilisent toujours `terminal_write` et conservent le comportement habituel de l’historique de l’appareil.

## Shell SSH partagé

Connectez-vous avec une clé privée autorisée (une phrase secrète est demandée pour les clés chiffrées) ou saisissez votre mot de passe à la demande
(aucun des deux n'est enregistré). Pour un serveur inconnu, vérifiez l'empreinte affichée et saisissez `yes`
pour enregistrer sa clé publique dans `~/.ssh/known_hosts` ; les clés modifiées sont refusées.
Ces invites nécessitent un terminal interactif :

```bash
# Terminal 1 : ouvrir le shell, puis y exécuter uname -a.
channelterm ssh user@192.168.1.50

# Terminal 2 : observer et utiliser le même shell.
channelterm attach SSH-1
```

Gardez la première commande ouverte : sa fermeture arrête ce Host et ses Sessions. Le port local par défaut doit être libre ; utilisez `--listen` et le point de connexion attach affiché pour un autre Host. Le [guide SSH](docs/getting-started/ssh-terminal.md) décrit les chemins des clés, la vérification du serveur, les ports et les limites actuelles.

## Fichiers via SSH

Utilisez SFTP pour transférer et gérer les fichiers distants, ou SCP classique avec les serveurs POSIX compatibles :

```bash
channelterm sftp user@192.168.1.50 ls /tmp
channelterm sftp user@192.168.1.50 put ./firmware.bin /tmp/firmware.bin
channelterm sftp user@192.168.1.50 get /tmp/log.txt ./log.txt
channelterm scp user@192.168.1.50 put ./release /tmp/release --recursive
```

Ces commandes réutilisent l'authentification par clé/mot de passe et la vérification des hôtes SSH,
sur une connexion indépendante des Sessions de terminal. Le remplacement de fichiers nécessite
`--overwrite`. Voir [transfert de fichiers](docs/getting-started/file-transfer.md#sftp-and-scp-over-ssh)
pour les règles des répertoires, l'annulation et les limites.

Les transferts via les Sessions de terminal partagées utilisent un petit protocole de shell Linux.
Les connexions SSH dédiées aux fichiers utilisent SFTP ou SCP classique.

<a id="ia-fichiers-et-commandes-locales"></a>

## Fichiers via une Session partagée

La même Session série partagée peut aussi transférer des fichiers ordinaires et des répertoires, sans client IA ni agent de transfert installé séparément sur la carte. Dans un `attach` partagé actif, appuyez sur `Ctrl+]`, puis sur `f`, pour ouvrir le menu guidé d’envoi/réception. Les répertoires utilisent tar standard et des blocs acquittés qui facilitent l’annulation. Les mêmes opérations sont disponibles en ligne de commande :

```bash
channelterm file send firmware.bin --session SER-1
channelterm file receive /tmp/log.txt ./log.txt --session SER-1
channelterm file send ./build/release /tmp/release --session SER-1
channelterm file receive /var/log/myapp ./myapp --session SER-1
```

Les transferts nécessitent un shell Linux inactif et les outils indiqués dans le [guide de transfert](docs/getting-started/file-transfer.md). Ils vérifient SHA-256 et choisissent un nouveau nom au lieu d’écraser une destination existante. Pendant un transfert, `Ctrl+C` dans un attachement partagé demande confirmation de l’annulation ; les autres écritures restent bloquées jusqu’à la fin du nettoyage et à la libération du bail.

## GDB en espace utilisateur via MCP

Sous Linux natif, un client MCP peut prendre le contrôle d’une Session Bash inactive avec les outils `debug_start` / `debug_command` / `debug_events` / `debug_stop`, poser des points d’arrêt, avancer pas à pas, examiner variables/threads/registres et se détacher. Ce premier backend exige ChannelTerm, GDB, gdbserver et l’ELF dans le même environnement Linux. Il prend en charge un seul inferior en mode all-stop ; l’écriture normale reprend uniquement après restauration du terminal et vérification du shell par un marqueur unique.

```text
debug_start -> break_insert -> continue -> debug_events -> next/step/finish -> debug_stop
```

[Procédure, appels MCP, attach sûr et couverture des tests PTY locaux](docs/getting-started/gdb-debugging.md).

Pour un GDB indépendant, `debug_bridge_*` expose une Session série préparée via un pont TLS/TCP authentifié. L’opérateur prépare un gdbserver Linux compatible et exécute GDB indépendamment ; `channelterm rsp-client` assure la connexion. Le modèle de carte et l’architecture CPU cible ne sont pas restreints. La déconnexion conserve la propriété exclusive pendant un délai de récupération limité. Une restauration vérifiée préserve la Session ; la fermeture explicite ou l’expiration la ferme. Consultez le [guide du pont externe](docs/getting-started/gdb-debugging.md#external-gdb-over-a-prepared-serial-session) pour Windows/WSL2, le renouvellement et la récupération. Le débogage bare-metal est hors périmètre.

## Fonctionnement

```text
Human CLI -----+
               +--> Session Host --> Session <--> Channel <--> Serial / SSH endpoint
AI via MCP ----+                         |
                                        +-- retained output, activity, events
                                        `-- serialized writes
```

Le Session Host possède les connexions et gère la durée de vie des Sessions. Un Serial ou SSH Transport établit un Channel indépendant du protocole ; la Session lit ce Channel, conserve en mémoire une quantité limitée de sortie, d’activité et d’événements, et sérialise les écritures. Les adaptateurs CLI et MCP réutilisent le même comportement du Core. La présentation, comme la coloration et le bloc d’activité `AI`, reste en dehors des données de terminal stockées.

Les lecteurs disposent de curseurs indépendants. Les écritures brutes sont sérialisées, sans coordination du sens des commandes shell. Les baux applicatifs bloquent temporairement les autres écritures pendant les commandes, transferts et débogages. Voir les [Sessions partagées](docs/getting-started/shared-session.md) pour la propriété et la concurrence.

Les références partagées comme `SER-1` ne sont valides que dans le processus Host qui les possède. Arrêter ce Host ferme ses Sessions. Une connexion CLI supplémentaire peut se terminer indépendamment, mais quitter la connexion propriétaire d’un Host temporaire ou la commande SSH propriétaire ferme aussi leurs Sessions. Voir la [vue d’ensemble de l’architecture](docs/architecture/overview.md) pour les limites entre couches.

## Configuration

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
- [Utiliser un shell SSH](docs/getting-started/ssh-terminal.md)
- [Partager une Session](docs/getting-started/shared-session.md)
- [Workflow de transfert de fichiers](docs/getting-started/file-transfer.md)
- [Serveur MCP](docs/getting-started/mcp-server.md)
- [Référence CLI](docs/reference/cli.md)
- [Référence des outils MCP](docs/reference/mcp-tools.md)
- [Identifiants et références](docs/reference/identifiers.md)
- [Vue d’ensemble de l’architecture](docs/architecture/overview.md)

Pour les options exactes et leurs valeurs par défaut, la sortie `--help` du programme en cours d’exécution fait autorité.

## Feuille de route

Les connexions série et les shells SSH PTY sont des Transports implémentés ; des clients de fichiers dédiés SFTP et SCP traditionnel sont également disponibles. SSH exec arbitraire, Telnet et une coordination plus forte entre plusieurs clients en écriture restent des orientations futures, et non des fonctionnalités implémentées ou des engagements de livraison.

ChannelTerm n’implémente actuellement ni JTAG/OpenOCD, ni périphériques série virtuels, ni IA intégrée, ni orchestration multi-agents.

## Licence

ChannelTerm est distribué sous [licence Apache 2.0](LICENSE).
