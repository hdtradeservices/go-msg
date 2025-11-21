package logging_test

import (
	"context"
	"testing"

	"github.com/hdtradeservices/go-msg"
	"github.com/hdtradeservices/go-msg/backends/mem"
	"github.com/hdtradeservices/go-msg/decorators/logging"
	"github.com/hdtradeservices/go-test/assert"
	"github.com/hdtradeservices/zapctx/zapctx"
)

func Test_LogLevel(t *testing.T) {
	ctx := context.Background()
	assert := assert.New(t)
	logger := zapctx.NewLogger("DEBUG", "stdout")
	ctx = zapctx.With(ctx, logger)

	c := make(chan *msg.Message, 2)

	// setup topics
	t1 := &mem.Topic{C: c}
	t2 := logging.Topic(t1)

	ctx = context.WithValue(ctx, "z-log-level", "debug")
	ctx = context.WithValue(ctx, "z-trace-id", "trace-1234")
	w := t2.NewWriter(ctx)
	w.Write([]byte("hello,"))
	w.Write([]byte("world!"))
	w.Close()

	m := <-c
	actLogLevel := m.Attributes.Get("z-log-level")
	assert.Equal("debug", actLogLevel)
	actTraceID := m.Attributes.Get("z-trace-id")
	assert.Equal("trace-1234", actTraceID)
}
