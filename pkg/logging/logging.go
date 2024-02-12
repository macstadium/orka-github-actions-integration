package logging

import (
	"fmt"

	"go.uber.org/zap"
)

const (
	LogLevelDebug = "debug"
	LogLevelInfo  = "info"
	LogLevelWarn  = "warn"
	LogLevelError = "error"
)

var Logger *zap.SugaredLogger

func SetupLogger(logLevel string) {
	var cfg zap.Config = zap.NewDevelopmentConfig()

	cfg.EncoderConfig.EncodeCaller = nil

	switch logLevel {
	case LogLevelDebug:
		cfg.Level = zap.NewAtomicLevelAt(zap.DebugLevel)
	case LogLevelInfo:
		cfg.Level = zap.NewAtomicLevelAt(zap.InfoLevel)
	case LogLevelWarn:
		cfg.Level = zap.NewAtomicLevelAt(zap.WarnLevel)
	case LogLevelError:
		cfg.Level = zap.NewAtomicLevelAt(zap.ErrorLevel)
	default:
		cfg.Level = zap.NewAtomicLevelAt(zap.InfoLevel)
	}

	logger, err := cfg.Build()
	if err != nil {
		panic(fmt.Sprintf("unable to create logger %s", err))
	}

	Logger = logger.Sugar()
}
