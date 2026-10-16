[English](README.md) | [Deutsch](README.de.md) | [Español](README.es.md) | [Français](README.fr.md) | [日本語](README.ja.md) | [한국어](README.ko.md) | [Русский](README.ru.md) | [简体中文](README.zh-CN.md) | [繁體中文](README.zh-TW.md)

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

ChannelTerm admite comunicación serie y shells SSH con PTY en Windows, Linux y macOS. No incorpora una IA; ofrece Sessions de terminal reales a personas y clientes de IA externos mediante MCP.

## GDB en espacio de usuario mediante MCP

En Linux nativo, un cliente MCP puede tomar una Session Bash inactiva mediante las herramientas `debug_start` / `debug_command` / `debug_events` / `debug_stop`, establecer puntos de interrupción, ejecutar paso a paso, consultar variables/hilos/registros y desconectarse del proceso. Este primer backend requiere ChannelTerm, GDB, gdbserver y ELF en el mismo entorno Linux. Admite un solo inferior y depuración all-stop; la escritura normal se habilita después de restaurar el terminal y verificar la shell con un marcador único.

```text
debug_start -> break_insert -> continue -> debug_events -> next/step/finish -> debug_stop
```

[Flujo, llamadas MCP, attach seguro y cobertura de pruebas PTY locales](docs/getting-started/gdb-debugging.md).

Para un GDB ejecutado de forma independiente, `debug_bridge_*` expone una Session serie ya preparada mediante un puente TCP autenticado. ChannelTerm no inicia GDB ni gdbserver. GDB utiliza `channelterm rsp-client`; las conexiones autenticadas renuevan sus concesiones automáticamente incluso sin tráfico RSP, con una duración máxima de 24 horas. Al desconectarse, la concesión exclusiva se mantiene hasta verificar la restauración de shell/TTY. El cierre explícito también cierra la Session. El [flujo del puente externo](docs/getting-started/gdb-debugging.md#external-gdb-over-a-prepared-serial-session) explica Windows/WSL2 y la validación separada con PTY y placa de desarrollo.

El puente externo admite cualquier placa de desarrollo con Linux que disponga de un gdbserver compatible a través de la conexión serie preparada y un GDB adecuado para el destino. El puente no restringe el modelo de placa ni la arquitectura de CPU del destino; la depuración bare-metal queda fuera del alcance.

## Archivos mediante SSH

Usa SFTP para transferir y gestionar archivos remotos, o SCP clásico con servidores POSIX compatibles:

```bash
channelterm sftp user@192.168.1.50 ls /tmp
channelterm sftp user@192.168.1.50 put ./firmware.bin /tmp/firmware.bin
channelterm sftp user@192.168.1.50 get /tmp/log.txt ./log.txt
channelterm scp user@192.168.1.50 put ./release /tmp/release --recursive
```

Estos comandos usan la misma autenticación por clave/contraseña y verificación del servidor que SSH,
con una conexión independiente de las Sessions de terminal. Reemplazar archivos requiere
`--overwrite`. Consulta [transferencia de archivos](docs/getting-started/file-transfer.md#sftp-and-scp-over-ssh)
para las reglas de directorios, cancelación y límites.

Las transferencias mediante Sessions de terminal compartidas usan un pequeño protocolo de shell Linux.
Las conexiones SSH dedicadas a archivos usan SFTP o SCP clásico.

## Shell SSH compartida

Conéctate con una clave privada autorizada (las claves cifradas solicitan una frase de contraseña) o introduce tu contraseña cuando se solicite
(ninguna se guarda). Para un servidor desconocido, verifica la huella mostrada y escribe `yes` para
guardar su clave pública en `~/.ssh/known_hosts`; las claves cambiadas se rechazan.
Las solicitudes requieren un terminal interactivo:

```bash
# Terminal 1: abrir la shell y ejecutar uname -a en ella.
channelterm ssh user@192.168.1.50

# Terminal 2: observar y usar la misma shell.
channelterm attach SSH-1
```

Mantén activo el primer comando: al salir se cierran este Host y sus Sessions. El puerto local predeterminado debe estar libre; usa `--listen` y el endpoint de attach mostrado para otro Host. El [flujo SSH](docs/getting-started/ssh-terminal.md) explica las rutas de claves, la verificación del servidor, los puertos y las limitaciones actuales.

## Inicio rápido: un comando, un terminal

Enumera los dispositivos serie detectados, los Profile guardados y, cuando esté disponible el Host
HTTP predeterminado, sus Sessions:

```bash
channelterm list
```

No es necesario que el Host esté en ejecución. Si no está disponible, `list` informa que MCP está
offline y sigue mostrando los dispositivos y Profile locales.

| Opción habitual | Efecto |
| --- | --- |
| `--kind device`, `profile` o `session` | Mostrar solo los tipos seleccionados; se aceptan varios valores separados por comas. |
| `--transport serial` | Mostrar solo resultados serie. |
| `--no-mcp` | Omitir la consulta al Host MCP y mostrar solo fuentes locales. |
| `--long`, `-l` | Incluir los ID opacos completos de las Sessions en la tabla. |
| `--json` | Emitir JSON estructurado para scripts. |
| `--endpoint URL` | Consultar Sessions de un Host HTTP no predeterminado. |

Consulta la [referencia de CLI](docs/reference/cli.md#list) para ver el contrato completo de opciones.

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

Mientras estás conectado:

```text
Ctrl+C      Enviar 0x03 al terminal remoto
Ctrl+] q    Salir de este CLI; también se detiene cualquier Host temporal que le pertenezca
Ctrl+] ?    Mostrar la ayuda local de secuencias de escape
Ctrl+] ]    Enviar un byte Ctrl+] literal al remoto
Ctrl+] t    Activar o desactivar las marcas de tiempo locales del prompt
Ctrl+] f    Abrir el menú local para enviar/recibir archivos
Ctrl+] Esc  Cancelar el modo de escape local
```

Para conectar un cliente de IA compatible al mismo Host, ejecuta `channelterm init --mcp`, elige HTTP y permite que el cliente enumere u opere `SER-1`. Consulta [Servidor MCP](docs/getting-started/mcp-server.md) para la autenticación y el flujo del cliente.

Para obtener una guía de flujo reutilizable, instala el
[Agent Skill `cterm-serial-debug`](docs/getting-started/mcp-server.md#install-the-bundled-agent-skill)
incluido en un cliente compatible con Agent Skills. `channelterm init --mcp` configura clientes
MCP, pero no instala el Skill.

A partir de v0.2.0, las [Releases](https://github.com/akira-init1/ChannelTerm/releases) incluyen
`channelterm_skills_VERSION.zip`. Descomprímelo y copia el directorio completo `cterm-serial-debug`
siguiendo la guía de instalación del Skill enlazada arriba.

La IA también puede crear por sí misma la Session compartida. Ejecuta primero una vez
`channelterm init --mcp` y elige HTTP para configurar el cliente. Después inicia el Host persistente
en primer plano con `channelterm mcp --transport http` y proporciona a la IA el destino serie y los
parámetros exactos. Puede enumerar los puertos, llamar a `terminal_open_serial` e informar de la
referencia `SER-N` resultante para que una persona se una con `channelterm attach SER-N`. Iniciar el
Host por sí solo no abre ningún puerto: una decisión `ask` sigue requiriendo aprobación explícita,
mientras que `auto` devuelve `connect`, pero la IA todavía debe llamar a `terminal_open_serial`; el
Host nunca abre un puerto automáticamente. Detén normalmente el Host con `Ctrl+C`; al hacerlo se
cierran las Sessions que posee.

## Un terminal compartido en la práctica

Una CLI conectada muestra un comando del agente como un bloque local de actividad `AI`. `terminal_exec` evita que el comando entre en el historial de un prompt Bash inactivo. Las líneas siguientes siguen siendo la salida sin procesar del dispositivo, y la persona puede volver a escribir cuando se libera la breve concesión del comando:

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

La salida exacta depende del dispositivo conectado. El bloque `AI` es una presentación local derivada de eventos estructurados de Session; no se inserta en el flujo de bytes del dispositivo ni en la salida de terminal almacenada. Las teclas sin procesar y los programas interactivos siguen usando `terminal_write` y conservan el comportamiento normal del historial del dispositivo.

## ¿Por qué ChannelTerm?

Un terminal serie tradicional deja la conexión física en manos de la persona:

```text
Human ---- terminal ---- Serial device
```

Una herramienta serie exclusiva para IA le da la conexión a la IA, pero deja a la persona fuera del terminal en vivo:

```text
AI ---- MCP / serial ---- Serial device

Human         (not attached)
```

ChannelTerm mantiene una conexión física en una Session compartida:

```text
                         +---- Human terminal
                         |
Serial device ---- Session
                         |
                         +---- AI / MCP client
```

Así, las acciones de la IA siguen siendo observables durante la depuración de hardware y la persona continúa presente. Compartir es más que volver a abrir el mismo puerto: cada cliente lee la salida original retenida mediante un cursor independiente y todas las escrituras pasan por la misma Session.

## IA, archivos y controles locales

La misma Session serie compartida también puede transferir archivos normales y directorios sin un cliente de IA ni un agente de transferencia instalado por separado en la placa. En un `attach` compartido activo, pulsa `Ctrl+]` y luego `f` para abrir el menú guiado de envío/recepción. Los directorios usan tar estándar y bloques con confirmación que facilitan la cancelación. Las mismas operaciones están disponibles como comandos:

```bash
channelterm file send firmware.bin --session SER-1
channelterm file receive /tmp/log.txt ./log.txt --session SER-1
channelterm file send ./build/release /tmp/release --session SER-1
channelterm file receive /var/log/myapp ./myapp --session SER-1
```

Cuando el comando omite el destino remoto, guarda en `/tmp/cterm/mcp-files/`; el atajo interactivo usa `/tmp/cterm/user-files/`. ChannelTerm crea la jerarquía de destino que falte y la reutiliza en transferencias posteriores. La shell Linux utiliza `mkdir`, `stty`, `dd`, `wc`, `sha256sum` y `sleep` nativos, además de `tar` para directorios. ChannelTerm transmite bloques de tamaño limitado, informa del progreso y verifica de extremo a extremo con SHA-256 tanto los bytes de archivos como los flujos tar de directorios. Los directorios usan archivos tar temporales en el destino, por lo que la cancelación se detiene al terminar el bloque activo sin transferir el resto del directorio. Una concesión temporal de transferencia bloquea a los demás escritores de ChannelTerm hasta que termina el comando. Mientras está activa, `Ctrl+C` en cualquiera de las conexiones de terminal incluidas abre una confirmación local de cancelación cuya opción predeterminada es No. Fuera de ese caso conserva su significado normal en el terminal remoto. Consulta el [flujo de transferencia de archivos](docs/getting-started/file-transfer.md) para los requisitos y límites.

Para que una IA participe en la misma Session, configura su cliente MCP para usar el mismo endpoint Streamable HTTP:

```text
http://127.0.0.1:37099/mcp
```

La IA puede usar las herramientas MCP de Session para enumerar y leer `SER-1`, ejecutar un comando en un Bash inactivo con `terminal_exec` o enviar explícitamente bytes de terminal sin procesar con `terminal_write`. Consulta el [flujo MCP](docs/getting-started/mcp-server.md) y la [referencia de herramientas](docs/reference/mcp-tools.md). Ejecuta `channelterm init --mcp` para instalar la configuración del cliente, o `channelterm init --mcp-show` para imprimirla. Ambos permiten elegir HTTP o stdio y usan HTTP por defecto. La configuración HTTP impresa contiene la credencial Bearer local y no debe compartirse ni incorporarse a un commit.

Pulsa `Ctrl+]` para entrar en el modo de escape local; ChannelTerm muestra localmente los comandos disponibles. `Ctrl+] f` inicia el envío o la recepción guiada de archivos/directorios en la conexión actual, con `/tmp/cterm/user-files/<basename>` y `./<basename>` como destinos predeterminados y sufijos `_1`, `_2` para evitar sobrescrituras. `Ctrl+] t` está desactivado por defecto y antepone marcas locales `[HH:MM:SS]` solo a prompts de shell identificados de forma conservadora; no modifica la salida de la Session compartida ni de MCP.

El Host HTTP exige un token Bearer y solo escucha en loopback de forma predeterminada. Los clientes
CLI integrados leen automáticamente el token del usuario local. También puede
[escuchar en una LAN de confianza](docs/getting-started/mcp-server.md#listen-on-a-lan); los clientes
remotos deben usar la dirección alcanzable del Host y enviar el mismo token mediante el encabezado
`Authorization`. No expongas el endpoint a una red no fiable sin TLS y controles de acceso de red
adicionales.

## Qué permite hacer

- Compartir una única conexión serie propiedad del Host entre clientes CLI humanos y clientes MCP.
- Usar cursores independientes de salida y actividad para que un lector no consuma los datos de otro.
- Ver las escrituras del agente en los terminales conectados sin modificar la salida original de Session.
- Introducir datos directamente, incluido un `Ctrl+C` remoto, mientras la IA sigue conectada.
- Desconectar un cliente sin cerrar la Session que utilizan otros clientes.
- Descubrir puertos serie con nombres de destino nativos de Windows, Linux y macOS, usar perfiles TOML y conexiones directas privadas cuando no se desea compartir.
- Usar MCP sobre stdio o Streamable HTTP; el flujo CLI compartido utiliza el HTTP Session Host.
- Enviar y recibir archivos/directorios desde la CLI en bloques limitados sobre una Session serie existente, con verificación SHA-256 y extracción segura de directorios en un área temporal.

## Ejemplos de flujos

### Depuración de Linux embebido

Conéctate a la placa una sola vez. Una IA lee el registro de arranque retenido y ejecuta un diagnóstico específico mientras el ingeniero observa la misma salida del dispositivo y responde en la shell en vivo.

### La IA opera y la persona observa

En un prompt Bash inactivo, la IA envía un comando no interactivo con `terminal_exec`. La CLI conectada lo muestra una vez como bloque de actividad `AI`, seguido de los bytes reales devueltos por el dispositivo. El comando y la preparación interna quedan fuera del historial de Bash.

### La persona interviene

La persona puede escribir en la Session compartida o enviar `Ctrl+C` al terminal remoto, y luego seguir observando o desconectarse con `Ctrl+] q`. Una breve concesión de `terminal_exec` bloquea temporalmente la entrada humana. Las escrituras normales sin procesar se serializan a nivel de bytes, sin arbitraje del estado de la shell; por eso los clientes deben evitar secuencias de comandos sin procesar que entren en conflicto.

## Cómo funciona

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

El Session Host posee el puerto físico y administra la vida de la Session. Un Serial Transport abre un Channel independiente del protocolo. La Session es responsable del único lector del Channel, de la retención limitada en memoria de salida y actividad y de serializar las escrituras. Los adaptadores CLI y MCP usan el mismo comportamiento del Core; la presentación, como el resaltado y el bloque de actividad `AI`, queda fuera de los datos de terminal almacenados.

Las referencias de Session compartida, como `SER-1`, solo son válidas dentro del proceso Host propietario. Detener ese Host cierra sus Sessions; desconectar un cliente no lo hace.

## Instalación

Descarga de [Releases](https://github.com/akira-init1/ChannelTerm/releases) el archivo para tu sistema
operativo y arquitectura y verifícalo con `SHA256SUMS` de la misma versión. Descomprímelo y ejecuta
`.\channelterm.exe install` en Windows o `./channelterm install` en Linux/macOS desde el directorio
extraído. Los binarios precompilados no requieren Go.

Compilar desde el código fuente requiere Go 1.26 o posterior. Usa una versión con parches que siga
siendo compatible y compila desde la raíz del repositorio:

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
- [Flujo de shell SSH](docs/getting-started/ssh-terminal.md)
- [Compartir una Session](docs/getting-started/shared-session.md)
- [Flujo de transferencia de archivos](docs/getting-started/file-transfer.md)
- [Servidor MCP](docs/getting-started/mcp-server.md)
- [Referencia de CLI](docs/reference/cli.md)
- [Referencia de herramientas MCP](docs/reference/mcp-tools.md)
- [Identificadores y referencias](docs/reference/identifiers.md)
- [Descripción de la arquitectura](docs/architecture/overview.md)

La salida `--help` del programa en ejecución es la referencia para los indicadores exactos y sus valores predeterminados.

## Hoja de ruta

Las conexiones serie y las shells SSH PTY son Transports implementados; también están disponibles clientes dedicados de archivos SFTP y SCP tradicional. SSH exec arbitrario, Telnet y una coordinación más fuerte entre múltiples escritores siguen siendo direcciones futuras, no funciones implementadas ni compromisos de lanzamiento.

ChannelTerm no implementa actualmente JTAG/OpenOCD, dispositivos serie virtuales, una IA integrada ni orquestación de múltiples agentes.

## Licencia

ChannelTerm se distribuye bajo la [licencia Apache 2.0](LICENSE).
