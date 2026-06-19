package jobs

import (
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"errors"
	"fmt"
	"time"
)

const advisoryLockName = "pdttweb_render_worker"

type Store struct {
	db            *sql.DB
	leaseDuration time.Duration
	workerID      string
}

func NewStore(db *sql.DB, leaseDuration time.Duration) (*Store, error) {
	if db == nil {
		return nil, errors.New("db is required")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := db.PingContext(ctx); err != nil {
		return nil, fmt.Errorf("mysql ping: %w", err)
	}
	if err := Migrate(ctx, db); err != nil {
		return nil, err
	}
	return &Store{
		db:            db,
		leaseDuration: leaseDuration,
		workerID:      newWorkerID(),
	}, nil
}

func newWorkerID() string {
	var b [8]byte
	_, _ = rand.Read(b[:])
	return hex.EncodeToString(b[:])
}

func (s *Store) WorkerID() string {
	return s.workerID
}

func (s *Store) Create(ctx context.Context, scene string) (*Job, error) {
	id, err := newJobID()
	if err != nil {
		return nil, err
	}
	now := time.Now().UTC()
	_, err = s.db.ExecContext(ctx, `
		INSERT INTO render_jobs (id, scene, status, stage, created_at)
		VALUES (?, ?, ?, ?, ?)`,
		id, scene, StatusQueued, StageQueued, now,
	)
	if err != nil {
		return nil, fmt.Errorf("insert job: %w", err)
	}
	return s.GetByID(ctx, id)
}

func (s *Store) CreateRunning(ctx context.Context, scene string) (*Job, error) {
	id, err := newJobID()
	if err != nil {
		return nil, err
	}
	now := time.Now().UTC()
	_, err = s.db.ExecContext(ctx, `
		INSERT INTO render_jobs (id, scene, status, stage, created_at, started_at, lease_owner)
		VALUES (?, ?, ?, ?, ?, ?, ?)`,
		id, scene, StatusRunning, StageRenderingFrames, now, now, s.workerID,
	)
	if err != nil {
		return nil, fmt.Errorf("insert running render log: %w", err)
	}
	return s.GetByID(ctx, id)
}

func (s *Store) CreateFailed(ctx context.Context, scene, message string) error {
	id, err := newJobID()
	if err != nil {
		return err
	}
	now := time.Now().UTC()
	_, err = s.db.ExecContext(ctx, `
		INSERT INTO render_jobs (id, scene, status, stage, error_message, created_at, completed_at)
		VALUES (?, ?, ?, ?, ?, ?, ?)`,
		id, scene, StatusFailed, StageFailed, message, now, now,
	)
	if err != nil {
		return fmt.Errorf("insert failed render log: %w", err)
	}
	return nil
}

func newJobID() (string, error) {
	var b [6]byte
	if _, err := rand.Read(b[:]); err != nil {
		return "", err
	}
	return hex.EncodeToString(b[:]), nil
}

func (s *Store) GetByID(ctx context.Context, id string) (*Job, error) {
	row := s.db.QueryRowContext(ctx, `
		SELECT id, scene, status, stage, COALESCE(error_message, ''),
		       COALESCE(video_path, ''), COALESCE(video_size, 0),
		       created_at, started_at, completed_at,
		       COALESCE(lease_owner, ''), lease_expires_at
		FROM render_jobs WHERE id = ?`, id)
	return scanJob(row)
}

func (s *Store) StatusView(ctx context.Context, id string) (*StatusView, error) {
	job, err := s.GetByID(ctx, id)
	if err != nil {
		return nil, err
	}
	pos, err := s.queuePosition(ctx, job)
	if err != nil {
		return nil, err
	}
	return job.toStatusView(pos), nil
}

func (s *Store) queuePosition(ctx context.Context, job *Job) (int, error) {
	if job.Status != StatusQueued {
		return 0, nil
	}
	var pos int
	err := s.db.QueryRowContext(ctx, `
		SELECT COUNT(*) + 1
		FROM render_jobs
		WHERE status = ? AND (created_at < ? OR (created_at = ? AND id < ?))`,
		StatusQueued, job.CreatedAt, job.CreatedAt, job.ID,
	).Scan(&pos)
	if err != nil {
		return 0, err
	}
	return pos, nil
}

func (s *Store) RecoverStale(ctx context.Context) (int64, error) {
	res, err := s.db.ExecContext(ctx, `
		UPDATE render_jobs
		SET status = ?, stage = ?, error_message = ?, completed_at = ?
		WHERE status = ? AND lease_expires_at IS NOT NULL AND lease_expires_at < ?`,
		StatusFailed, StageFailed, "worker lease expired (stale running job recovered)",
		time.Now().UTC(), StatusRunning, time.Now().UTC(),
	)
	if err != nil {
		return 0, err
	}
	return res.RowsAffected()
}

func (s *Store) TryClaimNext(ctx context.Context) (*Job, error) {
	conn, err := s.db.Conn(ctx)
	if err != nil {
		return nil, err
	}
	defer conn.Close()

	var got int
	if err := conn.QueryRowContext(ctx, `SELECT GET_LOCK(?, 0)`, advisoryLockName).Scan(&got); err != nil {
		return nil, fmt.Errorf("get lock: %w", err)
	}
	if got != 1 {
		return nil, nil
	}
	defer func() {
		_, _ = conn.ExecContext(context.Background(), `SELECT RELEASE_LOCK(?)`, advisoryLockName)
	}()

	tx, err := conn.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()

	if _, err := tx.ExecContext(ctx, `
		UPDATE render_jobs
		SET status = ?, stage = ?, error_message = ?, completed_at = ?
		WHERE status = ? AND lease_expires_at IS NOT NULL AND lease_expires_at < ?`,
		StatusFailed, StageFailed, "worker lease expired (stale running job recovered)",
		time.Now().UTC(), StatusRunning, time.Now().UTC(),
	); err != nil {
		return nil, err
	}

	var active int
	if err := tx.QueryRowContext(ctx, `
		SELECT COUNT(*) FROM render_jobs WHERE status = ?`, StatusRunning,
	).Scan(&active); err != nil {
		return nil, err
	}
	if active > 0 {
		return nil, nil
	}

	var id, scene string
	var createdAt time.Time
	err = tx.QueryRowContext(ctx, `
		SELECT id, scene, created_at
		FROM render_jobs
		WHERE status = ?
		ORDER BY created_at, id
		LIMIT 1
		FOR UPDATE`,
		StatusQueued,
	).Scan(&id, &scene, &createdAt)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}

	now := time.Now().UTC()
	leaseUntil := now.Add(s.leaseDuration)
	if _, err := tx.ExecContext(ctx, `
		UPDATE render_jobs
		SET status = ?, stage = ?, started_at = ?, lease_owner = ?, lease_expires_at = ?
		WHERE id = ?`,
		StatusRunning, StageRenderingFrames, now, s.workerID, leaseUntil, id,
	); err != nil {
		return nil, err
	}
	if err := tx.Commit(); err != nil {
		return nil, err
	}
	return s.GetByID(ctx, id)
}

func (s *Store) RenewLease(ctx context.Context, id string) error {
	until := time.Now().UTC().Add(s.leaseDuration)
	res, err := s.db.ExecContext(ctx, `
		UPDATE render_jobs
		SET lease_expires_at = ?
		WHERE id = ? AND status = ? AND lease_owner = ?`,
		until, id, StatusRunning, s.workerID,
	)
	if err != nil {
		return err
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return fmt.Errorf("lease renew rejected for job %s", id)
	}
	return nil
}

func (s *Store) SetStage(ctx context.Context, id, stage string) error {
	res, err := s.db.ExecContext(ctx, `
		UPDATE render_jobs SET stage = ?
		WHERE id = ? AND status = ? AND lease_owner = ?`,
		stage, id, StatusRunning, s.workerID,
	)
	if err != nil {
		return err
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return fmt.Errorf("set stage rejected for job %s", id)
	}
	return nil
}

func (s *Store) MarkCompleted(ctx context.Context, id, videoPath string, videoSize int64) error {
	now := time.Now().UTC()
	res, err := s.db.ExecContext(ctx, `
		UPDATE render_jobs
		SET status = ?, stage = ?, video_path = ?, video_size = ?,
		    completed_at = ?, lease_owner = NULL, lease_expires_at = NULL, error_message = NULL
		WHERE id = ? AND status = ? AND lease_owner = ?`,
		StatusCompleted, StageCompleted, videoPath, videoSize, now, id, StatusRunning, s.workerID,
	)
	if err != nil {
		return err
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return fmt.Errorf("mark completed rejected for job %s", id)
	}
	return nil
}

func (s *Store) MarkFailed(ctx context.Context, id, message string) error {
	now := time.Now().UTC()
	res, err := s.db.ExecContext(ctx, `
		UPDATE render_jobs
		SET status = ?, stage = ?, error_message = ?,
		    completed_at = ?, lease_owner = NULL, lease_expires_at = NULL
		WHERE id = ? AND (status = ? OR (status = ? AND lease_owner = ?))`,
		StatusFailed, StageFailed, message, now, id, StatusQueued, StatusRunning, s.workerID,
	)
	if err != nil {
		return err
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return fmt.Errorf("mark failed rejected for job %s", id)
	}
	return nil
}

func (s *Store) ListRecent(ctx context.Context, limit int) ([]*Job, error) {
	if limit <= 0 {
		limit = 100
	}
	rows, err := s.db.QueryContext(ctx, `
		SELECT id, scene, status, stage, COALESCE(error_message, ''),
		       COALESCE(video_path, ''), COALESCE(video_size, 0),
		       created_at, started_at, completed_at,
		       COALESCE(lease_owner, ''), lease_expires_at
		FROM render_jobs
		ORDER BY created_at DESC
		LIMIT ?`, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []*Job
	for rows.Next() {
		job, err := scanJobRows(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, job)
	}
	return out, rows.Err()
}

func (s *Store) QueueStats(ctx context.Context) (queued, running int, err error) {
	err = s.db.QueryRowContext(ctx, `
		SELECT
			COALESCE(SUM(status = ?), 0),
			COALESCE(SUM(status = ?), 0)
		FROM render_jobs`,
		StatusQueued, StatusRunning,
	).Scan(&queued, &running)
	return
}

type ExpiredJob struct {
	ID        string
	VideoPath string
}

func (s *Store) ListExpired(ctx context.Context, before time.Time, limit int) ([]ExpiredJob, error) {
	if limit <= 0 {
		limit = 200
	}
	rows, err := s.db.QueryContext(ctx, `
		SELECT id, COALESCE(video_path, '')
		FROM render_jobs
		WHERE completed_at IS NOT NULL AND completed_at < ?
		ORDER BY completed_at
		LIMIT ?`, before, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []ExpiredJob
	for rows.Next() {
		var e ExpiredJob
		if err := rows.Scan(&e.ID, &e.VideoPath); err != nil {
			return nil, err
		}
		out = append(out, e)
	}
	return out, rows.Err()
}

func (s *Store) DeleteByID(ctx context.Context, id string) error {
	_, err := s.db.ExecContext(ctx, `DELETE FROM render_jobs WHERE id = ?`, id)
	return err
}

func scanJob(row *sql.Row) (*Job, error) {
	var j Job
	var started, completed, lease sql.NullTime
	if err := row.Scan(
		&j.ID, &j.Scene, &j.Status, &j.Stage, &j.ErrorMessage,
		&j.VideoPath, &j.VideoSize,
		&j.CreatedAt, &started, &completed,
		&j.LeaseOwner, &lease,
	); err != nil {
		return nil, err
	}
	if started.Valid {
		t := started.Time
		j.StartedAt = &t
	}
	if completed.Valid {
		t := completed.Time
		j.CompletedAt = &t
	}
	if lease.Valid {
		t := lease.Time
		j.LeaseExpires = &t
	}
	return &j, nil
}

func scanJobRows(rows *sql.Rows) (*Job, error) {
	var j Job
	var started, completed, lease sql.NullTime
	if err := rows.Scan(
		&j.ID, &j.Scene, &j.Status, &j.Stage, &j.ErrorMessage,
		&j.VideoPath, &j.VideoSize,
		&j.CreatedAt, &started, &completed,
		&j.LeaseOwner, &lease,
	); err != nil {
		return nil, err
	}
	if started.Valid {
		t := started.Time
		j.StartedAt = &t
	}
	if completed.Valid {
		t := completed.Time
		j.CompletedAt = &t
	}
	if lease.Valid {
		t := lease.Time
		j.LeaseExpires = &t
	}
	return &j, nil
}

func (j *Job) toStatusView(queuePos int) *StatusView {
	v := &StatusView{
		ID:          j.ID,
		Status:      j.Status,
		Stage:       j.Stage,
		QueuePos:    queuePos,
		Error:       j.ErrorMessage,
		VideoSize:   j.VideoSize,
		CreatedAt:   j.CreatedAt,
		StartedAt:   j.StartedAt,
		CompletedAt: j.CompletedAt,
	}
	if j.Status == StatusCompleted && j.VideoPath != "" {
		v.VideoURL = "/video/" + j.ID
	}
	v.ElapsedSec = elapsedSeconds(j)
	return v
}

func elapsedSeconds(j *Job) float64 {
	end := time.Now().UTC()
	if j.CompletedAt != nil {
		end = j.CompletedAt.UTC()
	}
	start := j.CreatedAt.UTC()
	if j.StartedAt != nil {
		start = j.StartedAt.UTC()
	}
	if j.Status == StatusQueued {
		start = j.CreatedAt.UTC()
	}
	if end.Before(start) {
		return 0
	}
	return end.Sub(start).Seconds()
}

func (j *Job) AdminQueuePos(ctx context.Context, store *Store) (int, error) {
	return store.queuePosition(ctx, j)
}

func (j *Job) ElapsedSeconds() float64 {
	return elapsedSeconds(j)
}

func (j *Job) DurationSeconds() *float64 {
	if j.StartedAt == nil || j.CompletedAt == nil {
		return nil
	}
	sec := j.CompletedAt.Sub(*j.StartedAt).Seconds()
	return &sec
}
