// Package mem is an in-memory msg backend built on Go channels.
package mem

import (
	"context"
	"log"
	"sync"
	"sync/atomic"
	"time"

	"github.com/hdtradeservices/go-msg"
)

// Server subscribes to a channel and listens for Messages.
type Server struct {
	C chan *msg.Message

	// Concurrency is the maximum number of Messages that can be processed
	// concurrently by the Server.
	Concurrency int

	// maxConcurrentReceives is a buffered channel which acts as
	// a shared lock that limits the number of concurrent goroutines
	maxConcurrentReceives chan struct{}

	listenerCtx        context.Context
	listenerCancelFunc context.CancelFunc

	receiverCtx        context.Context
	receiverCancelFunc context.CancelFunc

	// requeueMu guards stopped: once Shutdown returns, the caller may close C,
	// so no worker may send to it.
	requeueMu sync.RWMutex
	stopped   bool
	inFlight  atomic.Int64
}

// Ensure that Server implements msg.Server
var _ msg.Server = &Server{}

// Serve always returns a non-nil error.
// After Shutdown, the returned error is ErrServerClosed
func (s *Server) Serve(_ context.Context, r msg.Receiver) error {
	defer close(s.maxConcurrentReceives)
	for {
		// Count this receive before checking the listener: Shutdown cancels it
		// before polling inFlight, so it never sees 0 while Serve can still take
		// a message.
		s.inFlight.Add(1)
		if s.listenerCtx.Err() != nil {
			s.inFlight.Add(-1)
			return msg.ErrServerClosed
		}

		// Take a slot before a message, so Serve never holds a message it cannot start.
		select {
		case <-s.listenerCtx.Done():
			s.inFlight.Add(-1)
			return msg.ErrServerClosed
		case s.maxConcurrentReceives <- struct{}{}:
		}

		select {
		case <-s.listenerCtx.Done():
			<-s.maxConcurrentReceives
			s.inFlight.Add(-1)
			return msg.ErrServerClosed

		case m, ok := <-s.C:
			if !ok || m == nil {
				<-s.maxConcurrentReceives
				s.inFlight.Add(-1)
				if !ok {
					// park rather than spin on the closed channel
					<-s.listenerCtx.Done()
					return msg.ErrServerClosed
				}
				continue
			}

			go func(ctx context.Context, m *msg.Message) {
				defer s.inFlight.Add(-1)
				err := r.Receive(ctx, m)
				// release before re-queueing: Serve may be waiting on this slot
				<-s.maxConcurrentReceives
				if err != nil {
					log.Printf("could not receive message %s", err)
					s.requeue(m)
				}
			}(s.receiverCtx, m)
		}
	}
}

// requeue puts a failed message back on C. It drops the message only when C is
// full and the Server is shutting down, or once Shutdown has returned.
func (s *Server) requeue(m *msg.Message) {
	s.requeueMu.RLock()
	defer s.requeueMu.RUnlock()
	if s.stopped {
		log.Printf("server shut down: dropping failed message")
		return
	}
	select {
	case s.C <- m:
		return
	default:
	}
	select {
	case s.C <- m:
	case <-s.listenerCtx.Done():
		log.Printf("server shutting down: dropping failed message")
	}
}

func (s *Server) stop() {
	s.requeueMu.Lock()
	s.stopped = true
	s.requeueMu.Unlock()
}

// shutdownPollInterval is how often we poll for quiescence
// during Server.Shutdown.
const shutdownPollInterval = 50 * time.Millisecond

// Shutdown attempts to gracefully shut down the Server without
// interrupting any messages in flight.
// When Shutdown is signalled, the Server stops polling for new Messages
// and then it waits for all of the active goroutines to complete.
//
// If the provided context expires before the shutdown is complete,
// then any remaining goroutines will be killed and the context's error
// is returned.
func (s *Server) Shutdown(ctx context.Context) error {
	if ctx == nil {
		panic("invalid context (nil)")
	}
	s.listenerCancelFunc()

	ticker := time.NewTicker(shutdownPollInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			s.receiverCancelFunc()
			s.stop()
			return ctx.Err()

		case <-ticker.C:
			if s.inFlight.Load() == 0 {
				s.stop()
				return msg.ErrServerClosed
			}
		}
	}
}

// NewServer creates and initializes a new Server.
func NewServer(c chan *msg.Message, cc int) *Server {
	listenerCtx, listenerCancelFunc := context.WithCancel(context.Background())
	receiverCtx, receiverCancelFunc := context.WithCancel(context.Background())

	srv := &Server{
		C:           c,
		Concurrency: cc,

		listenerCtx:           listenerCtx,
		listenerCancelFunc:    listenerCancelFunc,
		receiverCtx:           receiverCtx,
		receiverCancelFunc:    receiverCancelFunc,
		maxConcurrentReceives: make(chan struct{}, cc),
	}
	return srv
}
