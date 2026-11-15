package mcp

import (
	"encoding/json"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestRepairPreviewIsReadOnlyAndApplyPreservesUnrelatedSettings(t *testing.T) {
	for _, client := range []string{"codex", "claude", "opencode", "zoo"} {
		t.Run(client, func(t *testing.T) {
			root := t.TempDir()
			options := Options{Home: root, ConfigDir: root, WorkDir: root}
			target := RepairTarget{Client: client, Scope: "user"}
			endpoint := Endpoint{Transport: TransportStreamableHTTP, URL: "http://127.0.0.1:37099/mcp", Headers: map[string]string{"Authorization": "Bearer secret-for-test-only"}}
			plan, err := PreviewRepair(RepairLocations{Options: options}, target, endpoint)
			if err != nil {
				t.Fatal(err)
			}
			if entries, _ := os.ReadDir(root); len(entries) != 0 {
				t.Fatal("preview created files")
			}
			if plan.Existing || plan.Before != "null" || strings.Contains(plan.After, "secret-for-test") {
				t.Fatalf("unsafe preview: %+v", plan)
			}
			if err := os.MkdirAll(filepath.Dir(plan.Path), 0700); err != nil {
				t.Fatal(err)
			}
			original := []byte(`{"unrelated":{"number":9007199254740993,"secret":"unrelated-private-value"}}`)
			if client == "codex" {
				original = []byte("# preserved setting, formatting is explicitly normalized\nunrelated = 'unrelated-private-value'\n")
			}
			if err := os.WriteFile(plan.Path, original, 0600); err != nil {
				t.Fatal(err)
			}
			if err := plan.Apply(plan.Fingerprint); err == nil {
				t.Fatal("stale absent-file preview applied")
			}
			plan, err = PreviewRepair(RepairLocations{Options: options}, target, endpoint)
			if err != nil {
				t.Fatal(err)
			}
			expected, err := plan.ReplacementFingerprint()
			if err != nil {
				t.Fatal(err)
			}
			if err := plan.Apply(plan.Fingerprint); err != nil {
				t.Fatal(err)
			}
			actual, err := RepairFingerprint(plan.Path)
			if err != nil || actual != expected {
				t.Fatalf("replacement fingerprint = %s, %v", actual, err)
			}
			data, _ := os.ReadFile(plan.Path)
			if !strings.Contains(string(data), "unrelated-private-value") || !strings.Contains(string(data), "secret-for-test-only") {
				t.Fatal("settings or replacement missing")
			}
			if client != "codex" && !strings.Contains(string(data), "9007199254740993") {
				t.Fatal("unrelated JSON number lost precision")
			}
			next, err := PreviewRepair(RepairLocations{Options: options}, target, endpoint)
			if err != nil {
				t.Fatal(err)
			}
			if !next.Existing || next.ExistingCredential() != "secret-for-test-only" || next.ExistingURL() != endpoint.URL {
				t.Fatal("existing endpoint not recognized")
			}
			public, _ := json.Marshal(next)
			if strings.Contains(string(public), "secret-for-test") || strings.Contains(string(public), "unrelated-private") {
				t.Fatal("plan serialization leaked a credential")
			}
		})
	}
}

func TestRepairExplicitScopesAndOpenCodeVersionDoNotFollowWorkingDirectory(t *testing.T) {
	root := t.TempDir()
	options := Options{Home: root, ConfigDir: root, WorkDir: filepath.Join(root, "not-the-project")}
	for _, client := range []string{"claude", "opencode", "zoo"} {
		plan, err := PreviewRepair(RepairLocations{Options: options}, RepairTarget{Client: client, Scope: "project", Project: root}, DefaultEndpoint())
		if err != nil || strings.Contains(plan.Path, "not-the-project") {
			t.Fatalf("%s: %v", client, err)
		}
	}
	for _, target := range []RepairTarget{{Client: "codex", Scope: "project", Project: root}, {Client: "claude", Scope: "project"}, {Client: "claude", Scope: "user", Project: root}, {Client: "unknown", Scope: "user"}} {
		if _, err := PreviewRepair(RepairLocations{Options: options}, target, DefaultEndpoint()); err == nil {
			t.Fatalf("accepted %+v", target)
		}
	}
	plan, err := PreviewRepair(RepairLocations{Options: options}, RepairTarget{Client: "opencode", Scope: "user", OpenCodeV1: true}, DefaultEndpoint())
	if err != nil {
		t.Fatal(err)
	}
	if err := plan.Apply(plan.Fingerprint); err != nil {
		t.Fatal(err)
	}
	data, _ := os.ReadFile(plan.Path)
	var parsed map[string]any
	if err := json.Unmarshal(data, &parsed); err != nil {
		t.Fatal(err)
	}
	if _, ok := parsed["mcp"].(map[string]any)["channelterm"]; !ok {
		t.Fatal("V1 schema not selected")
	}
	if err := os.WriteFile(plan.Path+"c", []byte("{}"), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := PreviewRepair(RepairLocations{Options: options}, RepairTarget{Client: "opencode", Scope: "user"}, DefaultEndpoint()); err == nil {
		t.Fatal("edited shadowed JSON")
	}
}

func TestRepairRejectsDamagedOversizedAndChangedFilesWithoutOverwriting(t *testing.T) {
	root := t.TempDir()
	options := Options{Home: root, ConfigDir: root, WorkDir: root}
	target := RepairTarget{Client: "claude", Scope: "user"}
	path := filepath.Join(root, ".claude.json")
	for _, bad := range []string{"{broken", `{"mcpServers":[]}`, `{"mcpServers":{"channelterm":false}}`, `{"mcpServers":{},"mcpServers":{}}`, `{"nested":` + strings.Repeat("[", 65) + "0" + strings.Repeat("]", 65) + "}", "null", strings.Repeat(" ", repairConfigLimit+1)} {
		if err := os.WriteFile(path, []byte(bad), 0600); err != nil {
			t.Fatal(err)
		}
		if _, err := PreviewRepair(RepairLocations{Options: options}, target, DefaultEndpoint()); err == nil {
			t.Fatal("accepted unsafe input")
		}
		actual, _ := os.ReadFile(path)
		if string(actual) != bad {
			t.Fatal("damaged file changed")
		}
	}
	if err := os.WriteFile(path, []byte("{}"), 0600); err != nil {
		t.Fatal(err)
	}
	plan, err := PreviewRepair(RepairLocations{Options: options}, target, DefaultEndpoint())
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(`{"changed":true}`), 0600); err != nil {
		t.Fatal(err)
	}
	if err := plan.Apply(plan.Fingerprint); err == nil {
		t.Fatal("overwrote concurrent edit")
	}
	data, _ := os.ReadFile(path)
	if string(data) != `{"changed":true}` {
		t.Fatal("concurrent edit lost")
	}
}

func TestRepairRestrictsCredentialFilePermissions(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("Unix file modes do not validate Windows ACLs")
	}
	root := t.TempDir()
	path := filepath.Join(root, ".claude.json")
	if err := os.WriteFile(path, []byte("{}"), 0644); err != nil {
		t.Fatal(err)
	}
	plan, err := PreviewRepair(RepairLocations{Options: Options{Home: root}}, RepairTarget{Client: "claude", Scope: "user"}, DefaultEndpoint())
	if err != nil {
		t.Fatal(err)
	}
	if err := plan.Apply(plan.Fingerprint); err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(path)
	if err != nil || info.Mode().Perm() != 0600 {
		t.Fatalf("credential replacement must be private: %v, %v", info, err)
	}
	original, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	plan, err = PreviewRepair(RepairLocations{Options: Options{Home: root}}, RepairTarget{Client: "claude", Scope: "user"}, DefaultEndpoint())
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(root, 0500); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(root, 0700) })
	if os.Geteuid() == 0 {
		t.Skip("root bypasses read-only directory permissions")
	}
	if err := plan.Apply(plan.Fingerprint); err == nil {
		t.Fatal("replaced configuration in a read-only directory")
	}
	current, err := os.ReadFile(path)
	if err != nil || string(current) != string(original) {
		t.Fatal("failed replacement damaged the original configuration")
	}
}

func TestRepairRedactionAndEndpointBoundary(t *testing.T) {
	for _, value := range []string{"http://127.0.0.1:37099/mcp", "http://[::1]:1234/mcp"} {
		if !SafeRepairEndpoint(value) {
			t.Fatal(value)
		}
	}
	for _, value := range []string{"http://localhost:37099/mcp", "https://127.0.0.1/mcp", "http://127.0.0.1/mcp?token=secret", "http://user:secret@127.0.0.1/mcp", "http://192.0.2.1/mcp"} {
		if SafeRepairEndpoint(value) {
			t.Fatal(value)
		}
	}
	text := redactedEntry(map[string]any{"url": "http://user:secret@127.0.0.1/mcp", "command": "private-path", "args": []string{"secret"}, "unknown-secret-key": "secret", "headers": map[string]any{"X": "secret"}})
	for _, secret := range []string{"private-path", "secret", "unknown-secret-key"} {
		if strings.Contains(text, secret) {
			t.Fatalf("preview leaked %s", secret)
		}
	}
}
