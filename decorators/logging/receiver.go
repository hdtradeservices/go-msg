package logging

import (
	"context"

	"github.com/hdtradeservices/go-msg"
	"github.com/hdtradeservices/zapctx/zapctx"
	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
)

func Receiver(next msg.Receiver) msg.Receiver {
	return msg.ReceiverFunc(func(ctx context.Context, m *msg.Message) error {
		logLevel := m.Attributes.Get(zapctx.LogLevelKey)
		if logLevel != "" {
			logger := zapctx.Extract(ctx)
			logger = logger.WithOptions(
				zap.WrapCore(func(core zapcore.Core) zapcore.Core {
					return zapctx.NewCore(zapctx.ZapLevel(logLevel), "stdout")
				}),
			)
			ctx = zapctx.With(ctx, logger)
			ctx = context.WithValue(ctx, zapctx.LogLevelKey, logLevel)
		}
		traceID := m.Attributes.Get(zapctx.TraceIDKey)
		if traceID != "" {
			ctx = context.WithValue(ctx, zapctx.TraceIDKey, traceID)
		}
		return next.Receive(ctx, m)
	})
}
