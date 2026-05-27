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
