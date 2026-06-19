package jobs

import (
	"context"
	"database/sql"
	"fmt"
	"time"
)

const (
	GenerationStatusRunning   = "running"
	GenerationStatusCompleted = "completed"
	GenerationStatusFailed    = "failed"
)

type GenerationLog struct {
	ID                  string
	Prompt              string
	Model               string
	Status              string
	Scene               string
	ErrorMessage        string
	AttemptCount        int
	LastValidationError string
	LastOpenRouterError string
	CreatedAt           time.Time
	CompletedAt         *time.Time
}

type GenerationAttempt struct {
	ID              int64
	GenerationID    string
	Attempt         int
	RequestMessages string
	ResponseContent string
	ExtractedScene  string
	ValidationError string
	OpenRouterError string
	CreatedAt       time.Time
}

func (s *Store) CreateGeneration(ctx context.Context, prompt, model string) (string, error) {
	id, err := newJobID()
	if err != nil {
		return "", err
	}
	now := time.Now().UTC()
	_, err = s.db.ExecContext(ctx, `
		INSERT INTO generation_logs (id, prompt, model, status, created_at)
		VALUES (?, ?, ?, ?, ?)`,
		id, prompt, model, GenerationStatusRunning, now,
	)
	if err != nil {
		return "", fmt.Errorf("insert generation log: %w", err)
	}
	return id, nil
}

func (s *Store) RecordGenerationAttempt(ctx context.Context, generationID string, attempt int, requestMessages, responseContent, extractedScene, validationError, openRouterError string) error {
	_, err := s.db.ExecContext(ctx, `
		INSERT INTO generation_attempts (
			generation_id, attempt, request_messages, response_content,
			extracted_scene, validation_error, openrouter_error, created_at
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?)`,
		generationID, attempt, requestMessages, responseContent, extractedScene, validationError, openRouterError, time.Now().UTC(),
	)
	if err != nil {
		return fmt.Errorf("insert generation attempt: %w", err)
	}
	return nil
}

func (s *Store) MarkGenerationCompleted(ctx context.Context, id, scene string) error {
	_, err := s.db.ExecContext(ctx, `
		UPDATE generation_logs
		SET status = ?, scene = ?, error_message = NULL, completed_at = ?
		WHERE id = ?`,
		GenerationStatusCompleted, scene, time.Now().UTC(), id,
	)
	if err != nil {
		return fmt.Errorf("mark generation completed: %w", err)
	}
	return nil
}

func (s *Store) MarkGenerationFailed(ctx context.Context, id, message string) error {
	_, err := s.db.ExecContext(ctx, `
		UPDATE generation_logs
		SET status = ?, error_message = ?, completed_at = ?
		WHERE id = ?`,
		GenerationStatusFailed, message, time.Now().UTC(), id,
	)
	if err != nil {
		return fmt.Errorf("mark generation failed: %w", err)
	}
	return nil
}

func (s *Store) ListRecentGenerations(ctx context.Context, limit int) ([]*GenerationLog, error) {
	if limit <= 0 {
		limit = 100
	}
	rows, err := s.db.QueryContext(ctx, `
		SELECT id, prompt, model, status, COALESCE(scene, ''), COALESCE(error_message, ''),
		       created_at, completed_at,
		       (SELECT COUNT(*) FROM generation_attempts ga WHERE ga.generation_id = generation_logs.id) AS attempt_count,
		       COALESCE((
		           SELECT validation_error FROM generation_attempts ga
		           WHERE ga.generation_id = generation_logs.id AND validation_error IS NOT NULL AND validation_error <> ''
		           ORDER BY attempt DESC
		           LIMIT 1
		       ), '') AS last_validation_error,
		       COALESCE((
		           SELECT openrouter_error FROM generation_attempts ga
		           WHERE ga.generation_id = generation_logs.id AND openrouter_error IS NOT NULL AND openrouter_error <> ''
		           ORDER BY attempt DESC
		           LIMIT 1
		       ), '') AS last_openrouter_error
		FROM generation_logs
		ORDER BY created_at DESC
		LIMIT ?`, limit)
	return scanGenerationLogRows(rows, err)
}

func (s *Store) ListAllGenerations(ctx context.Context) ([]*GenerationLog, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT id, prompt, model, status, COALESCE(scene, ''), COALESCE(error_message, ''),
		       created_at, completed_at,
		       (SELECT COUNT(*) FROM generation_attempts ga WHERE ga.generation_id = generation_logs.id) AS attempt_count,
		       COALESCE((
		           SELECT validation_error FROM generation_attempts ga
		           WHERE ga.generation_id = generation_logs.id AND validation_error IS NOT NULL AND validation_error <> ''
		           ORDER BY attempt DESC
		           LIMIT 1
		       ), '') AS last_validation_error,
		       COALESCE((
		           SELECT openrouter_error FROM generation_attempts ga
		           WHERE ga.generation_id = generation_logs.id AND openrouter_error IS NOT NULL AND openrouter_error <> ''
		           ORDER BY attempt DESC
		           LIMIT 1
		       ), '') AS last_openrouter_error
		FROM generation_logs
		ORDER BY created_at DESC`)
	return scanGenerationLogRows(rows, err)
}

func scanGenerationLogRows(rows *sql.Rows, err error) ([]*GenerationLog, error) {
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()

	var out []*GenerationLog
	for rows.Next() {
		var g GenerationLog
		var completed sql.NullTime
		if err := rows.Scan(
			&g.ID, &g.Prompt, &g.Model, &g.Status, &g.Scene, &g.ErrorMessage,
			&g.CreatedAt, &completed, &g.AttemptCount, &g.LastValidationError, &g.LastOpenRouterError,
		); err != nil {
			return nil, err
		}
		if completed.Valid {
			t := completed.Time
			g.CompletedAt = &t
		}
		out = append(out, &g)
	}
	return out, rows.Err()
}

func (s *Store) ListRecentGenerationAttempts(ctx context.Context, limit int) ([]*GenerationAttempt, error) {
	if limit <= 0 {
		limit = 200
	}
	rows, err := s.db.QueryContext(ctx, `
		SELECT id, generation_id, attempt, request_messages,
		       COALESCE(response_content, ''), COALESCE(extracted_scene, ''),
		       COALESCE(validation_error, ''), COALESCE(openrouter_error, ''),
		       created_at
		FROM generation_attempts
		ORDER BY created_at DESC, id DESC
		LIMIT ?`, limit)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()

	var out []*GenerationAttempt
	for rows.Next() {
		var a GenerationAttempt
		if err := rows.Scan(
			&a.ID, &a.GenerationID, &a.Attempt, &a.RequestMessages,
			&a.ResponseContent, &a.ExtractedScene, &a.ValidationError, &a.OpenRouterError,
			&a.CreatedAt,
		); err != nil {
			return nil, err
		}
		out = append(out, &a)
	}
	return out, rows.Err()
}

func (s *Store) LastGenerationAttempt(ctx context.Context, generationID string) (*GenerationAttempt, error) {
	row := s.db.QueryRowContext(ctx, `
		SELECT id, generation_id, attempt, request_messages,
		       COALESCE(response_content, ''), COALESCE(extracted_scene, ''),
		       COALESCE(validation_error, ''), COALESCE(openrouter_error, ''),
		       created_at
		FROM generation_attempts
		WHERE generation_id = ?
		ORDER BY attempt DESC, id DESC
		LIMIT 1`, generationID)
	var a GenerationAttempt
	if err := row.Scan(
		&a.ID, &a.GenerationID, &a.Attempt, &a.RequestMessages,
		&a.ResponseContent, &a.ExtractedScene, &a.ValidationError, &a.OpenRouterError,
		&a.CreatedAt,
	); err != nil {
		return nil, err
	}
	return &a, nil
}

func (s *Store) DeleteExpiredGenerations(ctx context.Context, before time.Time, limit int) (int64, error) {
	if limit <= 0 {
		limit = 200
	}
	res, err := s.db.ExecContext(ctx, `
		DELETE FROM generation_logs
		WHERE completed_at IS NOT NULL AND completed_at < ?
		ORDER BY completed_at
		LIMIT ?`, before, limit)
	if err != nil {
		return 0, err
	}
	return res.RowsAffected()
}
