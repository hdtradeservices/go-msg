package mem_test

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"sync/atomic"
	"syscall"
	"testing"
	"time"

	"github.com/hdtradeservices/go-msg"
	"github.com/hdtradeservices/go-msg/backends/mem"
)

// ConcurrentReceiver writes to an channel upon consumption of a Message.
// It is safe to utilize by concurrent goroutines.
type ConcurrentReceiver struct {
	t *testing.T
	C chan struct{}
}

func (r *ConcurrentReceiver) Receive(ctx context.Context, _ *msg.Message) error {
	select {
	case <-ctx.Done():
		return ctx.Err()
	default:
		r.C <- struct{}{}
	}
	return nil
}

// RetryReceiver returns an error upon consumption of a Message. Once it
// has been called a certain number of times, it writes to an channel and
// returns nil.
//
// It it safe to utilize by concurrent goroutines.
type RetryReceiver struct {
	t *testing.T
	C chan struct{}

	allowedRetries int
	calls          int
}

func (r *RetryReceiver) Receive(ctx context.Context, _ *msg.Message) error {
	select {
	case <-ctx.Done():
		r.t.Fatalf("[ERROR] Receiver could not finish executing")
	default:
	}

	if r.calls == r.allowedRetries {
		r.C <- struct{}{}
		return nil
	}
	r.calls++

	return errors.New("could not complete transaction")
}

// TestServer_Serve asserts that the Server can process all messages that
// have been sent to its input buffer using a single goroutine
func TestServer_Serve(t *testing.T) {
	messages := []*msg.Message{
		{
			Attributes: msg.Attributes{},
			Body:       bytes.NewBufferString("message #1: hello world!"),
		},
		{
			Attributes: msg.Attributes{},
			Body:       bytes.NewBufferString("message #2: foo bar"),
		},
		{
			Attributes: msg.Attributes{},
			Body:       bytes.NewBufferString("message #3: gophercon9000"),
		},
	}

	srv := mem.NewServer(make(chan *msg.Message, len(messages)), 1)

	for _, m := range messages {
		srv.C <- m
	}
	defer close(srv.C)

	ctx := context.Background()
	outputChannel := make(chan struct{})
	go func() {
		srv.Serve(ctx, &ConcurrentReceiver{
			t: t,
			C: outputChannel,
		})
	}()

	// block until all requests have been made
	calls := 0
	for range outputChannel {
		calls++
		if calls == len(messages) {
			close(outputChannel)
		}
	}
}

// TestServer_Serve asserts that the Server can process all messages that
// have been sent to its input buffer using lots of concurrent goroutines
func TestServer_ServeConcurrency(t *testing.T) {
	var messages [10000]*msg.Message

	for i := 0; i < len(messages); i++ {
		messages[i] = &msg.Message{
			Attributes: msg.Attributes{},
			Body:       bytes.NewBufferString(fmt.Sprintf("this is a test message #%d", i)),
		}
	}

	srv := mem.NewServer(make(chan *msg.Message, len(messages)), 10)

	for _, m := range messages {
		srv.C <- m
	}
	defer close(srv.C)

	outputChannel := make(chan struct{})
	ctx := context.Background()
	go func() {
		srv.Serve(ctx, &ConcurrentReceiver{
			t: t,
			C: outputChannel,
		})
	}()

	// block until all requests have been made
	calls := 0
	for range outputChannel {
		calls++
		if calls == len(messages) {
			close(outputChannel)
		}
	}
}

// TestServer_ServeCanRetryMessages asserts that the Server can handle
// errors returned by the Receiver and "retry" the message that caused
// the error
func TestServer_ServeCanRetryMessages(t *testing.T) {
	messages := []*msg.Message{
		{
			Attributes: msg.Attributes{},
			Body:       bytes.NewBufferString("message #1: hello world!"),
		},
	}

	srv := mem.NewServer(make(chan *msg.Message, len(messages)), 1)

	for _, m := range messages {
		srv.C <- m
	}
	defer close(srv.C)

	outputChannel := make(chan struct{})
	ctx := context.Background()
	go func() {
		srv.Serve(ctx, &RetryReceiver{
			t: t,
			C: outputChannel,

			calls:          0,
			allowedRetries: 10,
		})
	}()

	// after 10th retry receiver will write to channel
	timeout, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	select {
	case <-timeout.Done():
		t.Fatalf("context timed out")
	case <-outputChannel:
		close(outputChannel)
	}
}

// TestServer_ShutdownContextTimeoutExceeded tests that the Server can
// use a Context to shut down before all of it's goroutines have finished.
func TestServer_ShutdownContextTimeoutExceeded(t *testing.T) {
	done := make(chan struct{})

	message := &msg.Message{
		Attributes: msg.Attributes{},
		Body:       bytes.NewBufferString("hello world!"),
	}

	srv := mem.NewServer(make(chan *msg.Message, 1), 1)
	defer close(srv.C)

	receiver := msg.ReceiverFunc(func(_ context.Context, _ *msg.Message) error {
		defer close(done)

		// set a fast timeout so srv doesn't have enough time to finish
		sctx, cancel := context.WithTimeout(context.Background(), 1*time.Second)
		defer cancel()

		if err := srv.Shutdown(sctx); err == msg.ErrServerClosed {
			t.Fatal("expected context timeout")
		}
		return nil
	})
	srv.C <- message

	ctx := context.Background()
	if err := srv.Serve(ctx, receiver); err != msg.ErrServerClosed {
		t.Fatalf("expected %v, got %v", msg.ErrServerClosed, err)
	}

	<-done
}

func newMessage(body string) *msg.Message {
	return &msg.Message{Attributes: msg.Attributes{}, Body: bytes.NewBufferString(body)}
}

func push(t *testing.T, c chan *msg.Message, m *msg.Message) {
	t.Helper()
	select {
	case c <- m:
	case <-time.After(2 * time.Second):
		t.Fatal("setup: push blocked")
	}
}

// TestServer_RequeueToFullChannelDoesNotDeadlock fails a message while the
// channel is full and Serve is waiting for the failing worker's slot.
func TestServer_RequeueToFullChannelDoesNotDeadlock(t *testing.T) {
	c := make(chan *msg.Message, 1)
	srv := mem.NewServer(c, 1)
	defer func() {
		ctx, cancel := context.WithTimeout(context.Background(), time.Second)
		defer cancel()
		_ = srv.Shutdown(ctx)
	}()

	release := make(chan struct{})
	var calls atomic.Int32
	r := msg.ReceiverFunc(func(_ context.Context, _ *msg.Message) error {
		if calls.Add(1) == 1 {
			<-release
			return errors.New("transient failure")
		}
		return nil
	})

	push(t, c, newMessage("m1"))
	go srv.Serve(context.Background(), r) //nolint:errcheck
	for calls.Load() == 0 {
		time.Sleep(time.Millisecond)
	}
	push(t, c, newMessage("m2"))
	go func() { c <- newMessage("m3") }() // fills C once m2 is taken
	time.Sleep(20 * time.Millisecond)     // lets C fill before m1 fails
	close(release)                        // m1 fails and is re-queued

	deadline := time.After(2 * time.Second)
	for calls.Load() < 4 {
		select {
		case <-deadline:
			t.Fatalf("deadlock: %d Receive call(s) in 2s, want 4 (m1 twice, m2, m3)", calls.Load())
		case <-time.After(10 * time.Millisecond):
		}
	}
}

// TestServer_NoRequeueAfterShutdownTimeout closes C once Shutdown has timed out,
// while a cancelled receiver is still failing. A re-queue onto the closed channel
// panics, so the scenario runs in a child process.
func TestServer_NoRequeueAfterShutdownTimeout(t *testing.T) {
	if os.Getenv("MEM_SERVER_SHUTDOWN_CHILD") == "1" {
		c := make(chan *msg.Message, 1)
		srv := mem.NewServer(c, 1)
		started := make(chan struct{})
		closed := make(chan struct{})
		r := msg.ReceiverFunc(func(ctx context.Context, _ *msg.Message) error {
			close(started)
			<-ctx.Done()
			<-closed // still unwinding when the caller tears down
			return ctx.Err()
		})
		c <- newMessage("m1")
		go srv.Serve(context.Background(), r) //nolint:errcheck
		<-started
		ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
		defer cancel()
		if err := srv.Shutdown(ctx); !errors.Is(err, context.DeadlineExceeded) {
			t.Fatalf("expected Shutdown to time out, got %v", err)
		}
		close(c) // Shutdown has returned, so the caller owns C again
		close(closed)
		time.Sleep(500 * time.Millisecond)
		return
	}
	cmd := exec.Command(os.Args[0], "-test.run=^TestServer_NoRequeueAfterShutdownTimeout$")
	cmd.Env = append(os.Environ(), "MEM_SERVER_SHUTDOWN_CHILD=1")
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("child failed: %v\n%s", err, out)
	}
}

// TestServer_ServeAfterShutdownLeavesC asserts that a Serve started after
// Shutdown returns takes nothing from C.
func TestServer_ServeAfterShutdownLeavesC(t *testing.T) {
	c := make(chan *msg.Message, 1)
	srv := mem.NewServer(c, 1)
	if err := srv.Shutdown(context.Background()); !errors.Is(err, msg.ErrServerClosed) {
		t.Fatalf("Shutdown returned %v, want %v", err, msg.ErrServerClosed)
	}
	c <- newMessage("m")
	err := srv.Serve(context.Background(), msg.ReceiverFunc(func(context.Context, *msg.Message) error { return nil }))
	if !errors.Is(err, msg.ErrServerClosed) || len(c) != 1 {
		t.Fatalf("Serve returned %v with %d message(s) on C, want %v and 1", err, len(c), msg.ErrServerClosed)
	}
}

func cpuTime() time.Duration {
	var ru syscall.Rusage
	_ = syscall.Getrusage(syscall.RUSAGE_SELF, &ru)
	return time.Duration(ru.Utime.Nano() + ru.Stime.Nano())
}

// TestServer_ClosedChannelDoesNotSpin closes C under a running Serve, which must
// wait for Shutdown rather than loop on the closed channel.
func TestServer_ClosedChannelDoesNotSpin(t *testing.T) {
	c := make(chan *msg.Message, 1)
	srv := mem.NewServer(c, 1)
	served := make(chan error, 1)
	go func() {
		served <- srv.Serve(context.Background(), msg.ReceiverFunc(func(context.Context, *msg.Message) error { return nil }))
	}()
	time.Sleep(50 * time.Millisecond)

	close(c)
	start := cpuTime()
	time.Sleep(300 * time.Millisecond)
	if spin := cpuTime() - start; spin > 100*time.Millisecond {
		t.Errorf("busy loop: %v CPU in 300ms after C was closed", spin)
	}

	_ = srv.Shutdown(context.Background())
	select {
	case err := <-served:
		if !errors.Is(err, msg.ErrServerClosed) {
			t.Errorf("Serve returned %v, want %v", err, msg.ErrServerClosed)
		}
	case <-time.After(time.Second):
		t.Error("Serve did not return after Shutdown")
	}
}
