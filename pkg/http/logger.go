package retryablehttp

import (
	"go.uber.org/zap"
)

type LeveledLogger struct {
	logger *zap.SugaredLogger
}

func (l *LeveledLogger) Error(msg string, keysAndValues ...interface{}) {
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
