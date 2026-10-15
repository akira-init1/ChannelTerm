package debugger

import (
	"context"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"
)

func fakeMI(t *testing.T, script string) *Client {
	t.Helper()
	path := filepath.Join(t.TempDir(), "fake-gdb")
	if e := os.WriteFile(path, []byte("#!/bin/sh\n"+script), 0700); e != nil {
		t.Fatal(e)
	}
	c, e := Start(path, nil)
	if e != nil {
		t.Fatal(e)
	}
	t.Cleanup(c.Close)
	return c
}
func TestMIClientCorrelatesConcurrentCommands(t *testing.T) {
	c := fakeMI(t, `while IFS= read -r line;do token=${line%%-*};printf '%s^done,value="7"\n' "$token";done`)
	var wg sync.WaitGroup
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			ctx, cancel := context.WithTimeout(context.Background(), time.Second)
			defer cancel()
			r, e := c.Command(ctx, "-test")
			if e != nil || r.Results["value"] != "7" || r.Token == "" {
				t.Errorf("correlation: %v %v", r, e)
			}
		}()
	}
	wg.Wait()
}
func TestMIClientTimeoutKillsAndReaps(t *testing.T) {
	c := fakeMI(t, `while IFS= read -r line;do :;done`)
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()
	if _, e := c.Command(ctx, "-never-answers"); e == nil {
		t.Fatal("uncertain command returned success")
	}
	select {
	case <-c.Done():
	case <-time.After(time.Second):
		t.Fatal("GDB not stopped after timeout")
	}
	c.Close()
	c.Close()
}
func TestMIClientRejectsMalformedResponse(t *testing.T) {
	c := fakeMI(t, `printf 'invalid\n'`)
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	if _, e := c.Command(ctx, "-test"); e == nil {
		t.Fatal("malformed response accepted")
	}
}
