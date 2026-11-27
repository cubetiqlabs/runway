package database

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	"github.com/cubetiqlabs/runway/internal/config"
	"go.uber.org/zap"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
	gormlogger "gorm.io/gorm/logger"
)

type zapGormLogger struct {
	log           *zap.Logger
	level         gormlogger.LogLevel
	slowThreshold time.Duration
}

func newZapGormLogger(log *zap.Logger, isProd bool) gormlogger.Interface {
	lvl := gormlogger.Warn
	if !isProd {
		lvl = gormlogger.Warn
	}
	return &zapGormLogger{
		log:           log.Named("gorm"),
		level:         lvl,
		slowThreshold: 250 * time.Millisecond,
	}
}

func (l *zapGormLogger) LogMode(level gormlogger.LogLevel) gormlogger.Interface {
	cp := *l
	cp.level = level
	return &cp
}

func (l *zapGormLogger) Info(_ context.Context, msg string, args ...any) {
	if l.level >= gormlogger.Info {
		l.log.Info(fmt.Sprintf(msg, args...))
	}
}

func (l *zapGormLogger) Warn(_ context.Context, msg string, args ...any) {
	if l.level >= gormlogger.Warn {
		l.log.Warn(fmt.Sprintf(msg, args...))
	}
}

func (l *zapGormLogger) Error(_ context.Context, msg string, args ...any) {
	if l.level >= gormlogger.Error {
		l.log.Error(fmt.Sprintf(msg, args...))
	}
}

func (l *zapGormLogger) Trace(_ context.Context, begin time.Time, fc func() (string, int64), err error) {
	if l.level <= gormlogger.Silent {
		return
	}
	elapsed := time.Since(begin)
	sqlText, rows := fc()
	switch {
	case err != nil && !errors.Is(err, gorm.ErrRecordNotFound) && l.level >= gormlogger.Error:
		l.log.Error("sql query error", zap.Error(err), zap.Duration("elapsed", elapsed), zap.Int64("rows", rows), zap.String("sql", sqlText))
	case elapsed > l.slowThreshold && l.level >= gormlogger.Warn:
		l.log.Warn("slow sql query", zap.Duration("elapsed", elapsed), zap.Int64("rows", rows), zap.String("sql", sqlText))
	case l.level >= gormlogger.Info:
		l.log.Debug("sql query", zap.Duration("elapsed", elapsed), zap.Int64("rows", rows))
	}
}

// NewGormDB initializes a tuned GORM PostgreSQL 18+ connection pool.
func NewGormDB(ctx context.Context, cfg config.DatabaseConfig, log *zap.Logger, isProd bool) (*gorm.DB, error) {
	db, err := gorm.Open(postgres.Open(cfg.URL), &gorm.Config{
		Logger:                 newZapGormLogger(log, isProd),
		TranslateError:         true,
		PrepareStmt:            true,
		SkipDefaultTransaction: true,
	})
	if err != nil {
		return nil, fmt.Errorf("open gorm postgres: %w", err)
	}

	sqlDB, err := db.DB()
	if err != nil {
		return nil, fmt.Errorf("unwrap sql.DB: %w", err)
	}
	sqlDB.SetMaxOpenConns(int(cfg.MaxConns))
	sqlDB.SetMaxIdleConns(int(cfg.MinConns))
	sqlDB.SetConnMaxLifetime(cfg.MaxConnLifetime)
	sqlDB.SetConnMaxIdleTime(cfg.MaxConnIdleTime)

	pingCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	if err := sqlDB.PingContext(pingCtx); err != nil {
		_ = sqlDB.Close()
		return nil, fmt.Errorf("ping database: %w", err)
	}

	return db, nil
}

// Ping verifies database connectivity for /readyz probes.
func Ping(ctx context.Context, db *gorm.DB) error {
	sqlDB, err := db.DB()
	if err != nil {
		return err
	}
	return sqlDB.PingContext(ctx)
}

// Close shuts down the underlying database connection pool.
func Close(db *gorm.DB) error {
	if db == nil {
		return nil
	}
	sqlDB, err := db.DB()
	if err != nil {
		return err
	}
	return sqlDB.Close()
}

// WithTx executes fn inside a GORM database transaction.
func WithTx(ctx context.Context, db *gorm.DB, fn func(tx *gorm.DB) error) error {
	return db.WithContext(ctx).Transaction(fn)
}

func sqlConn(ctx context.Context, db *gorm.DB) (*sql.Conn, error) {
	sqlDB, err := db.DB()
	if err != nil {
		return nil, err
	}
	return sqlDB.Conn(ctx)
}
