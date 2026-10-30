// Package logging provides decorators that carry zapctx's log level and trace ID
// across a message, as message attributes.
package logging

import (
	"context"

	"github.com/hdtradeservices/go-msg"
	"github.com/hdtradeservices/zapctx/zapctx"
	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
)

// Receiver reads the log level and trace ID attributes off each message and
// stores both on the context. When a level is set, it also replaces the context's
// logger with a new stdout logger at that level.
func Receiver(next msg.Receiver) msg.Receiver {
	return msg.ReceiverFunc(func(ctx context.Context, m *msg.Message) error {
		logger := zapctx.Extract(ctx)
		logLevel := m.Attributes.Get(zapctx.LogLevelKey)
		if logLevel != "" {
			logger = logger.WithOptions(
				zap.WrapCore(func(_ zapcore.Core) zapcore.Core {
					return zapctx.NewCore(zapctx.ZapLevel(logLevel), "stdout")
				}),
			)
			ctx = zapctx.With(ctx, logger)
			ctx = context.WithValue(ctx, zapctx.LogLevelKey, logLevel)
		}
		traceID := m.Attributes.Get(zapctx.TraceIDKey)
		if traceID != "" {
			logger = logger.With(zap.String(zapctx.TraceIDKey, traceID))
			ctx = zapctx.With(ctx, logger)
			ctx = context.WithValue(ctx, zapctx.TraceIDKey, traceID)
		}
		return next.Receive(ctx, m)
	})
}
