# Serial Profiles

ChannelTerm stores named serial profiles in TOML. Unless `--config` or `config_path` selects another file, the file is `channelterm/config.toml` below the directory returned by Go's `os.UserConfigDir`.

## Save a profile

`--save` writes the fully resolved settings before ChannelTerm attempts to open the port:

```powershell
channelterm serial --port COM8 --baud 115200 --save board
```

If this is the first saved profile, it also becomes `[serial].default`. A failed physical open does not roll back the saved profile.

Use the profile later with:

```powershell
channelterm serial --profile board
```

For a target-first shared connection, the target still selects the port while the profile supplies other settings:

```powershell
channelterm attach SER-COM8 --profile board
```

## TOML example

```toml
[serial]
default = "board"

[serial.profiles.board]
port = "COM8"
baud = 115200
data_bits = 8
parity = "none"
stop_bits = "1"
flow_control = "none"
wake = false

[connection]
default_policy = "ask"
```

When a profile omits an optional serial value, it inherits 115200 baud, 8 data bits, `none` parity, `1` stop bit, `none` flow control, and `wake = false`. A usable final profile must have a port.

Only flags that were actually supplied override the selected profile. For example, this keeps every saved setting except baud:

```powershell
channelterm serial --profile board --baud 921600
```

Ordinary opens do not rewrite the file. `--save NAME` creates or replaces only that named profile. See [Configuration](../reference/configuration.md) for precedence, state storage, and validation.
