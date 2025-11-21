package logging

import (
	"context"

	"github.com/hdtradeservices/go-msg"
	"github.com/hdtradeservices/zapctx/zapctx"
)

func Topic(next msg.Topic) msg.Topic {
	return msg.TopicFunc(func(ctx context.Context) msg.MessageWriter {
		w := next.NewWriter(ctx)
		level, ok := logLevel(ctx)
		if ok {
			w.Attributes().Set(zapctx.LogLevelKey, level)
		}
		traceID, ok := traceID(ctx)
		if ok {
			w.Attributes().Set(zapctx.TraceIDKey, traceID)
		}
		return w
	})
}

func logLevel(ctx context.Context) (string, bool) {
	if v := ctx.Value(zapctx.LogLevelKey); v != nil {
		if logLevel, ok := v.(string); ok {
			if logLevel != "" {
				return logLevel, true
			}
		}
	}
	return "", false
}

func traceID(ctx context.Context) (string, bool) {
	if v := ctx.Value(zapctx.TraceIDKey); v != nil {
		if traceID, ok := v.(string); ok {
			if traceID != "" {
				return traceID, true
			}
		}
	}
	return "", false
}
