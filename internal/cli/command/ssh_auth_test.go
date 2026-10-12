package command

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	sshtransport "github.com/akira-init1/ChannelTerm/internal/transport/ssh"
)

func TestSSHIdentityDiscoveryAndExplicitSelection(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	dir := filepath.Join(home, ".ssh")
	if err := os.Mkdir(dir, 0700); err != nil {
		t.Fatal(err)
	}
	first, second := filepath.Join(dir, "id_ed25519"), filepath.Join(dir, "id_rsa")
	for _, file := range []string{first, second} {
		if err := os.WriteFile(file, []byte("fixture path discovery only"), 0600); err != nil {
			t.Fatal(err)
		}
	}
	config := sshtransport.Config{}
	if err := configureSSHAuthentication(&config, strings.NewReader(""), os.Stdout); err != nil {
		t.Fatal(err)
	}
	if config.PrivateKeyPath != first || !reflect.DeepEqual(config.PrivateKeyPaths, []string{second}) {
		t.Fatalf("default order=%+v", config)
	}
	var selected identityFlags
	selected.Set("explicit-first")
	selected.Set("explicit-second")
	config = sshtransport.Config{}
	selected.configure(&config)
	if err := configureSSHAuthentication(&config, strings.NewReader(""), os.Stdout); err != nil {
		t.Fatal(err)
	}
	if config.PrivateKeyPath != "explicit-first" || !reflect.DeepEqual(config.PrivateKeyPaths, []string{"explicit-second"}) {
		t.Fatal("defaults mixed with explicit identities")
	}
	if config.KeyPassphrase != nil || config.Password != nil {
		t.Fatal("nonterminal received interactive callbacks")
	}
}
