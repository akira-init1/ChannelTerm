package app

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/akira-init1/ChannelTerm/internal/core/config"
	"github.com/akira-init1/ChannelTerm/internal/core/session"
	"github.com/gofrs/flock"
)

func TestSerialProfilesShareConfigurationWithoutOpeningHardware(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.toml")
	manager := session.NewManager()
	defer manager.Close()
	application, err := New(Dependencies{Manager: manager, Serial: SerialDependencies{ConfigPath: func() (string, error) { return path, nil }}})
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	snapshot, err := application.ReadSerialProfiles(ctx)
	if err != nil || len(snapshot.Profiles) != 0 {
		t.Fatalf("empty read: %+v %v", snapshot, err)
	}
	if _, err := os.Stat(path); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("read created configuration")
	}
	file, err := config.LoadOrCreate(path)
	if err != nil {
		t.Fatal(err)
	}
	profile, err := file.ResolveSerial("")
	if err != nil {
		t.Fatal(err)
	}
	profile.Port = "FAKE-NOT-OPENED"
	profile.Wake = true
	if err := application.SaveSerialProfile(ctx, "first", profile, false); err != nil {
		t.Fatal(err)
	}
	if err := application.SaveSerialProfile(ctx, "first", profile, false); !errors.Is(err, ErrSerialProfileExists) {
		t.Fatalf("overwrite: %v", err)
	}
	var wg sync.WaitGroup
	for _, name := range []string{"second", "third"} {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if err := application.SaveSerialProfile(ctx, name, profile, false); err != nil {
				t.Error(err)
			}
		}()
	}
	wg.Wait()
	if err := application.SetSerialDefault(ctx, "missing"); !errors.Is(err, config.ErrProfileNotFound) {
		t.Fatalf("missing: %v", err)
	}
	if err := application.SetSerialDefault(ctx, "second"); err != nil {
		t.Fatal(err)
	}
	profile.BaudRate = 9600
	if err := application.SaveSerialProfile(ctx, "first", profile, true); err != nil {
		t.Fatal(err)
	}
	snapshot, err = application.ReadSerialProfiles(ctx)
	if err != nil || snapshot.Default != "second" || len(snapshot.Profiles) != 3 || snapshot.Profiles[0].Profile.BaudRate != 9600 {
		t.Fatalf("snapshot: %+v %v", snapshot, err)
	}
	// The existing CLI reader and resolver see the same persisted fields/default.
	cli, err := application.ListSerialProfiles(ctx, path)
	if err != nil || len(cli) != 3 || !cli[0].Profile.Wake {
		t.Fatalf("CLI: %+v %v", cli, err)
	}
	lock := flock.New(path + ".lock")
	if err := lock.Lock(); err != nil {
		t.Fatal(err)
	}
	defer lock.Unlock()
	cancelled, cancel := context.WithTimeout(ctx, 50*time.Millisecond)
	defer cancel()
	if err := application.SetSerialDefault(cancelled, "first"); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("lock cancellation: %v", err)
	}
	snapshot, err = application.ReadSerialProfiles(ctx)
	if err != nil || snapshot.Default != "second" {
		t.Fatalf("cancel changed default: %+v %v", snapshot, err)
	}
	profile.BaudRate = -1
	if err := application.SaveSerialProfile(ctx, "bad", profile, false); err == nil {
		t.Fatal("invalid settings saved")
	}
}
