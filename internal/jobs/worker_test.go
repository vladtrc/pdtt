package jobs

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/vladtrc/pdtt/pkg/render"
	"github.com/vladtrc/pdtt/internal/config"
)

type fakeEncoder struct {
	size int64
	err  error
}

func (f fakeEncoder) EncodeFramesToFile(string, float64, int, string) (int64, error) {
	return f.size, f.err
}

func TestWorkerProcessesClaimedJob(t *testing.T) {
	store := &recordingStore{
		job: &Job{ID: "job1", Scene: "scene", Status: StatusRunning, Stage: StageRenderingFrames},
	}
	cfg := &config.Config{
		DataDir:       t.TempDir(),
		RenderTimeout: time.Minute,
		LeaseDuration: time.Minute,
	}
	worker := NewWorker(store, cfg, fakeEncoder{size: 42})
	worker.onRender = func(ctx context.Context, scene, workDir string) (*render.Result, error) {
		return &render.Result{FramesDir: workDir}, nil
	}
	worker.logf = func(string, ...any) {}

	worker.processJob(context.Background(), store.job)

	if !store.completed {
		t.Fatal("expected completed")
	}
	if store.failed {
		t.Fatal("unexpected failed")
	}
	if store.videoPath == "" {
		t.Fatal("expected video path")
	}
}

func TestWorkerMarksFailedOnEncodeError(t *testing.T) {
	store := &recordingStore{
		job: &Job{ID: "job2", Scene: "scene", Status: StatusRunning, Stage: StageRenderingFrames},
	}
	cfg := &config.Config{
		DataDir:       t.TempDir(),
		RenderTimeout: time.Minute,
		LeaseDuration: time.Minute,
	}
	worker := NewWorker(store, cfg, fakeEncoder{err: errors.New("encode failed")})
	worker.onRender = func(ctx context.Context, scene, workDir string) (*render.Result, error) {
		return &render.Result{FramesDir: workDir}, nil
	}
	worker.logf = func(string, ...any) {}

	worker.processJob(context.Background(), store.job)
	if !store.failed {
		t.Fatal("expected failed")
	}
}

func TestWorkerRenderTimeoutWaitsForGoroutine(t *testing.T) {
	store := &recordingStore{
		job: &Job{ID: "job3", Scene: "scene", Status: StatusRunning, Stage: StageRenderingFrames},
	}
	cfg := &config.Config{
		DataDir:       t.TempDir(),
		RenderTimeout: 20 * time.Millisecond,
		LeaseDuration: time.Minute,
	}
	worker := NewWorker(store, cfg, fakeEncoder{size: 1})
	started := make(chan struct{})
	done := make(chan struct{})
	worker.onRender = func(ctx context.Context, scene, workDir string) (*render.Result, error) {
		ch := make(chan struct{})
		go func() {
			close(started)
			time.Sleep(80 * time.Millisecond)
			close(done)
			ch <- struct{}{}
		}()
		select {
		case <-ctx.Done():
			<-ch
			return nil, fmt.Errorf(
				"render timeout after %s (render.Scene is not cancellable; waited for goroutine to finish)",
				cfg.RenderTimeout,
			)
		case <-ch:
			if ctx.Err() != nil {
				return nil, fmt.Errorf(
					"render timeout after %s (render.Scene is not cancellable; waited for goroutine to finish)",
					cfg.RenderTimeout,
				)
			}
			return &render.Result{FramesDir: workDir}, nil
		}
	}
	worker.logf = func(string, ...any) {}

	doneProcess := make(chan struct{})
	go func() {
		worker.processJob(context.Background(), store.job)
		close(doneProcess)
	}()
	<-started
	<-done
	<-doneProcess
	if !store.failed {
		t.Fatal("expected timeout failure")
	}
}

func TestWorkerRenewLeaseContinuesAfterRenderTimeout(t *testing.T) {
	store := &renewalTrackingStore{
		recordingStore: recordingStore{
			job: &Job{ID: "job4", Scene: "scene", Status: StatusRunning, Stage: StageRenderingFrames},
		},
	}
	cfg := &config.Config{
		DataDir:       t.TempDir(),
		RenderTimeout: 25 * time.Millisecond,
		LeaseDuration: time.Minute,
	}
	worker := NewWorker(store, cfg, fakeEncoder{size: 1})
	worker.leaseRenewEvery = 12 * time.Millisecond
	worker.onRender = func(ctx context.Context, scene, workDir string) (*render.Result, error) {
		ch := make(chan struct{})
		go func() {
			<-ctx.Done()
			store.markRenderTimedOut()
			time.Sleep(80 * time.Millisecond)
			ch <- struct{}{}
		}()
		select {
		case <-ctx.Done():
			<-ch
			return nil, fmt.Errorf(
				"render timeout after %s (render.Scene is not cancellable; waited for goroutine to finish)",
				cfg.RenderTimeout,
			)
		case <-ch:
			if ctx.Err() != nil {
				return nil, fmt.Errorf(
					"render timeout after %s (render.Scene is not cancellable; waited for goroutine to finish)",
					cfg.RenderTimeout,
				)
			}
			return &render.Result{FramesDir: workDir}, nil
		}
	}
	worker.logf = func(string, ...any) {}

	doneProcess := make(chan struct{})
	go func() {
		worker.processJob(context.Background(), store.job)
		close(doneProcess)
	}()

	deadline := time.Now().Add(200 * time.Millisecond)
	for time.Now().Before(deadline) {
		if store.renewalsAfterRenderTimeout() >= 1 {
			break
		}
		time.Sleep(5 * time.Millisecond)
	}
	if store.renewalsAfterRenderTimeout() < 1 {
		t.Fatal("expected lease renewal after render timeout while render still running")
	}
	<-doneProcess
	if !store.failed {
		t.Fatal("expected timeout failure after render finished")
	}
}

func TestWorkerLeaseRenewContinuesDuringShutdown(t *testing.T) {
	store := &renewalTrackingStore{
		recordingStore: recordingStore{
			job: &Job{ID: "job5", Scene: "scene", Status: StatusRunning, Stage: StageRenderingFrames},
		},
	}
	cfg := &config.Config{
		DataDir:       t.TempDir(),
		RenderTimeout: time.Minute,
		LeaseDuration: time.Minute,
	}
	worker := NewWorker(store, cfg, fakeEncoder{size: 1})
	worker.leaseRenewEvery = 12 * time.Millisecond

	runCtx, cancelRun := context.WithCancel(context.Background())
	renderStarted := make(chan struct{})
	renderRelease := make(chan struct{})
	worker.onRender = func(ctx context.Context, scene, workDir string) (*render.Result, error) {
		close(renderStarted)
		<-renderRelease
		return &render.Result{FramesDir: workDir}, nil
	}
	worker.logf = func(string, ...any) {}

	doneProcess := make(chan struct{})
	go func() {
		worker.processJob(runCtx, store.job)
		close(doneProcess)
	}()

	<-renderStarted
	cancelRun()
	store.markRenderTimedOut()

	deadline := time.Now().Add(200 * time.Millisecond)
	for time.Now().Before(deadline) {
		if store.renewalsAfterRenderTimeout() >= 1 {
			break
		}
		time.Sleep(5 * time.Millisecond)
	}
	if store.renewalsAfterRenderTimeout() < 1 {
		t.Fatal("expected lease renewal after shutdown cancel while render still running")
	}

	close(renderRelease)
	<-doneProcess
	if !store.completed {
		t.Fatal("expected job to complete after render finished")
	}
}

type renewalTrackingStore struct {
	recordingStore
	renewMu        sync.Mutex
	renewAfter     []time.Time
	renderTimedOut time.Time
}

func (r *renewalTrackingStore) RenewLease(context.Context, string) error {
	r.renewMu.Lock()
	r.renewAfter = append(r.renewAfter, time.Now())
	r.renewMu.Unlock()
	return nil
}

func (r *renewalTrackingStore) markRenderTimedOut() {
	r.renewMu.Lock()
	r.renderTimedOut = time.Now()
	r.renewMu.Unlock()
}

func (r *renewalTrackingStore) renewalsAfterRenderTimeout() int {
	r.renewMu.Lock()
	defer r.renewMu.Unlock()
	if r.renderTimedOut.IsZero() {
		return 0
	}
	n := 0
	for _, ts := range r.renewAfter {
		if ts.After(r.renderTimedOut) {
			n++
		}
	}
	return n
}

type recordingStore struct {
	job       *Job
	completed bool
	failed    bool
	videoPath string
}

func (r *recordingStore) TryClaimNext(context.Context) (*Job, error)  { return nil, nil }
func (r *recordingStore) RecoverStale(context.Context) (int64, error) { return 0, nil }
func (r *recordingStore) RenewLease(context.Context, string) error    { return nil }
func (r *recordingStore) SetStage(context.Context, string, string) error {
	return nil
}
func (r *recordingStore) MarkCompleted(_ context.Context, id, videoPath string, _ int64) error {
	r.completed = true
	r.videoPath = videoPath
	return nil
}
func (r *recordingStore) MarkFailed(_ context.Context, id, _ string) error {
	r.failed = true
	return nil
}

var _ workerStore = (*recordingStore)(nil)
