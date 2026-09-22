[English](README.md) | [Deutsch](README.de.md) | [Español](README.es.md) | [Français](README.fr.md) | [日本語](README.ja.md) | [한국어](README.ko.md) | Русский | [简体中文](README.zh-CN.md) | [繁體中文](README.zh-TW.md)

# ChannelTerm

> Это русскоязычная вводная страница. Источниками истины для поведения, параметров и совместимости
> являются английский [README](README.md), текущий исходный код и `channelterm --help`.

ChannelTerm позволяет человеку и ИИ совместно использовать одну аппаратную терминальную Session.
ИИ не заменяет ваш терминал: он может читать данные и управлять устройством, пока вы остаётесь
подключены, видите его действия, вводите команды вручную и при необходимости вмешиваетесь.

```text
                    Физическое последовательное устройство
                                      |
                               Serial Transport
                                (одно соединение)
                                      |
                                      v
                          ChannelTerm Session SER-1
                             /                 \
                            /                   \
                  Терминал человека         Клиент ИИ / MCP
                 channelterm attach       инструменты чтения/записи
```

Сейчас ChannelTerm поддерживает последовательное соединение в Windows, Linux и macOS. SSH и Telnet —
возможные будущие направления, а не реализованные функции. В ChannelTerm нет встроенного ИИ; через
MCP он предоставляет внешним ИИ-клиентам реальные терминальные Sessions.

## Быстрый старт: одна команда, один терминал

Выведите список нативных последовательных устройств без запущенного MCP-сервера:

```bash
channelterm list --kind device --transport serial --no-mcp
```

Если нативная цель, например `COM50`, уже известна, этот шаг можно пропустить.

Если к Session должен подключаться ИИ-клиент, один раз настройте поддерживаемый клиент перед
подключением:

```bash
channelterm init --mcp
```

Выберите HTTP или нажмите Enter, чтобы использовать HTTP по умолчанию. Конфигурация содержит
локальные учётные данные Bearer. Перезагрузите или перезапустите ИИ-клиент, если он не увидел
изменения сразу.

Затем выполните только команду для своей платформы:

```bash
# Windows
channelterm attach COM8 --baud 115200 --label board

# Linux
channelterm attach /dev/ttyUSB0 --baud 115200 --label board

# macOS
channelterm attach /dev/cu.usbserial-110 --baud 115200 --label board
```

Одна команда `attach` открывает последовательное устройство и подключает текущий терминал. Если на
стандартном endpoint ещё нет совместимого Host, команда автоматически запускает временный локальный
HTTP MCP Session Host. Типичный вывод при запуске:

```text
Shared Session created: SER-1 (0123456789abcdef0123456789abcdef)
[ChannelTerm] Temporary Session Host started; it and all shared Sessions stop when this attachment exits. Run 'channelterm mcp --transport http' separately for a persistent Host.
```

Для этого быстрого сценария не нужен отдельный терминал с MCP-сервером. Пока attachment открыт,
настроенный HTTP MCP-клиент может использовать `http://127.0.0.1:37099/mcp` и работать с той же
Session `SER-1`. Временный Host и все принадлежащие ему Sessions завершаются вместе с attachment,
который его создал. Используйте постоянный Host только тогда, когда Sessions должны продолжать
работать независимо от этого терминала.

`--label board` — необязательное отображаемое имя, а не идентификатор. Другим клиентам следует
использовать `SER-1` или полный непрозрачный ID Session.

### Частые сценарии

| Задача | Команда |
| --- | --- |
| Открыть общую последовательную Session и автоматически запустить временный Host | `channelterm attach COM8 --baud 115200` |
| Настроить поддерживаемый ИИ-клиент для общего HTTP Host | `channelterm init --mcp` |
| Оставить Host и Sessions работающими независимо от терминала | `channelterm mcp --transport http` |
| Вывести список или подключиться к существующей Session | `channelterm list --kind session`, затем `channelterm attach SER-1` |
| Открыть приватное последовательное соединение без MCP | `channelterm attach COM8 --private --baud 115200` |
| Наблюдать структурированные события Session | `channelterm events SER-1` |
| Открыть пошаговое меню передачи файлов в текущем общем attachment | Нажмите `Ctrl+]`, затем `f` |
| Отправить или получить данные через общую Session | `channelterm file send firmware.bin --session SER-1` или `channelterm file receive /tmp/log.txt ./log.txt --session SER-1` |

Постоянный Host — отдельный длительно работающий процесс. После сообщения
`MCP Streamable HTTP listening on http://127.0.0.1:37099/mcp` другие оболочки и настроенные
ИИ-клиенты могут создавать Sessions или подключаться к ним. Отсутствие последующего вывода —
нормальное поведение. Нажмите `Ctrl+C`, чтобы остановить Host и все его Sessions.

## Интерактивные клавиши

```text
Ctrl+C      Отправить 0x03 удалённому терминалу
Ctrl+] q    Выйти из этого CLI; принадлежащий ему временный Host также остановится
Ctrl+] ?    Показать локальную справку по escape-командам
Ctrl+] ]    Отправить удалённой стороне буквальный байт Ctrl+]
Ctrl+] t    Включить или выключить локальные метки времени приглашения shell
Ctrl+] f    Открыть локальное меню отправки/получения файлов
Ctrl+] Esc  Отменить локальный escape-режим
```

## Подключение ИИ к той же Session

Выполните:

```bash
channelterm init --mcp
```

После выбора HTTP поддерживаемые клиенты Codex, Claude Code, OpenCode или Zoo Code используют общий
локальный endpoint `http://127.0.0.1:37099/mcp`. ИИ может выводить список, читать и управлять
`SER-1`. В заведомо свободном приглашении Bash предпочтительно использовать `terminal_exec`, а для
необработанных клавиш и интерактивных программ — `terminal_write`.

HTTP Host требует Bearer token и по умолчанию слушает только loopback-адрес. Встроенные CLI-клиенты
автоматически читают token локального пользователя. Host также может
[слушать в доверенной локальной сети](docs/getting-started/mcp-server.md#listen-on-a-lan); удалённые
клиенты должны использовать доступный адрес Host и передавать тот же token в заголовке
`Authorization`. Не публикуйте endpoint в недоверенной сети без TLS и дополнительных средств
контроля сетевого доступа.

## Передача файлов

Общая Session может отправлять и получать файлы и каталоги. В активном общем `attach` нажмите
`Ctrl+]`, затем `f`, чтобы открыть пошаговое меню отправки/получения. Те же операции доступны через
команды:

```bash
channelterm file send firmware.bin --session SER-1
channelterm file receive /tmp/log.txt ./log.txt --session SER-1
channelterm file send ./build/release /tmp/release --session SER-1
```

Если при отправке не указан удалённый путь, файл или каталог по умолчанию сохраняется в
`/tmp/cterm/mcp-files/`. Интерактивное меню использует `/tmp/cterm/user-files/`. ChannelTerm
создаёт отсутствующие каталоги и выбирает суффиксы `_1`, `_2` и далее, чтобы не перезаписывать
существующие данные.

На целевом устройстве должна быть Linux shell с необходимыми стандартными командами. Требования,
защита от перезаписи, отмена и проверка SHA-256 описаны в
[руководстве по передаче файлов](docs/getting-started/file-transfer.md).

## Установка и сборка

Для ChannelTerm требуется Go 1.25 или новее. Используйте поддерживаемый выпуск с актуальными
исправлениями и выполняйте сборку из корня репозитория:

```bash
git clone https://github.com/akira-init1/ChannelTerm.git
cd ChannelTerm
go build ./cmd/channelterm
./channelterm install
```

`install` копирует запущенный бинарный файл в стандартный каталог программ текущего пользователя,
создаёт команды `channelterm` и `cterm`, добавляет каталог в пользовательский PATH только при
необходимости и создаёт отсутствующую минимальную конфигурацию. Если PATH изменился, откройте новый
терминал. `channelterm uninstall` удаляет принадлежащие установщику команды и изменение PATH, но
сохраняет пользовательские данные. `channelterm uninstall --purge` требует явного подтверждения
перед удалением конфигурации, состояния устройств и локальных HTTP-учётных данных.

Стандартные расположения; после запуска установщик также выводит фактические пути:

| Платформа | Каталог команд | Состояние установки | Конфигурация по умолчанию |
| --- | --- | --- | --- |
| Windows | `%LOCALAPPDATA%\Programs\ChannelTerm\bin` | `%LOCALAPPDATA%\ChannelTerm\install.json` | `%APPDATA%\channelterm\config.toml` |
| Linux | `~/.local/bin` | `$XDG_STATE_HOME/channelterm/install.json` или `~/.local/state/channelterm/install.json` | `$XDG_CONFIG_HOME/channelterm/config.toml` или `~/.config/channelterm/config.toml` |
| macOS | `~/.local/bin` | `~/Library/Application Support/channelterm/install.json` | `~/Library/Application Support/channelterm/config.toml` |

Поддерживаемые настольные цели: Windows, Linux и macOS на amd64/arm64. Подробнее:
[сборка и установка из исходного кода](docs/getting-started/build.md), полный контракт
[`install` / `uninstall`](docs/reference/cli.md#install-and-uninstall),
[расположения конфигурации](docs/reference/configuration.md) и
[сборка и тестирование](docs/development/building-and-testing.md).

## Конфигурация

ChannelTerm хранит локальные файлы в пользовательском каталоге конфигурации платформы:

| Платформа | Каталог по умолчанию |
| --- | --- |
| Windows | `%AppData%\channelterm\` |
| Linux | `$XDG_CONFIG_HOME/channelterm/` или `~/.config/channelterm/`, если переменная не задана |
| macOS | `~/Library/Application Support/channelterm/` |

`config.toml` содержит управляемые пользователем последовательные профили и политику подключения.
`state.json` — состояние идентификации устройств, которым управляет ChannelTerm. `http-auth-token` —
секрет для аутентификации HTTP MCP-клиентов; не публикуйте его и не добавляйте в коммиты.

```powershell
# Сохранить и повторно использовать последовательный профиль.
channelterm serial --port COM8 --baud 115200 --save board
channelterm serial --profile board

# Использовать другой config.toml для этой операции.
channelterm serial --config ./channelterm.toml --profile board
```

Часто используемые параметры: `--profile`, `--save`, `--config`, `--port` и `--baud`. MCP Host также
поддерживает `--connection-policy ask|auto|deny`. Параметр `--config` меняет только выбранный
`config.toml`, но не стандартные `state.json` и `http-auth-token`. См. руководство по
[последовательным профилям](docs/getting-started/serial-profiles.md) и
[справочник по конфигурации](docs/reference/configuration.md).

## Документация

- [Индекс документации](docs/README.md)
- [Работа с последовательным терминалом](docs/getting-started/serial-terminal.md)
- [Совместное использование Session](docs/getting-started/shared-session.md)
- [Передача файлов](docs/getting-started/file-transfer.md)
- [MCP-сервер](docs/getting-started/mcp-server.md)
- [Справочник CLI](docs/reference/cli.md)
- [Справочник инструментов MCP](docs/reference/mcp-tools.md)
- [Идентификаторы и ссылки](docs/reference/identifiers.md)

## Лицензия

ChannelTerm распространяется по [лицензии Apache 2.0](LICENSE).
