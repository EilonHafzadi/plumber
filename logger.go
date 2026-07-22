package main

import (
	"fmt"
	"path"

	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
)

var logger *zap.Logger

func initLogger() error {
	config := zap.Config{
		Level:       zap.NewAtomicLevelAt(zap.InfoLevel),
		Development: true,
		Encoding:    "console",

		EncoderConfig: zapcore.EncoderConfig{
			TimeKey:    "time",
			LevelKey:   "level",
			MessageKey: "msg",
			CallerKey:  "caller",

			EncodeLevel: zapcore.CapitalColorLevelEncoder,
			EncodeTime:  zapcore.TimeEncoderOfLayout("15:04:05"),

			// IMPORTANT: custom caller encoder
			EncodeCaller: func(caller zapcore.EntryCaller, enc zapcore.PrimitiveArrayEncoder) {
				// only keep file:line, drop full path
				enc.AppendString(fmt.Sprintf("%s:%d", path.Base(caller.File), caller.Line))
			},

			ConsoleSeparator: " ", // IMPORTANT: prevents tab alignment spacing
		},

		OutputPaths:      []string{"stdout"},
		ErrorOutputPaths: []string{"stderr"},
	}

	l, err := config.Build(zap.AddCaller())

	if err != nil {
		return fmt.Errorf("failed to setup logger: %w", err)
	}

	logger = l
	return nil
}
