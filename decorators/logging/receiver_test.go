package logging_test

import (
	"context"
	"testing"

	"github.com/hdtradeservices/go-msg"
	"github.com/hdtradeservices/go-msg/decorators/logging"
	"github.com/hdtradeservices/go-test/assert"
	"github.com/hdtradeservices/zapctx/zapctx"
	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
	"go.uber.org/zap/zaptest/observer"
)

type ChanReceiver struct {
	ll  chan string
	tid chan string
}

func (r ChanReceiver) Receive(ctx context.Context, _ *msg.Message) error {
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

// Test_LogLevel_KeepsSink asserts that a message's log level lowers the
// threshold of the context's logger without replacing its sink or fields.
func Test_LogLevel_KeepsSink(t *testing.T) {
	assert := assert.New(t)
	core, logs := observer.New(zapcore.WarnLevel)
	ctx := zapctx.With(context.Background(), zap.New(core).With(zap.String("service", "test")))

	attrs := msg.Attributes{}
	attrs.Set(zapctx.LogLevelKey, "DEBUG")
	r := logging.Receiver(msg.ReceiverFunc(func(ctx context.Context, _ *msg.Message) error {
		zapctx.Extract(ctx).Debug("debug line")
		return nil
	}))
	assert.NoError(r.Receive(ctx, &msg.Message{Attributes: attrs}))

	entries := logs.FilterMessage("debug line").All()
	assert.Equal(1, len(entries))
	if len(entries) == 1 {
		assert.Equal("test", entries[0].ContextMap()["service"])
	}
}
