package debugger

import (
	"context"
	"fmt"
	"net"
	"strings"
	"sync"
	"testing"
)

// TestBridgeDefaultPortPool checks the public range and OS bind exclusion while
// allowing other processes to allocate from the same pool concurrently.
func TestBridgeDefaultPortPool(t *testing.T) {
	seen := make(map[string]bool)
	for i := 0; i < 2; i++ {
		// Separate allocators must observe each other's live OS binds without
		// sharing reservation maps. Keep both endpoints open until cleanup.
		e, err := new(EndpointAllocator).Listen("", "", true)
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(e.Close)
		bound := e.listener.Addr().(*net.TCPAddr)
		if !bound.IP.Equal(net.IPv4(127, 0, 0, 1)) || bound.Port < 37100 || bound.Port > 37999 {
			t.Fatalf("default endpoint %s is outside the loopback port pool", bound)
		}
		address := e.Connection().Address
		if seen[address] {
			t.Fatalf("separate allocators reused an active endpoint: %s", address)
		}
		seen[address] = true
	}
}

func TestBridgePortExhaustionAndExplicitBind(t *testing.T) {
	occupied, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer occupied.Close()
	port := occupied.Addr().(*net.TCPAddr).Port
	a := EndpointAllocator{first: port, last: port}
	if _, err = a.Listen("", "", true); err == nil || !strings.Contains(err.Error(), "exhausted") {
		t.Fatalf("strict pool: %v", err)
	}
	if _, err = a.Listen(occupied.Addr().String(), "", false); err == nil || !strings.Contains(err.Error(), occupied.Addr().String()) {
		t.Fatalf("explicit occupied bind: %v", err)
	}
	fallback, err := a.Listen("", "", false)
	if err != nil {
		t.Fatal(err)
	}
	defer fallback.Close()
	if fallback.Connection().Address == occupied.Addr().String() || strings.HasSuffix(fallback.Connection().Address, ":0") {
		t.Fatal("fallback did not return actual distinct port")
	}
	// Explicit zero intentionally overrides strict pool policy.
	explicit, err := a.Listen("127.0.0.1:0", "", true)
	if err != nil {
		t.Fatal(err)
	}
	explicit.Close()
}

// smallBridgePortPool finds a contiguous test range without assuming that any
// well-known ports are free. Production allocation always uses the real bind.
func smallBridgePortPool(t *testing.T, size int) *EndpointAllocator {
	t.Helper()
	for attempt := 0; attempt < 32; attempt++ {
		first, err := net.Listen("tcp", "127.0.0.1:0")
		if err != nil {
			t.Fatal(err)
		}
		port := first.Addr().(*net.TCPAddr).Port
		listeners := []net.Listener{first}
		for offset := 1; offset < size && port+offset <= 65535; offset++ {
			listener, err := net.Listen("tcp", fmt.Sprintf("127.0.0.1:%d", port+offset))
			if err != nil {
				break
			}
			listeners = append(listeners, listener)
		}
		for _, listener := range listeners {
			listener.Close()
		}
		if len(listeners) == size {
			return &EndpointAllocator{first: port, last: port + size - 1}
		}
	}
	t.Fatal("no contiguous test port range available")
	return nil
}

func TestBridgePortCursorWrapsWithoutRestarting(t *testing.T) {
	a := smallBridgePortPool(t, 4)
	listen := func(port int) *Endpoint {
		t.Helper()
		e, err := a.Listen("", "", true)
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(e.Close)
		if want := fmt.Sprintf("127.0.0.1:%d", port); e.Connection().Address != want {
			t.Fatalf("allocated %s, want %s", e.Connection().Address, want)
		}
		return e
	}
	first := listen(a.first)
	first.Close()
	// A newly freed lower port must not reset the next allocation to the start.
	second := listen(a.first + 1)
	listen(a.first + 2)
	listen(a.last)
	listen(a.first)
	second.Close()
	listen(a.first + 1)
	if _, err := a.Listen("", "", true); err == nil || !strings.Contains(err.Error(), "exhausted") {
		t.Fatalf("full active pool: %v", err)
	}
	// The non-strict policy must also apply when our own operations fill the pool.
	fallback, err := a.Listen("", "", false)
	if err != nil {
		t.Fatal(err)
	}
	defer fallback.Close()
	port := fallback.listener.Addr().(*net.TCPAddr).Port
	if port >= a.first && port <= a.last {
		t.Fatalf("fallback reused an active pool port: %d", port)
	}
}

func TestBridgePortCursorSkipsActiveAndOccupiedPorts(t *testing.T) {
	a := smallBridgePortPool(t, 4)
	explicit, err := a.Listen(fmt.Sprintf("127.0.0.1:%d", a.first), "", true)
	if err != nil {
		t.Fatal(err)
	}
	defer explicit.Close()
	occupied, err := net.Listen("tcp", fmt.Sprintf("127.0.0.1:%d", a.first+1))
	if err != nil {
		t.Fatal(err)
	}
	defer occupied.Close()
	for _, port := range []int{a.first + 2, a.last, a.first + 1} {
		e, err := a.Listen("", "", true)
		if err != nil {
			t.Fatal(err)
		}
		defer e.Close()
		if want := fmt.Sprintf("127.0.0.1:%d", port); e.Connection().Address != want {
			t.Fatalf("allocated %s, want %s", e.Connection().Address, want)
		}
		// A failed OS bind is not a logical reservation. On the next wrap it
		// becomes eligible again, after the cursor has reached the pool's end.
		occupied.Close()
	}
}

func TestBridgePortHeldThroughAuthenticationAndDisconnect(t *testing.T) {
	var a EndpointAllocator
	e, err := a.Listen("127.0.0.1:0", "", false)
	if err != nil {
		t.Fatal(err)
	}
	defer e.Close()
	address := e.Connection().Address
	accepted := make(chan net.Conn, 1)
	go func() { conn, _ := e.Accept(context.Background()); accepted <- conn }()
	client, err := DialBridge(context.Background(), e.Connection())
	if err != nil {
		t.Fatal(err)
	}
	defer client.Close()
	server := <-accepted
	if server == nil {
		t.Fatal("authentication failed")
	}
	defer server.Close()
	for _, disconnected := range []bool{false, true} {
		if disconnected {
			client.Close()
			server.Close()
		}
		if _, err := a.Listen(address, "", false); err == nil || !strings.Contains(err.Error(), "reserved") {
			t.Fatalf("logical reservation after disconnect=%t: %v", disconnected, err)
		}
		if listener, err := net.Listen("tcp", address); err == nil {
			listener.Close()
			t.Fatal("OS listener released before operation ended")
		}
	}
	e.Close()
	e.Close()
	replacement, err := a.Listen(address, "", false)
	if err != nil {
		t.Fatalf("reservation leaked: %v", err)
	}
	replacement.Close()
	if len(a.held) != 0 {
		t.Fatal("allocator leaked records")
	}
}

func TestBridgeConcurrentPortAllocationAndRelease(t *testing.T) {
	var a EndpointAllocator
	const count = 64
	for round := 0; round < 3; round++ {
		results := make(chan *Endpoint, count)
		var wg sync.WaitGroup
		for i := 0; i < count; i++ {
			wg.Add(1)
			go func() {
				defer wg.Done()
				e, err := a.Listen("", "", false)
				if err != nil {
					t.Error(err)
					return
				}
				results <- e
			}()
		}
		wg.Wait()
		close(results)
		seen := map[string]bool{}
		for e := range results {
			address := e.Connection().Address
			if seen[address] {
				t.Errorf("duplicate address: %s", address)
			}
			seen[address] = true
			wg.Add(1)
			go func() { defer wg.Done(); e.Close(); e.Close() }()
		}
		wg.Wait()
		if len(seen) != count || len(a.held) != 0 || a.poolHeld != 0 {
			t.Fatalf("allocated=%d retained=%d pool-held=%d", len(seen), len(a.held), a.poolHeld)
		}
	}
}

func TestBridgeCloseKeepsBindUntilReservationRelease(t *testing.T) {
	var a EndpointAllocator
	e, err := a.Listen("127.0.0.1:0", "", false)
	if err != nil {
		t.Fatal(err)
	}
	address := e.Connection().Address
	release := e.release
	releasing, finish, done := make(chan struct{}), make(chan struct{}), make(chan struct{})
	// Pause finalization just before the allocator can publish the free address.
	// The OS must not make it eligible for another dynamic bind during this gap.
	e.release = func() { close(releasing); <-finish; release() }
	go func() { e.Close(); close(done) }()
	<-releasing
	probe, err := net.Listen("tcp", address)
	if err == nil {
		probe.Close()
		t.Error("OS bind released before the allocator could release its reservation")
	}
	close(finish)
	<-done
	replacement, err := a.Listen(address, "", false)
	if err != nil {
		t.Fatalf("finalization did not release the address: %v", err)
	}
	replacement.Close()
}

func TestBridgeConcurrentPortWrapAndReuse(t *testing.T) {
	const poolSize, workers, rounds = 8, 32, 16
	a := smallBridgePortPool(t, poolSize)
	var mu sync.Mutex
	active := make(map[string]bool)
	seen := make(map[int]bool)
	fallbacks := 0
	var ready, done sync.WaitGroup
	ready.Add(workers)
	done.Add(workers)
	release := make(chan struct{})
	for i := 0; i < workers; i++ {
		go func() {
			defer done.Done()
			for round := 0; round < rounds; round++ {
				e, err := a.Listen("", "", false)
				if round == 0 {
					// Fill the pool and exercise concurrent fallback before any
					// worker starts closing and allocating at the same time.
					ready.Done()
					<-release
				}
				if err != nil {
					t.Error(err)
					return
				}
				address := e.Connection().Address
				port := e.listener.Addr().(*net.TCPAddr).Port
				mu.Lock()
				if active[address] {
					t.Errorf("duplicate active endpoint: %s", address)
				}
				active[address] = true
				if port >= a.first && port <= a.last {
					seen[port] = true
				} else {
					fallbacks++
				}
				mu.Unlock()

				// Keep test bookkeeping atomic with Close so a newly bound
				// replacement cannot be mistaken for the previous operation.
				mu.Lock()
				e.Close()
				e.Close()
				delete(active, address)
				mu.Unlock()
			}
		}()
	}
	ready.Wait()
	close(release)
	done.Wait()
	if len(seen) != poolSize || fallbacks < workers-poolSize {
		t.Fatalf("pool ports used=%d, fallbacks=%d", len(seen), fallbacks)
	}
	if len(active) != 0 || len(a.held) != 0 || a.poolHeld != 0 {
		t.Fatalf("leaked active=%d reservations=%d pool-held=%d", len(active), len(a.held), a.poolHeld)
	}
}
