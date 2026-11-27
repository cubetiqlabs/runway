package database

import (
	"context"
	"fmt"

	"go.uber.org/zap"
	"gorm.io/gorm"
)

const migrationAdvisoryLockID int64 = 7491028374619

// AutoMigrate runs GORM schema migration for all registered models under a PostgreSQL advisory lock.
func AutoMigrate(ctx context.Context, db *gorm.DB, log *zap.Logger, models ...any) error {
	conn, err := sqlConn(ctx, db)
	if err != nil {
		return fmt.Errorf("acquire connection for auto-migration: %w", err)
	}
	defer conn.Close()

	if _, err := conn.ExecContext(ctx, "SELECT pg_advisory_lock($1)", migrationAdvisoryLockID); err != nil {
		return fmt.Errorf("acquire migration advisory lock: %w", err)
	}
	defer func() {
		_, _ = conn.ExecContext(context.Background(), "SELECT pg_advisory_unlock($1)", migrationAdvisoryLockID)
	}()

	if err := db.WithContext(ctx).AutoMigrate(models...); err != nil {
		return fmt.Errorf("gorm automigrate: %w", err)
	}

	// Ensure PostgreSQL GIN index on runner_pools.labels for fast array matching.
	if err := db.WithContext(ctx).Exec(`CREATE INDEX IF NOT EXISTS idx_runner_pools_labels ON runner_pools USING GIN(labels)`).Error; err != nil {
		return fmt.Errorf("ensure GIN index: %w", err)
	}

	log.Info("database auto-migration completed", zap.Int("models", len(models)))
	return nil
}
