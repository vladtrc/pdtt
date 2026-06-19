package jobs

import (
	"context"
	"regexp"
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"
)

func TestMigrateIdempotent(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	mock.ExpectExec("CREATE TABLE IF NOT EXISTS render_jobs").
		WillReturnResult(sqlmock.NewResult(0, 0))
	mock.ExpectExec("CREATE TABLE IF NOT EXISTS generation_logs").
		WillReturnResult(sqlmock.NewResult(0, 0))
	mock.ExpectExec("CREATE TABLE IF NOT EXISTS generation_attempts").
		WillReturnResult(sqlmock.NewResult(0, 0))
	mock.ExpectQuery("SELECT COUNT\\(\\*\\) FROM information_schema.COLUMNS").
		WillReturnRows(sqlmock.NewRows([]string{"count"}).AddRow(0))

	if err := Migrate(context.Background(), db); err != nil {
		t.Fatalf("Migrate: %v", err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

func TestStoreCreateAndStatusView(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	now := time.Now().UTC()
	mock.ExpectExec("CREATE TABLE IF NOT EXISTS render_jobs").
		WillReturnResult(sqlmock.NewResult(0, 0))
	mock.ExpectExec("CREATE TABLE IF NOT EXISTS generation_logs").
		WillReturnResult(sqlmock.NewResult(0, 0))
	mock.ExpectExec("CREATE TABLE IF NOT EXISTS generation_attempts").
		WillReturnResult(sqlmock.NewResult(0, 0))
	mock.ExpectQuery("SELECT COUNT\\(\\*\\) FROM information_schema.COLUMNS").
		WillReturnRows(sqlmock.NewRows([]string{"count"}).AddRow(0))
	mock.ExpectPing()

	store, err := NewStore(db, time.Minute)
	if err != nil {
		t.Fatalf("NewStore: %v", err)
	}

	mock.ExpectExec("INSERT INTO render_jobs").
		WithArgs(sqlmock.AnyArg(), "scene text", StatusQueued, StageQueued, sqlmock.AnyArg()).
		WillReturnResult(sqlmock.NewResult(1, 1))

	id := "abc123"
	mock.ExpectQuery("SELECT id, scene, status").
		WithArgs(sqlmock.AnyArg()).
		WillReturnRows(sqlmock.NewRows([]string{
			"id", "scene", "status", "stage", "error_message",
			"video_path", "video_size", "created_at", "started_at", "completed_at",
			"lease_owner", "lease_expires_at",
		}).AddRow(id, "scene text", StatusQueued, StageQueued, "", "", 0, now, nil, nil, "", nil))

	job, err := store.Create(context.Background(), "scene text")
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	if job.ID == "" {
		t.Fatal("expected generated id")
	}

	mock.ExpectQuery("SELECT id, scene, status").
		WithArgs(job.ID).
		WillReturnRows(sqlmock.NewRows([]string{
			"id", "scene", "status", "stage", "error_message",
			"video_path", "video_size", "created_at", "started_at", "completed_at",
			"lease_owner", "lease_expires_at",
		}).AddRow(job.ID, "scene text", StatusQueued, StageQueued, "", "", 0, now, nil, nil, "", nil))
	mock.ExpectQuery("FROM render_jobs WHERE status = \\? AND \\(created_at < \\? OR").
		WithArgs(StatusQueued, now, now, sqlmock.AnyArg()).
		WillReturnRows(sqlmock.NewRows([]string{"pos"}).AddRow(1))

	view, err := store.StatusView(context.Background(), job.ID)
	if err != nil {
		t.Fatalf("StatusView: %v", err)
	}
	if view.Status != StatusQueued || view.Stage != StageQueued {
		t.Fatalf("unexpected view: %+v", view)
	}
	if view.QueuePos != 1 {
		t.Fatalf("queue pos = %d", view.QueuePos)
	}
}

func TestRecoverStaleMarksExpiredRunningJobs(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	mock.ExpectExec("UPDATE render_jobs").
		WithArgs(StatusFailed, StageFailed, sqlmock.AnyArg(), sqlmock.AnyArg(), StatusRunning, sqlmock.AnyArg()).
		WillReturnResult(sqlmock.NewResult(0, 2))

	store := &Store{db: db, leaseDuration: time.Minute, workerID: "worker"}
	n, err := store.RecoverStale(context.Background())
	if err != nil {
		t.Fatalf("RecoverStale: %v", err)
	}
	if n != 2 {
		t.Fatalf("rows = %d", n)
	}
}

func TestMarkFailedAndCompleted(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	store := &Store{db: db, leaseDuration: time.Minute, workerID: "worker1"}

	mock.ExpectExec(regexp.QuoteMeta(`UPDATE render_jobs
		SET status = ?, stage = ?, error_message = ?,
		    completed_at = ?, lease_owner = NULL, lease_expires_at = NULL
		WHERE id = ? AND (status = ? OR (status = ? AND lease_owner = ?))`)).
		WithArgs(StatusFailed, StageFailed, "boom", sqlmock.AnyArg(), "job1", StatusQueued, StatusRunning, "worker1").
		WillReturnResult(sqlmock.NewResult(0, 1))
	if err := store.MarkFailed(context.Background(), "job1", "boom"); err != nil {
		t.Fatal(err)
	}

	mock.ExpectExec(regexp.QuoteMeta(`UPDATE render_jobs
		SET status = ?, stage = ?, video_path = ?, video_size = ?,
		    completed_at = ?, lease_owner = NULL, lease_expires_at = NULL, error_message = NULL
		WHERE id = ? AND status = ? AND lease_owner = ?`)).
		WithArgs(StatusCompleted, StageCompleted, "/data/videos/job1.mp4", int64(123), sqlmock.AnyArg(), "job1", StatusRunning, "worker1").
		WillReturnResult(sqlmock.NewResult(0, 1))
	if err := store.MarkCompleted(context.Background(), "job1", "/data/videos/job1.mp4", 123); err != nil {
		t.Fatal(err)
	}
}

func TestGenerationLogging(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	store := &Store{db: db, leaseDuration: time.Minute, workerID: "worker1"}

	mock.ExpectExec("INSERT INTO generation_logs").
		WithArgs(sqlmock.AnyArg(), "draw parabola", "test-model", GenerationStatusRunning, sqlmock.AnyArg()).
		WillReturnResult(sqlmock.NewResult(1, 1))
	id, err := store.CreateGeneration(context.Background(), "draw parabola", "test-model")
	if err != nil {
		t.Fatal(err)
	}
	if id == "" {
		t.Fatal("expected generation id")
	}

	mock.ExpectExec("INSERT INTO generation_attempts").
		WithArgs(id, 1, `[{"role":"user","content":"draw"}]`, "raw response", "scene bad", "line 1: invalid scene", "", sqlmock.AnyArg()).
		WillReturnResult(sqlmock.NewResult(1, 1))
	if err := store.RecordGenerationAttempt(context.Background(), id, 1, `[{"role":"user","content":"draw"}]`, "raw response", "scene bad", "line 1: invalid scene", ""); err != nil {
		t.Fatal(err)
	}

	mock.ExpectExec("UPDATE generation_logs").
		WithArgs(GenerationStatusFailed, "generated scene did not validate", sqlmock.AnyArg(), id).
		WillReturnResult(sqlmock.NewResult(0, 1))
	if err := store.MarkGenerationFailed(context.Background(), id, "generated scene did not validate"); err != nil {
		t.Fatal(err)
	}

	mock.ExpectExec("UPDATE generation_logs").
		WithArgs(GenerationStatusCompleted, "scene ok", sqlmock.AnyArg(), id).
		WillReturnResult(sqlmock.NewResult(0, 1))
	if err := store.MarkGenerationCompleted(context.Background(), id, "scene ok"); err != nil {
		t.Fatal(err)
	}
}

func TestElapsedSecondsUsesStartedAtForRunning(t *testing.T) {
	started := time.Now().UTC().Add(-10 * time.Second)
	job := &Job{
		Status:    StatusRunning,
		Stage:     StageRenderingFrames,
		CreatedAt: started.Add(-5 * time.Second),
		StartedAt: &started,
	}
	sec := job.ElapsedSeconds()
	if sec < 9 || sec > 12 {
		t.Fatalf("elapsed = %f", sec)
	}
}
