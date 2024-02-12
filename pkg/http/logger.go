package retryablehttp

import (
	"go.uber.org/zap"
)

type LeveledLogger struct {
	logger *zap.SugaredLogger
}

func (l *LeveledLogger) Error(msg string, keysAndValues ...interface{}) {
	l.logger.With(keysAndValues...).Error(msg)
}

func (l *LeveledLogger) Info(msg string, keysAndValues ...interface{}) {
	l.logger.With(keysAndValues...).Info(msg)
}

func (l *LeveledLogger) Debug(msg string, keysAndValues ...interface{}) {
	l.logger.With(keysAndValues...).Debug(msg)
}

func (l *LeveledLogger) Warn(msg string, keysAndValues ...interface{}) {
	l.logger.With(keysAndValues...).Warn(msg)
}
