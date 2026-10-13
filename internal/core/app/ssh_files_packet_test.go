package app

import (
	"bytes"
	"context"
	"encoding/binary"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"testing"
	"time"

	sshtransport "github.com/akira-init1/ChannelTerm/internal/transport/ssh"
	gossh "golang.org/x/crypto/ssh"
)

// TestSFTPInvalidSizeDoesNotTruncateDownload verifies that unrepresentable
// remote metadata fails before an explicitly overwritable local file is opened.
func TestSFTPInvalidSizeDoesNotTruncateDownload(t *testing.T) {
	config := fileServerWithHandler(t, false, func(ch gossh.Channel) {
		for {
			var header [4]byte
			if _, err := io.ReadFull(ch, header[:]); err != nil {
				return
			}
			size := binary.BigEndian.Uint32(header[:])
			if size < 5 || size > 4096 {
				t.Error("invalid fixture request size")
				return
			}
			request := make([]byte, size)
			if _, err := io.ReadFull(ch, request); err != nil {
				return
			}
			var reply []byte
			switch request[0] {
			case 1: // INIT
				reply = []byte{2, 0, 0, 0, 3}
			case 7: // LSTAT: a regular file whose uint64 size cannot fit int64.
				reply = append([]byte{105}, request[1:5]...)
				reply = binary.BigEndian.AppendUint32(reply, 5) // SIZE | PERMISSIONS
				reply = binary.BigEndian.AppendUint64(reply, ^uint64(0))
				reply = binary.BigEndian.AppendUint32(reply, 0100644)
			case 3: // OPEN remains valid to expose premature local truncation.
				reply = append([]byte{102}, request[1:5]...)
				reply = binary.BigEndian.AppendUint32(reply, 1)
				reply = append(reply, 'h')
			case 4: // CLOSE
				reply = append([]byte{101}, request[1:5]...)
				reply = append(reply, make([]byte, 12)...)
			default:
				t.Errorf("unexpected fixture request %d", request[0])
				return
			}
			packet := binary.BigEndian.AppendUint32(nil, uint32(len(reply)))
			if _, err := ch.Write(append(packet, reply...)); err != nil {
				return
			}
		}
	})
	local := filepath.Join(t.TempDir(), "existing.bin")
	want := []byte("preserve-existing-content")
	if err := os.WriteFile(local, want, 0600); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	_, err := RunSSHFiles(ctx, SSHFilesRequest{Config: config, Protocol: "sftp", Operation: "get", Remote: "/fixture", Local: local, Overwrite: true})
	if err == nil {
		t.Fatal("unrepresentable size accepted")
	}
	got, readErr := os.ReadFile(local)
	if readErr != nil || !bytes.Equal(got, want) {
		t.Fatalf("invalid metadata changed destination: %q, %v", got, readErr)
	}
}

// TestSFTPRejectsAttributeAllocationAmplification sends a tiny malformed ATTRS
// reply through the real SSH/SFTP client. The count would allocate 16 MiB on
// the old decoder, safely below an OOM-sized attack but above the test budget.
// The parser must reject it based on packet length before allocating the array.
func TestSFTPRejectsAttributeAllocationAmplification(t *testing.T) {
	config := fileServerWithHandler(t, false, func(ch gossh.Channel) {
		readPacket := func() ([]byte, error) {
			var header [4]byte
			if _, err := io.ReadFull(ch, header[:]); err != nil {
				return nil, err
			}
			size := binary.BigEndian.Uint32(header[:])
			if size < 5 || size > 4096 {
				t.Errorf("invalid fixture packet size %d", size)
				return nil, io.ErrUnexpectedEOF
			}
			data := make([]byte, size)
			_, err := io.ReadFull(ch, data)
			return data, err
		}
		writePacket := func(data []byte) error {
			packet := binary.BigEndian.AppendUint32(nil, uint32(len(data)))
			packet = append(packet, data...)
			_, err := ch.Write(packet)
			return err
		}
		init, err := readPacket()
		if err != nil || init[0] != 1 {
			t.Errorf("SFTP init failed: %v", err)
			return
		}
		if err := writePacket([]byte{2, 0, 0, 0, 3}); err != nil {
			t.Error(err)
			return
		}
		request, err := readPacket()
		if err != nil || request[0] != 7 {
			t.Errorf("expected LSTAT request: %v", err)
			return
		}
		reply := append([]byte{105}, request[1:5]...)
		reply = binary.BigEndian.AppendUint32(reply, 0x80000000) // extended attributes
		reply = binary.BigEndian.AppendUint32(reply, 1<<19)      // impossible count, no entries
		if err := writePacket(reply); err != nil {
			t.Error(err)
			return
		}
		_, _ = io.Copy(io.Discard, ch)
	})
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	transport, err := sshtransport.New(config)
	if err != nil {
		t.Fatal(err)
	}
	client, err := transport.OpenFiles(ctx, "sftp")
	if err != nil {
		t.Fatal(err)
	}
	defer client.Close()
	var before, after runtime.MemStats
	runtime.ReadMemStats(&before)
	_, err = client.Stat("/fixture")
	runtime.ReadMemStats(&after)
	if err == nil {
		t.Fatal("malformed SFTP attributes accepted")
	}
	if allocated := after.TotalAlloc - before.TotalAlloc; allocated >= 8<<20 {
		t.Fatalf("tiny malformed attribute packet allocated %d bytes (budget < 8 MiB)", allocated)
	}
}
