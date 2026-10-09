package terminalinput

import (
	"context"
	"errors"
	"os"
	"reflect"
	"testing"
	"time"
)

// TestCheckedSize prevents minimized or oversized dimensions from wrapping into
// uint16 and reaching a remote PTY.
func TestCheckedSize(t *testing.T) {
	for _, dimensions := range [][2]int{{0, 24}, {80, 0}, {-1, 24}, {80, -1}, {65536, 24}, {80, 65536}, {132, 43}, {65535, 65535}} {
		size, err := checkedSize(dimensions[0], dimensions[1])
		valid := dimensions[0] > 0 && dimensions[0] <= 65535 && dimensions[1] > 0 && dimensions[1] <= 65535
		if valid != (err == nil) {
			t.Fatalf("checkedSize(%v) = %+v, %v", dimensions, size, err)
		}
		if valid && (int(size.Columns) != dimensions[0] || int(size.Rows) != dimensions[1]) {
			t.Fatalf("dimensions changed: %v -> %+v", dimensions, size)
		}
	}
}

// TestReadSizeRejectsPipe keeps non-terminal streams outside size monitoring.
func TestReadSizeRejectsPipe(t *testing.T) {
	reader, writer, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	defer reader.Close()
	defer writer.Close()
	if _, err := ReadSize(reader); err == nil {
		t.Fatal("pipe supplied terminal dimensions")
	}
}

// TestWatchSizeCoalescesAndRecovers checks setup-time changes, duplicate
// notifications, transient query failures, and invalid measurements.
func TestWatchSizeCoalescesAndRecovers(t *testing.T) {
	for _, initial := range []Size{{100, 36}, {80, 24}, {}} {
		steps := []Size{{100, 36}, {100, 36}, {}, {}, {132, 43}, {132, 43}, {80, 24}}
		index := 0
		var got []Size
		err := watchSize(context.Background(), initial, func() (Size, error) {
			if index == 2 {
				return Size{}, errors.New("temporary terminal query failure")
			}
			return steps[index], nil
		}, func(context.Context) bool {
			index++
			return index < len(steps)
		}, func(cols, rows uint16) error {
			got = append(got, Size{cols, rows})
			return nil
		})
		want := []Size{{132, 43}, {80, 24}}
		if initial != steps[0] {
			want = append([]Size{steps[0]}, want...)
		}
		if err != nil || !reflect.DeepEqual(got, want) {
			t.Fatalf("initial=%+v: changes=%+v, %v; want %+v", initial, got, err, want)
		}
	}
}

// TestWatchSizeStopsOnCancellationAndFailure prevents callbacks after shutdown
// and keeps transport errors visible to the owning command.
func TestWatchSizeStopsOnCancellationAndFailure(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	err := watchSize(ctx, Size{}, func() (Size, error) {
		t.Fatal("read after cancellation")
		return Size{}, nil
	}, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	failure := errors.New("connection closed")
	err = watchSize(context.Background(), Size{}, func() (Size, error) {
		return Size{132, 43}, nil
	}, func(context.Context) bool {
		t.Fatal("wait after failed resize")
		return false
	}, func(uint16, uint16) error { return failure })
	if !errors.Is(err, failure) {
		t.Fatalf("resize error = %v", err)
	}
}

// TestWatchSizeReleasesPlatformWait repeatedly cancels actual signal/timer waits
// and joins the watcher; it does not leave an adapter goroutine behind.
func TestWatchSizeReleasesPlatformWait(t *testing.T) {
	reader, writer, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	defer reader.Close()
	defer writer.Close()
	for range 20 {
		ctx, cancel := context.WithCancel(context.Background())
		done := make(chan error, 1)
		go func() { done <- WatchSize(ctx, reader, Size{}, func(uint16, uint16) error { return nil }) }()
		cancel()
		select {
		case err := <-done:
			if err != nil {
				t.Fatal(err)
			}
		case <-time.After(time.Second):
			t.Fatal("size watcher survived cancellation")
		}
	}
}

// TestWatchSizeCancellationDuringResize requires the owner to unblock callback
// I/O before joining, and prevents any queued event from sending a later resize.
func TestWatchSizeCancellationDuringResize(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	entered, release := make(chan struct{}), make(chan struct{})
	defer close(release)
	done := make(chan error, 1)
	go func() {
		done <- watchSize(ctx, Size{}, func() (Size, error) {
			return Size{132, 43}, nil
		}, func(context.Context) bool { return true }, func(uint16, uint16) error {
			close(entered)
			<-release
			return nil
		})
	}()
	select {
	case <-entered:
	case <-time.After(time.Second):
		t.Fatal("watcher did not call Resize")
	}
	cancel()
	select {
	case <-done:
		t.Fatal("watcher returned with its callback still running")
	default:
	}
	release <- struct{}{}
	select {
	case err := <-done:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(time.Second):
		t.Fatal("watcher did not stop after callback I/O was released")
	}
}
