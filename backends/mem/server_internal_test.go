package mem

import (
	"bytes"
	"context"
	"errors"
	"sync/atomic"
	"testing"
	"time"

	"github.com/hdtradeservices/go-msg"
)

// TestServer_ShutdownLeavesWaitingMessage starts Shutdown while Serve waits for
// a slot, and expects the next message to stay on C.
func TestServer_ShutdownLeavesWaitingMessage(t *testing.T) {
	c := make(chan *msg.Message, 2)
	srv := NewServer(c, 1)
	started := make(chan struct{})
	release := make(chan struct{})
	var calls atomic.Int32
	r := msg.ReceiverFunc(func(context.Context, *msg.Message) error {
		if calls.Add(1) == 1 {
			close(started)
			<-release
		}
		return nil
	})

	c <- &msg.Message{Attributes: msg.Attributes{}, Body: bytes.NewBufferString("m1")}
	go srv.Serve(context.Background(), r) //nolint:errcheck
	<-started
	c <- &msg.Message{Attributes: msg.Attributes{}, Body: bytes.NewBufferString("m2")}
	deadline := time.Now().Add(2 * time.Second)
	for srv.inFlight.Load() < 2 { // Serve waits for m1's slot
		if time.Now().After(deadline) {
			t.Fatal("Serve never waited for a slot")
		}
		time.Sleep(time.Millisecond)
	}

	shutdown := make(chan error, 1)
	go func() { shutdown <- srv.Shutdown(context.Background()) }()
	<-srv.listenerCtx.Done()
	close(release)

	select {
	case err := <-shutdown:
		if !errors.Is(err, msg.ErrServerClosed) {
			t.Fatalf("Shutdown returned %v, want %v", err, msg.ErrServerClosed)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("Shutdown did not return")
	}
	if n := calls.Load(); n != 1 {
		t.Errorf("Receive called %d times, want 1: m2 ran after Shutdown began", n)
	}
	if len(c) != 1 {
		t.Errorf("C holds %d messages, want m2 left on C", len(c))
	}
}
