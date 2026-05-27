package retryablehttp

import (
	"context"
	"errors"

	"go.uber.org/zap"
)

type LeveledLogger struct {
	logger *zap.SugaredLogger
}

func (l *LeveledLogger) Error(msg string, keysAndValues ...interface{}) {
	// go-retryablehttp logs every failed request at error level, including ones
	// that fail purely because the caller's context was cancelled (e.g. during
	// graceful shutdown). Downgrade those to debug to avoid alarming noise.
	for i := 1; i < len(keysAndValues); i += 2 {
		if err, ok := keysAndValues[i].(error); ok && errors.Is(err, context.Canceled) {
			l.logger.Debugw(msg, keysAndValues...)
			return
		}
	}

	l.logger.Errorw(msg, keysAndValues...)
}

func (l *LeveledLogger) Info(msg string, keysAndValues ...interface{}) {
	l.logger.Infow(msg, keysAndValues...)
}

func (l *LeveledLogger) Debug(msg string, keysAndValues ...interface{}) {
	l.logger.Debugw(msg, keysAndValues...)
}

func (l *LeveledLogger) Warn(msg string, keysAndValues ...interface{}) {
	l.logger.Warnw(msg, keysAndValues...)
}
