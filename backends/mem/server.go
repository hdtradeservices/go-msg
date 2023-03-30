package mem

import (
	"context"
	"log"
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
}

// Ensure that Server implements msg.Server
var _ msg.Server = &Server{}

// Serve always returns a non-nil error.
// After Shutdown, the returned error is ErrServerClosed
func (s *Server) Serve(ctx context.Context, r msg.Receiver) error {
	for {
		select {
		case <-s.listenerCtx.Done():
			close(s.maxConcurrentReceives)
			return msg.ErrServerClosed

		case m := <-s.C:
			if m == nil {
				continue
			}

			// acquire "lock"
			s.maxConcurrentReceives <- struct{}{}

			go func(ctx context.Context, m *msg.Message) {
				defer func() {
					<-s.maxConcurrentReceives
				}()

				if err := r.Receive(ctx, m); err != nil {
					log.Printf("could not receive message %s", err)
					s.C <- m
				}
			}(s.receiverCtx, m)
		}
	}
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
			return ctx.Err()

		case <-ticker.C:
			if len(s.maxConcurrentReceives) == 0 {
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
