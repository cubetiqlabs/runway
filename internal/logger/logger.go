package logger

import (
	"strings"

	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
)

// New constructs a configured Uber Zap logger for production (JSON) or development (console).
func New(env, level, format string) (*zap.Logger, error) {
	lvl := zapcore.InfoLevel
	if err := lvl.UnmarshalText([]byte(strings.ToLower(strings.TrimSpace(level)))); err != nil {
		lvl = zapcore.InfoLevel
	}

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

	return cfg.Build()
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
