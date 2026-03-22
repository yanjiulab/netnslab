package logx

import (
	"sync"

	"go.uber.org/zap"
)

var (
	logger     *zap.Logger
	initOnce   sync.Once
	sugar      *zap.SugaredLogger
	defaultCfg = zap.NewProductionConfig()
)

// Init initializes the global logger. It is safe to call multiple times.
func Init(debug bool) error {
	var err error

	initOnce.Do(func() {
		cfg := defaultCfg
		if debug {
			cfg = zap.NewDevelopmentConfig()
			cfg.Level = zap.NewAtomicLevelAt(zap.DebugLevel)
		}

		logger, err = cfg.Build()
		if err != nil {
			return
		}
		sugar = logger.Sugar()
	})

	return err
}

// L returns the underlying *zap.Logger. Init must be called before.
func L() *zap.Logger {
	if logger == nil {
		panic("logx: logger not initialized")
	}
	return logger
}

// S returns the global SugaredLogger. Init must be called before.
func S() *zap.SugaredLogger {
	if sugar == nil {
		panic("logx: logger not initialized")
	}
	return sugar
}

