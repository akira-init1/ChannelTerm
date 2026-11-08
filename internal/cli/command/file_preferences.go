package command

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"

	"github.com/akira-init1/ChannelTerm/internal/core/config"
)

// runFilePreferences is a local, bounded interface used by both the CLI and
// desktop companion. It never connects to a Host or returns other config data.
func runFilePreferences(ctx context.Context, args []string, output io.Writer) error {
	flags := flag.NewFlagSet("file preferences", flag.ContinueOnError)
	flags.SetOutput(output)
	path := flags.String("config", "", "local configuration file (default: platform user config)")
	mode := flags.String("directory-mode", "", "save directory preference: ask, tar or files")
	machine := flags.Bool("json", false, "emit only the directory preference as JSON")
	flags.Usage = func() { fmt.Fprintln(output, "Usage: channelterm file preferences [options]"); flags.PrintDefaults() }
	if err := flags.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return nil
		}
		return err
	}
	if flags.NArg() != 0 {
		return errors.New("file preferences takes no positional arguments")
	}
	explicit := false
	flags.Visit(func(f *flag.Flag) {
		if f.Name == "directory-mode" {
			explicit = true
		}
	})
	if explicit {
		if *mode == "" {
			return errors.New("--directory-mode must be ask, tar or files")
		}
		preference := config.TransferPreferences{DirectoryMode: *mode}
		if _, err := preference.Mode(); err != nil {
			return err
		}
		if *path == "" {
			var err error
			*path, err = config.DefaultPath()
			if err != nil {
				return err
			}
		}
		if err := config.UpdateContext(ctx, *path, func(file *config.File) error { file.Preferences.FileTransfer = preference; return nil }); err != nil {
			return err
		}
	}
	preference, err := config.LoadTransferPreferences(*path)
	if err != nil {
		return err
	}
	resolved, err := preference.Mode()
	if err != nil {
		return err
	}
	if *machine {
		return json.NewEncoder(output).Encode(struct {
			Mode string `json:"directory_mode"`
		}{resolved})
	}
	_, err = fmt.Fprintln(output, "Directory transfer preference:", resolved)
	return err
}
