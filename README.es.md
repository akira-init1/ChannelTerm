[English](README.md) | [Deutsch](README.de.md) | Español | [Français](README.fr.md) | [日本語](README.ja.md) | [한국어](README.ko.md) | [Русский](README.ru.md) | [简体中文](README.zh-CN.md) | [繁體中文](README.zh-TW.md)

# ChannelTerm

> Este documento es el punto de entrada en español. El [README en inglés](README.md), el código
> fuente actual y `channelterm --help` son la referencia para el comportamiento, las opciones y la
> compatibilidad.

ChannelTerm permite que las personas y la IA compartan la misma Session de terminal de hardware. La
IA no sustituye tu terminal: puede leer y operar el dispositivo mientras tú sigues conectado, ves
sus acciones, escribes directamente e intervienes cuando sea necesario.

```text
                 Dispositivo serie físico
                           |
                    Serial Transport
                     (una conexión)
                           |
                           v
               ChannelTerm Session SER-1
                  /                 \
                 /                   \
          Terminal humano         Cliente de IA / MCP
       channelterm attach       herramientas de lectura/escritura
```

ChannelTerm implementa actualmente comunicación serie en Windows, Linux y macOS. SSH y Telnet son
posibles direcciones futuras, no funciones actuales. ChannelTerm tampoco incorpora una IA; expone
Sessions de terminal reales a clientes de IA externos mediante MCP.

## Inicio rápido: un comando, un terminal

Enumera los destinos serie nativos sin requerir que ya exista un servidor MCP en ejecución:

```bash
channelterm list --kind device --transport serial --no-mcp
```

Omite este paso si ya conoces el destino nativo, por ejemplo `COM50`.

Si un cliente de IA debe unirse a la Session, configura una vez un cliente compatible antes de
conectarte:

```bash
channelterm init --mcp
```

Elige HTTP o pulsa Enter para aceptar HTTP como opción predeterminada. La configuración incluye la
credencial Bearer local. Recarga o reinicia el cliente de IA si no detecta el cambio inmediatamente.

Después ejecuta únicamente el comando correspondiente a tu plataforma:

```bash
# Windows
channelterm attach COM8 --baud 115200 --label board

# Linux
channelterm attach /dev/ttyUSB0 --baud 115200 --label board

# macOS
channelterm attach /dev/cu.usbserial-110 --baud 115200 --label board
```

Ese único comando `attach` abre el dispositivo serie y conecta el terminal actual. Si no hay un Host
compatible en el endpoint predeterminado, también inicia automáticamente un Host de Session MCP HTTP
local y temporal. Una salida típica es:

```text
Shared Session created: SER-1 (0123456789abcdef0123456789abcdef)
[ChannelTerm] Temporary Session Host started; it and all shared Sessions stop when this attachment exits. Run 'channelterm mcp --transport http' separately for a persistent Host.
```

Esta ruta rápida no requiere otro terminal para el servidor MCP. Mientras el attachment permanezca
abierto, un cliente MCP HTTP configurado puede usar `http://127.0.0.1:37099/mcp` y operar la misma
Session `SER-1`. El Host temporal y todas sus Sessions se detienen cuando finaliza el attachment que
lo creó. Usa un Host persistente solo cuando las Sessions deban sobrevivir a ese terminal.

`--label board` es un nombre visual opcional, no un identificador. Los demás clientes deben usar
`SER-1` o el ID opaco completo de la Session.

### Flujos habituales

| Objetivo | Comando |
| --- | --- |
| Abrir una Session serie compartida e iniciar automáticamente un Host temporal | `channelterm attach COM8 --baud 115200` |
| Configurar un cliente de IA compatible para el Host HTTP compartido | `channelterm init --mcp` |
| Mantener el Host y las Sessions activos de forma independiente | `channelterm mcp --transport http` |
| Enumerar o unirse a una Session existente | `channelterm list --kind session` y después `channelterm attach SER-1` |
| Abrir una conexión serie privada sin compartir por MCP | `channelterm attach COM8 --private --baud 115200` |
| Observar eventos estructurados de la Session | `channelterm events SER-1` |
| Abrir el menú guiado de transferencia en el attachment compartido actual | Pulsa `Ctrl+]` y después `f` |
| Enviar o recibir mediante una Session compartida | `channelterm file send firmware.bin --session SER-1` o `channelterm file receive /tmp/log.txt ./log.txt --session SER-1` |

Un Host persistente es un proceso separado de larga duración. Después de mostrar
`MCP Streamable HTTP listening on http://127.0.0.1:37099/mcp`, otros shells y los clientes de IA
configurados pueden crear Sessions o unirse a ellas. Es normal que no haya más salida tras el mensaje
de disponibilidad. Pulsa `Ctrl+C` para detener el Host y todas sus Sessions.

## Teclas interactivas

```text
Ctrl+C      Enviar 0x03 al terminal remoto
Ctrl+] q    Salir de este CLI; también se detiene cualquier Host temporal que le pertenezca
Ctrl+] ?    Mostrar la ayuda local de secuencias de escape
Ctrl+] ]    Enviar un byte Ctrl+] literal al remoto
Ctrl+] t    Activar o desactivar las marcas de tiempo locales del prompt
Ctrl+] f    Abrir el menú local para enviar/recibir archivos
Ctrl+] Esc  Cancelar el modo de escape local
```

## Conectar una IA a la misma Session

Ejecuta:

```bash
channelterm init --mcp
```

Al elegir HTTP, los clientes compatibles de Codex, Claude Code, OpenCode o Zoo Code utilizan el
endpoint compartido local `http://127.0.0.1:37099/mcp`. La IA puede enumerar, leer y operar `SER-1`.
En un prompt de Bash que se sabe inactivo, usa preferentemente `terminal_exec`; utiliza
`terminal_write` para teclas sin procesar y programas interactivos.

El Host HTTP exige un token Bearer y solo escucha en loopback de forma predeterminada. Los clientes
CLI integrados leen automáticamente el token del usuario local. También puede
[escuchar en una LAN de confianza](docs/getting-started/mcp-server.md#listen-on-a-lan); los clientes
remotos deben usar la dirección alcanzable del Host y enviar el mismo token mediante el encabezado
`Authorization`. No expongas el endpoint a una red no fiable sin TLS y controles de acceso de red
adicionales.

## Transferencia de archivos

Una Session compartida puede enviar o recibir archivos y directorios. En un `attach` compartido
activo, pulsa `Ctrl+]` y después `f` para abrir el menú guiado de envío/recepción. Las mismas
operaciones también están disponibles como comandos:

```bash
channelterm file send firmware.bin --session SER-1
channelterm file receive /tmp/log.txt ./log.txt --session SER-1
channelterm file send ./build/release /tmp/release --session SER-1
```

Si se omite el destino remoto al enviar, el archivo o directorio se guarda de forma predeterminada
en `/tmp/cterm/mcp-files/`. El atajo interactivo usa `/tmp/cterm/user-files/`. ChannelTerm crea
los directorios necesarios y elige sufijos como `_1` y `_2` para no sobrescribir contenido existente.

El destino debe ser un shell Linux con los comandos estándar necesarios. Consulta el
[flujo de transferencia de archivos](docs/getting-started/file-transfer.md) para conocer los
requisitos, la protección contra sobrescritura, la cancelación y la verificación SHA-256.

## Instalación y compilación

ChannelTerm requiere Go 1.25 o posterior. Usa una versión con parches que siga siendo compatible y
compila desde la raíz del repositorio:

```bash
git clone https://github.com/akira-init1/ChannelTerm.git
cd ChannelTerm
go build ./cmd/channelterm
./channelterm install
```

`install` copia el binario en ejecución a la ubicación estándar de programas del usuario actual,
crea los comandos `channelterm` y `cterm`, añade su directorio al PATH del usuario solo cuando es
necesario e inicializa una configuración mínima si no existe. Abre un terminal nuevo si cambia el
PATH. `channelterm uninstall` elimina los comandos y el cambio de PATH propiedad del instalador,
pero conserva los datos del usuario. `channelterm uninstall --purge` exige una confirmación explícita
antes de eliminar la configuración, el estado de dispositivos y la credencial HTTP local.

Ubicaciones predeterminadas; el instalador también muestra las rutas efectivas después de ejecutarse:

| Plataforma | Directorio de comandos | Registro de instalación | Configuración predeterminada |
| --- | --- | --- | --- |
| Windows | `%LOCALAPPDATA%\Programs\ChannelTerm\bin` | `%LOCALAPPDATA%\ChannelTerm\install.json` | `%APPDATA%\channelterm\config.toml` |
| Linux | `~/.local/bin` | `$XDG_STATE_HOME/channelterm/install.json` o `~/.local/state/channelterm/install.json` | `$XDG_CONFIG_HOME/channelterm/config.toml` o `~/.config/channelterm/config.toml` |
| macOS | `~/.local/bin` | `~/Library/Application Support/channelterm/install.json` | `~/Library/Application Support/channelterm/config.toml` |

Los destinos de escritorio compatibles son Windows, Linux y macOS en amd64/arm64. Consulta
[Compilar e instalar desde el código fuente](docs/getting-started/build.md), el contrato completo de
[`install` / `uninstall`](docs/reference/cli.md#install-and-uninstall), las
[ubicaciones de configuración](docs/reference/configuration.md) y
[Compilación y pruebas](docs/development/building-and-testing.md).

## Configuración

ChannelTerm almacena sus archivos locales en el directorio de configuración del usuario de cada
plataforma:

| Plataforma | Directorio predeterminado |
| --- | --- |
| Windows | `%AppData%\channelterm\` |
| Linux | `$XDG_CONFIG_HOME/channelterm/` o `~/.config/channelterm/` si no está definido |
| macOS | `~/Library/Application Support/channelterm/` |

`config.toml` contiene los perfiles serie y la política de conexión administrados por el usuario.
`state.json` es el estado de identidad de dispositivos administrado por ChannelTerm.
`http-auth-token` es el secreto usado para autenticar clientes MCP HTTP; no lo compartas ni lo
incluyas en commits.

```powershell
# Guardar y reutilizar un perfil serie.
channelterm serial --port COM8 --baud 115200 --save board
channelterm serial --profile board

# Usar otro config.toml para esta operación.
channelterm serial --config ./channelterm.toml --profile board
```

Las opciones habituales son `--profile`, `--save`, `--config`, `--port` y `--baud`; el Host MCP
también acepta `--connection-policy ask|auto|deny`. `--config` solo cambia el `config.toml`
seleccionado, no los archivos predeterminados `state.json` ni `http-auth-token`. Consulta
[Perfiles serie](docs/getting-started/serial-profiles.md) y la
[referencia de configuración](docs/reference/configuration.md).

## Documentación

- [Índice de documentación](docs/README.md)
- [Uso del terminal serie](docs/getting-started/serial-terminal.md)
- [Compartir una Session](docs/getting-started/shared-session.md)
- [Transferir archivos](docs/getting-started/file-transfer.md)
- [Servidor MCP](docs/getting-started/mcp-server.md)
- [Referencia de CLI](docs/reference/cli.md)
- [Referencia de herramientas MCP](docs/reference/mcp-tools.md)
- [Identificadores y referencias](docs/reference/identifiers.md)

## Licencia

ChannelTerm se distribuye bajo la [licencia Apache 2.0](LICENSE).
