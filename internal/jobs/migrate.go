package jobs

import (
	"context"
	"database/sql"
	"fmt"
)

var schemaSQL = []string{
	`CREATE TABLE IF NOT EXISTS render_jobs (
    id VARCHAR(16) NOT NULL PRIMARY KEY,
    scene MEDIUMTEXT NOT NULL,
    status VARCHAR(16) NOT NULL DEFAULT 'queued',
    stage VARCHAR(32) NOT NULL DEFAULT 'queued',
    error_message TEXT NULL,
    video_path VARCHAR(512) NULL,
    video_size BIGINT NULL,
    created_at DATETIME(6) NOT NULL,
    started_at DATETIME(6) NULL,
    completed_at DATETIME(6) NULL,
    lease_owner VARCHAR(64) NULL,
    lease_expires_at DATETIME(6) NULL,
    INDEX idx_render_jobs_status_created (status, created_at, id),
    INDEX idx_render_jobs_lease (status, lease_expires_at)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4`,

	`CREATE TABLE IF NOT EXISTS generation_logs (
    id VARCHAR(16) NOT NULL PRIMARY KEY,
    prompt MEDIUMTEXT NOT NULL,
    model VARCHAR(255) NOT NULL,
    status VARCHAR(16) NOT NULL,
    scene MEDIUMTEXT NULL,
    error_message TEXT NULL,
    created_at DATETIME(6) NOT NULL,
    completed_at DATETIME(6) NULL,
    INDEX idx_generation_logs_created (created_at),
    INDEX idx_generation_logs_completed (completed_at)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4`,

	`CREATE TABLE IF NOT EXISTS generation_attempts (
    id BIGINT NOT NULL AUTO_INCREMENT PRIMARY KEY,
    generation_id VARCHAR(16) NOT NULL,
    attempt INT NOT NULL,
    request_messages MEDIUMTEXT NOT NULL,
    response_content MEDIUMTEXT NULL,
    extracted_scene MEDIUMTEXT NULL,
    validation_error TEXT NULL,
    openrouter_error TEXT NULL,
    created_at DATETIME(6) NOT NULL,
    INDEX idx_generation_attempts_generation (generation_id, attempt),
    CONSTRAINT fk_generation_attempts_generation
        FOREIGN KEY (generation_id) REFERENCES generation_logs(id)
        ON DELETE CASCADE
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4`,
}

func Migrate(ctx context.Context, db *sql.DB) error {
	for _, stmt := range schemaSQL {
		if _, err := db.ExecContext(ctx, stmt); err != nil {
			return fmt.Errorf("apply schema: %w", err)
		}
	}
	return migrateLegacyGIFColumns(ctx, db)
}

func migrateLegacyGIFColumns(ctx context.Context, db *sql.DB) error {
	var gifCols int
	err := db.QueryRowContext(ctx, `
		SELECT COUNT(*) FROM information_schema.COLUMNS
		WHERE TABLE_SCHEMA = DATABASE()
		  AND TABLE_NAME = 'render_jobs'
		  AND COLUMN_NAME = 'gif_path'`).Scan(&gifCols)
	if err != nil {
		return fmt.Errorf("check legacy gif_path column: %w", err)
	}
	if gifCols == 0 {
		return nil
	}
	if _, err := db.ExecContext(ctx, `
		ALTER TABLE render_jobs
		CHANGE gif_path video_path VARCHAR(512) NULL`); err != nil {
		return fmt.Errorf("rename gif_path: %w", err)
	}
	if _, err := db.ExecContext(ctx, `
		ALTER TABLE render_jobs
		CHANGE gif_size video_size BIGINT NULL`); err != nil {
		return fmt.Errorf("rename gif_size: %w", err)
	}
	return nil
}
