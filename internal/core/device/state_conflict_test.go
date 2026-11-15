package device

import (
	"path/filepath"
	"reflect"
	"testing"
)

func TestStateStoreRejectsEveryLiveClaimantRegardlessOfScanOrder(t *testing.T) {
	for _, method := range []IdentityMethod{IdentityUSBSerial, IdentityUSBPath} {
		for _, existing := range []bool{false, true} {
			t.Run(string(method)+map[bool]string{false: "-new", true: "-stored"}[existing], func(t *testing.T) {
				store, err := LoadStateStore(filepath.Join(t.TempDir(), "state.json"))
				if err != nil {
					t.Fatal(err)
				}
				a := Endpoint{Transport: "serial", Endpoint: "COM1", Metadata: SerialMetadata{VID: "1234", PID: "5678", USBPath: "path-A"}}
				if method == IdentityUSBSerial {
					a.Metadata.USBSerial = "duplicate"
				}
				b := a
				b.Endpoint = "COM2"
				if method == IdentityUSBSerial {
					b.Metadata.USBPath = "path-B"
				}
				b.Metadata.Product = "Different diagnostics"
				persistentID := ""
				if existing {
					persistentID = mustResolve(t, store, a)[deviceKey(a)].DeviceID
				}
				before := cloneStateFile(store.state)
				first := mustResolve(t, store, a, b)
				if first[deviceKey(a)].DeviceID == first[deviceKey(b)].DeviceID {
					t.Fatal("conflicting endpoints share an ID")
				}
				for _, identity := range first {
					if identity.Method != IdentityRuntime || identity.Persistent || identity.DeviceID == persistentID {
						t.Fatalf("ambiguous identity = %#v", identity)
					}
				}
				second := mustResolve(t, store, b, a)
				if !reflect.DeepEqual(first, second) {
					t.Fatalf("scan order changed identities: %#v -> %#v", first, second)
				}
				if !reflect.DeepEqual(before, cloneStateFile(store.state)) {
					t.Fatal("ambiguous scan changed persistent state")
				}
				recovered := mustResolve(t, store, a)[deviceKey(a)]
				if !recovered.Persistent || recovered.Method != method || (existing && recovered.DeviceID != persistentID) {
					t.Fatalf("resolved conflict = %#v", recovered)
				}
			})
		}
	}
}
