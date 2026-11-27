package logger

import (
	"io"
	"os"
	"path/filepath"
	"strings"

	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
)

const maxLogFileBytes int64 = 20 * 1024 * 1024

// New constructs a configured Uber Zap logger for production (JSON) or development (console).
func New(env, level, format string) (*zap.Logger, error) {
	cfg := buildZapConfig(env, level, format)
	return cfg.Build()
}

// NewWithFile builds a Zap logger that writes to stdout and optionally tees to logFilePath (rotating at >20 MiB).
func NewWithFile(env, level, format, logFilePath string) (*zap.Logger, io.Closer, error) {
	lvl := parseLevel(level)
	isJSON := strings.EqualFold(env, "production") || strings.EqualFold(format, "json")

	consoleEncCfg := baseEncoderConfig(isJSON)
	if !isJSON {
		consoleEncCfg.EncodeLevel = zapcore.CapitalColorLevelEncoder
	}
	var consoleEnc zapcore.Encoder
	if isJSON {
		consoleEnc = zapcore.NewJSONEncoder(consoleEncCfg)
	} else {
		consoleEnc = zapcore.NewConsoleEncoder(consoleEncCfg)
	}
	cores := []zapcore.Core{
		zapcore.NewCore(consoleEnc, zapcore.Lock(os.Stdout), lvl),
	}

	var fileCloser io.Closer
	var fileOpenErr error
	if p := strings.TrimSpace(logFilePath); p != "" {
		if f, err := openRotatingLogFile(p); err != nil {
			fileOpenErr = err
		} else {
			fileCloser = f
			fileEncCfg := baseEncoderConfig(isJSON)
			var fileEnc zapcore.Encoder
			if isJSON {
				fileEnc = zapcore.NewJSONEncoder(fileEncCfg)
			} else {
				fileEnc = zapcore.NewConsoleEncoder(fileEncCfg)
			}
			cores = append(cores, zapcore.NewCore(fileEnc, zapcore.AddSync(f), lvl))
		}
	}

	log := zap.New(zapcore.NewTee(cores...), zap.AddCaller(), zap.AddStacktrace(zapcore.ErrorLevel))
	return log, fileCloser, fileOpenErr
}

func buildZapConfig(env, level, format string) zap.Config {
	lvl := parseLevel(level)
	var cfg zap.Config
	if strings.EqualFold(env, "production") || strings.EqualFold(format, "json") {
		cfg = zap.NewProductionConfig()
		cfg.Encoding = "json"
	} else {
		cfg = zap.NewDevelopmentConfig()
		cfg.Encoding = "console"
		cfg.EncoderConfig.EncodeLevel = zapcore.CapitalColorLevelEncoder
	}
	cfg.Level = zap.NewAtomicLevelAt(lvl)
	cfg.EncoderConfig.TimeKey = "ts"
	cfg.EncoderConfig.EncodeTime = zapcore.ISO8601TimeEncoder
	cfg.EncoderConfig.CallerKey = "caller"
	return cfg
}

func baseEncoderConfig(isJSON bool) zapcore.EncoderConfig {
	var enc zapcore.EncoderConfig
	if isJSON {
		enc = zap.NewProductionEncoderConfig()
	} else {
		enc = zap.NewDevelopmentEncoderConfig()
	}
	enc.TimeKey = "ts"
	enc.EncodeTime = zapcore.ISO8601TimeEncoder
	enc.CallerKey = "caller"
	return enc
}

func parseLevel(level string) zapcore.Level {
	lvl := zapcore.InfoLevel
	if err := lvl.UnmarshalText([]byte(strings.ToLower(strings.TrimSpace(level)))); err != nil {
		return zapcore.InfoLevel
	}
	return lvl
}

func openRotatingLogFile(logFilePath string) (*os.File, error) {
	if err := os.MkdirAll(filepath.Dir(logFilePath), 0o755); err != nil {
		return nil, err
	}
	if info, err := os.Stat(logFilePath); err == nil && info.Size() > maxLogFileBytes {
		_ = os.Rename(logFilePath, logFilePath+".1")
	}
	return os.OpenFile(logFilePath, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o640)
}

// RedactSecret masks sensitive strings so secrets never enter logs.
func RedactSecret(secret string) string {
	s := strings.TrimSpace(secret)
	if s == "" {
		return ""
	}
	if len(s) <= 8 {
		return "****"
	}
	return s[:4] + "****" + s[len(s)-4:]
}
