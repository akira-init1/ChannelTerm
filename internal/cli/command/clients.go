package command

import (
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"

	"github.com/akira-init1/ChannelTerm/internal/core/config"
)

// runClients manages local Host registrations. Filesystem access to the Host's
// configuration is the authority; these commands do not expose a remote admin API.
func runClients(args []string, output io.Writer) error {
	if len(args) == 0 || args[0] == "--help" || args[0] == "-h" {
		fmt.Fprintln(output, "Usage: channelterm clients <add|list|grant|revoke|remove> [options]")
		fmt.Fprintln(output, "  add --name NAME --kind user|agent --credential-file NEW_FILE")
		fmt.Fprintln(output, "  list                  List public client registrations and grants as JSON")
		fmt.Fprintln(output, "  New agents are read-only. Granting writes does not grant Host management; revocation does not stop accepted work.")
		fmt.Fprintln(output, "  grant CLIENT_ID       Allow agent device operations across this Host")
		fmt.Fprintln(output, "  revoke CLIENT_ID      Reject new agent device operations; keep reads")
		fmt.Fprintln(output, "  remove CLIENT_ID      Reject this credential on subsequent HTTP requests")
		fmt.Fprintln(output, "Run on the Host account. Credentials are scoped to its shared token. Never share credential files.")
		return nil
	}
	flags := flag.NewFlagSet("clients "+args[0], flag.ContinueOnError)
	flags.SetOutput(output)
	var name, kind, destination string
	if args[0] == "add" {
		flags.StringVar(&name, "name", "", "public display name")
		flags.StringVar(&kind, "kind", "agent", "registered source: user or agent")
		flags.StringVar(&destination, "credential-file", "", "new private file receiving the credential (never stdout)")
	} else if args[0] != "list" && args[0] != "remove" && args[0] != "grant" && args[0] != "revoke" {
		return errors.New("expected clients add, list, grant, revoke, or remove")
	}
	if err := flags.Parse(args[1:]); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return nil
		}
		return err
	}
	needsID := args[0] == "remove" || args[0] == "grant" || args[0] == "revoke"
	if (needsID && flags.NArg() != 1) || (!needsID && flags.NArg() != 0) {
		return errors.New("unexpected client arguments; see clients --help")
	}
	if args[0] == "add" && (name == "" || destination == "") {
		return errors.New("clients add requires --name and --credential-file")
	}
	token, err := loadHTTPAuthToken()
	if err != nil {
		return err
	}
	store, err := config.DefaultClientStore(token)
	if err != nil {
		return err
	}
	switch args[0] {
	case "list":
		clients, err := store.List()
		if err != nil {
			return err
		}
		return json.NewEncoder(output).Encode(clients)
	case "grant", "revoke":
		return store.SetWriteAllowed(flags.Arg(0), args[0] == "grant")
	case "remove":
		return store.Remove(flags.Arg(0))
	default:
		file, err := os.OpenFile(destination, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
		if err != nil {
			return fmt.Errorf("create new credential file: %w", err)
		}
		identity, credential, err := store.Register(name, kind)
		if err == nil {
			_, err = io.WriteString(file, credential+"\n")
		}
		if err == nil {
			err = file.Sync()
		}
		err = errors.Join(err, file.Close())
		if err != nil {
			if identity.ID != "" {
				err = errors.Join(err, store.Remove(identity.ID))
			}
			return errors.Join(err, os.Remove(destination))
		}
		return json.NewEncoder(output).Encode(identity)
	}
}
