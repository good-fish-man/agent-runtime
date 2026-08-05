// Package database provides the gorm-backed PostgreSQL connection used by the
// memory module. Connection details are read from config (which itself is
// sourced from the YAML config file with env overrides).
package database

import (
	"fmt"
	"time"

	"github.com/good-fish-man/agent-runtime/internal/config"
	log "github.com/good-fish-man/logx"

	"gorm.io/driver/postgres"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

// defaultSlowQueryThreshold flags SQL slower than this in the unified logs.
const defaultSlowQueryThreshold = 200 * time.Millisecond

// New opens a gorm PostgreSQL connection and configures the pool. Only the
// postgres driver is supported (matching agent-frame's default).
func New(cfg config.DBConfig) (*gorm.DB, error) {
	if cfg.DBType != "" && cfg.DBType != "postgres" {
		return nil, fmt.Errorf("unsupported db_type %q: only postgres is supported", cfg.DBType)
	}

	db, err := gorm.Open(postgres.Open(cfg.DSN()), &gorm.Config{
		// Route SQL logs through the house log package so every query carries
		// the caller goroutine's trace_id.
		Logger: &log.GormLogger{
			LogLevel:                  logLevel(cfg.LogMode),
			SlowThreshold:             defaultSlowQueryThreshold,
			IgnoreRecordNotFoundError: true,
		},
	})
	if err != nil {
		return nil, fmt.Errorf("open postgres: %w", err)
	}

	sqlDB, err := db.DB()
	if err != nil {
		return nil, fmt.Errorf("get sql.DB: %w", err)
	}
	if cfg.MaxOpenConn > 0 {
		sqlDB.SetMaxOpenConns(cfg.MaxOpenConn)
	}
	if cfg.MaxIdleConn > 0 {
		sqlDB.SetMaxIdleConns(cfg.MaxIdleConn)
	}
	if cfg.ConnMaxLifetime > 0 {
		sqlDB.SetConnMaxLifetime(time.Duration(cfg.ConnMaxLifetime) * time.Second)
	}
	if err := sqlDB.Ping(); err != nil {
		return nil, fmt.Errorf("ping postgres: %w", err)
	}
	return db, nil
}

func logLevel(mode int) logger.LogLevel {
	switch {
	case mode <= 1:
		return logger.Silent
	case mode == 2:
		return logger.Error
	case mode == 3:
		return logger.Warn
	default:
		return logger.Info
	}
}
