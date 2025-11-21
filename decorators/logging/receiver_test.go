package logging_test

import (
	"context"
	"testing"

	"github.com/hdtradeservices/go-msg"
	"github.com/hdtradeservices/go-msg/decorators/logging"
	"github.com/hdtradeservices/go-test/assert"
)

type ChanReceiver struct {
	ll  chan string
	tid chan string
}

func (r ChanReceiver) Receive(ctx context.Context, m *msg.Message) error {
	logLevel := ctx.Value("z-log-level").(string)
	r.ll <- logLevel
	traceID := ctx.Value("z-trace-id").(string)
	r.tid <- traceID
	return nil
}

func Test_LogLevel_LevelSet(t *testing.T) {
	assert := assert.New(t)
	testFinish := make(chan struct{})
	logLevelChan := make(chan string)
	traceIDChan := make(chan string)
	r := logging.Receiver(ChanReceiver{
		ll:  logLevelChan,
		tid: traceIDChan,
	})

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	attrs := msg.Attributes{}
	attrs.Set("z-log-level", "debug")
	attrs.Set("z-trace-id", "trace-1234")
	// Construct a message with log level set
	m := &msg.Message{
		Body:       nil,
		Attributes: attrs,
	}

	// Wait for ChanReceiver to write the message to msgChan, assert on the attributes
	go func() {
		actLogLevel := <-logLevelChan
		expectedLogLevel := "debug"
		assert.Equal(expectedLogLevel, actLogLevel)
		actTraceID := <-traceIDChan
		expectedTraceID := "trace-1234"
		assert.Equal(expectedTraceID, actTraceID)
		testFinish <- struct{}{}
	}()

	err := r.Receive(ctx, m)
	assert.NoError(err)
}
