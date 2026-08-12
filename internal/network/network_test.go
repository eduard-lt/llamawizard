package network

import (
	"fmt"
	"net"
	"testing"
)

func TestIsFreeOccupiedPort(t *testing.T) {
	// Occupy a random high port with a real listener.
	l, err := net.Listen("tcp", "127.0.0.1:0") // OS picks an ephemeral port
	if err != nil {
		t.Fatalf("failed to listen on ephemeral port: %v", err)
	}
	addr := l.Addr().(*net.TCPAddr)
	occupiedPort := addr.Port
	_ = l.Close()

	// Re-occupy it so IsFree sees it in use.
	l2, err := net.Listen("tcp", fmt.Sprintf("127.0.0.1:%d", occupiedPort))
	if err != nil {
		t.Fatalf("failed to re-listen on %d: %v", occupiedPort, err)
	}

	if IsFree(occupiedPort) {
		t.Errorf("IsFree(%d) should be false for an occupied port", occupiedPort)
	}

	_ = l2.Close()

	// Now it should be free again.
	if !IsFree(occupiedPort) {
		t.Errorf("IsFree(%d) should be true after closing listener", occupiedPort)
	}
}

func TestIsFreeGenuinelyOpen(t *testing.T) {
	// Pick a random high port that's almost certainly free.
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	port := l.Addr().(*net.TCPAddr).Port
	_ = l.Close()

	// Now it should be free.
	if !IsFree(port) {
		t.Errorf("IsFree(%d) should be true for an open port", port)
	}
}

func TestSuggestAlternatives(t *testing.T) {
	// Occupy the preferred port.
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	preferred := l.Addr().(*net.TCPAddr).Port
	_ = l.Close()

	// Re-occupy so it stays busy during the test.
	l2, err := net.Listen("tcp", fmt.Sprintf("127.0.0.1:%d", preferred))
	if err != nil {
		t.Fatalf("re-listen: %v", err)
	}
	defer func() { _ = l2.Close() }()

	// SuggestAlternatives should skip the occupied port and return free ones.
	free := SuggestAlternatives(preferred, 3)

	if len(free) == 0 {
		t.Fatal("expected at least some free ports")
	}

	for _, p := range free {
		if p == preferred {
			t.Errorf("occupied port %d should not be in suggestions", preferred)
		}
		if !IsFree(p) {
			t.Errorf("suggested port %d is not actually free", p)
		}
	}

	t.Logf("preferred=%d (occupied), suggested=%v", preferred, free)
}

func TestSuggestAlternativesPreferredFree(t *testing.T) {
	// Use a random high port that's almost certainly free.
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	port := l.Addr().(*net.TCPAddr).Port
	_ = l.Close()

	free := SuggestAlternatives(port, 3)

	if len(free) == 0 {
		t.Fatal("expected at least some free ports")
	}

	// The preferred port should be first if it's free.
	if free[0] != port {
		t.Errorf("expected %d as first suggestion (it is free), got %d", port, free[0])
	}
}
