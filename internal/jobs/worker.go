package jobs

import (
	"context"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"time"

	"github.com/vladtrc/pdtt/pkg/render"
	"github.com/vladtrc/pdtt/internal/config"
)

const (
	renderFPS  = 12.0
	renderSize = 640
)

type VideoFileEncoder interface {
	EncodeFramesToFile(framesDir string, fps float64, size int, destPath string) (int64, error)
}

type workerStore interface {
	TryClaimNext(ctx context.Context) (*Job, error)
	RecoverStale(ctx context.Context) (int64, error)
	RenewLease(ctx context.Context, id string) error
	SetStage(ctx context.Context, id, stage string) error
	MarkCompleted(ctx context.Context, id, videoPath string, videoSize int64) error
	MarkFailed(ctx context.Context, id, message string) error
}

type Worker struct {
	store    workerStore
	cfg      *config.Config
	encoder  VideoFileEncoder
	now      func() time.Time
	logf     func(string, ...any)
	onRender func(ctx context.Context, scene, workDir string) (*render.Result, error)

	// leaseRenewEvery overrides renewal interval (tests only); zero uses LeaseDuration/2.
	leaseRenewEvery time.Duration
}

func NewWorker(store workerStore, cfg *config.Config, encoder VideoFileEncoder) *Worker {
	return &Worker{
		store:   store,
		cfg:     cfg,
		encoder: encoder,
		now:     time.Now,
		logf:    log.Printf,
		onRender: func(ctx context.Context, scene, workDir string) (*render.Result, error) {
			// render.Scene does not accept context and cannot be interrupted safely.
			// Timeout is enforced by waiting on a side goroutine; the next job will not
			// start until the timed-out render goroutine finishes (global single-flight).
			type renderResult struct {
				res *render.Result
				err error
			}
			ch := make(chan renderResult, 1)
			go func() {
				res, err := render.Scene(scene, workDir, render.Config{
					FPS:    renderFPS,
					Width:  renderSize,
					Height: renderSize,
				})
				ch <- renderResult{res: res, err: err}
			}()
			select {
			case <-ctx.Done():
				// Wait for render goroutine to avoid overlapping renders.
				<-ch
				return nil, fmt.Errorf(
					"render timeout after %s (render.Scene is not cancellable; waited for goroutine to finish)",
					cfg.RenderTimeout,
				)
			case rr := <-ch:
				if ctx.Err() != nil {
					return nil, fmt.Errorf(
						"render timeout after %s (render.Scene is not cancellable; waited for goroutine to finish)",
						cfg.RenderTimeout,
					)
				}
				return rr.res, rr.err
			}
		},
	}
}

func (w *Worker) Run(ctx context.Context) {
	ticker := time.NewTicker(w.cfg.WorkerPoll)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			w.tick(ctx)
		}
	}
}

func (w *Worker) tick(parent context.Context) {
	if parent.Err() != nil {
		return
	}
	if _, err := w.store.RecoverStale(parent); err != nil {
		w.logf("recover stale jobs: %v", err)
	}

	job, err := w.store.TryClaimNext(parent)
	if err != nil {
		w.logf("claim job: %v", err)
		return
	}
	if job == nil {
		return
	}
	w.processJob(parent, job)
}

func (w *Worker) processJob(parent context.Context, job *Job) {
	// jobCtx survives worker shutdown cancel so lease renewal and job DB updates
	// continue while render.Scene finishes (global single-flight).
	jobCtx := context.WithoutCancel(parent)
	renderCtx, renderCancel := context.WithTimeout(parent, w.cfg.RenderTimeout)
	defer renderCancel()

	// Lease must outlive render timeout: render.Scene is not cancellable and may
	// keep running after renderCtx expires. Stopping renewal on timeout would let
	// RecoverStale claim the job while this worker still renders (breaks single-flight).
	leaseStop := make(chan struct{})
	go w.leaseRenewLoop(jobCtx, job.ID, leaseStop)
	defer close(leaseStop)

	workParent := filepath.Join(w.cfg.DataDir, "work")
	if err := os.MkdirAll(workParent, 0o755); err != nil {
		w.failJob(jobCtx, job.ID, "create work dir: "+err.Error())
		return
	}
	workDir, err := os.MkdirTemp(workParent, job.ID+"-*")
	if err != nil {
		w.failJob(jobCtx, job.ID, "create work dir: "+err.Error())
		return
	}
	defer os.RemoveAll(workDir)

	result, err := w.onRender(renderCtx, job.Scene, workDir)
	if err != nil {
		w.failJob(jobCtx, job.ID, err.Error())
		return
	}

	if err := w.store.SetStage(jobCtx, job.ID, StageEncodingVideo); err != nil {
		w.logf("set stage encoding: %v", err)
	}

	videoDir := filepath.Join(w.cfg.DataDir, "videos")
	if err := os.MkdirAll(videoDir, 0o755); err != nil {
		w.failJob(jobCtx, job.ID, "create video dir: "+err.Error())
		return
	}
	videoPath := filepath.Join(videoDir, job.ID+".mp4")

	size, err := w.encoder.EncodeFramesToFile(result.FramesDir, renderFPS, renderSize, videoPath)
	if err != nil {
		_ = os.Remove(videoPath)
		w.failJob(jobCtx, job.ID, "video encode: "+err.Error())
		return
	}

	if err := w.store.MarkCompleted(jobCtx, job.ID, videoPath, size); err != nil {
		w.logf("mark completed: %v", err)
		_ = os.Remove(videoPath)
	}
}

func (w *Worker) failJob(ctx context.Context, id, msg string) {
	if err := w.store.MarkFailed(ctx, id, msg); err != nil {
		w.logf("mark failed %s: %v", id, err)
	}
}

func (w *Worker) leaseRenewLoop(parent context.Context, jobID string, stop <-chan struct{}) {
	period := w.cfg.LeaseDuration / 2
	if w.leaseRenewEvery > 0 {
		period = w.leaseRenewEvery
	}
	if period < time.Millisecond {
		period = time.Millisecond
	}
	ticker := time.NewTicker(period)
	defer ticker.Stop()
	for {
		select {
		case <-stop:
			return
		case <-parent.Done():
			return
		case <-ticker.C:
			renewCtx, cancel := context.WithTimeout(parent, 10*time.Second)
			err := w.store.RenewLease(renewCtx, jobID)
			cancel()
			if err != nil {
				w.logf("renew lease %s: %v", jobID, err)
			}
		}
	}
}

type Cleaner struct {
	store *Store
	cfg   *config.Config
	logf  func(string, ...any)
}

func NewCleaner(store *Store, cfg *config.Config) *Cleaner {
	return &Cleaner{store: store, cfg: cfg, logf: log.Printf}
}

func (c *Cleaner) Run(ctx context.Context) {
	ticker := time.NewTicker(c.cfg.CleanupEvery)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			c.purge(ctx)
			c.purgeOrphanWorkDirs()
		}
	}
}

func (c *Cleaner) purge(ctx context.Context) {
	cutoff := c.now().Add(-c.cfg.Retention)
	expired, err := c.store.ListExpired(ctx, cutoff, 200)
	if err != nil {
		c.logf("list expired jobs: %v", err)
		return
	}
	for _, e := range expired {
		if e.VideoPath != "" {
			if err := os.Remove(e.VideoPath); err != nil && !os.IsNotExist(err) {
				c.logf("remove video %s: %v", e.VideoPath, err)
				continue
			}
		}
		if err := c.store.DeleteByID(ctx, e.ID); err != nil {
			c.logf("delete job %s: %v", e.ID, err)
		}
	}
	if _, err := c.store.DeleteExpiredGenerations(ctx, cutoff, 200); err != nil {
		c.logf("delete expired generation logs: %v", err)
	}
}

func (c *Cleaner) purgeOrphanWorkDirs() {
	workRoot := filepath.Join(c.cfg.DataDir, "work")
	entries, err := os.ReadDir(workRoot)
	if err != nil {
		if !os.IsNotExist(err) {
			c.logf("read work dir: %v", err)
		}
		return
	}
	cutoff := c.now().Add(-c.cfg.Retention)
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		info, err := e.Info()
		if err != nil {
			continue
		}
		if info.ModTime().Before(cutoff) {
			path := filepath.Join(workRoot, e.Name())
			if err := os.RemoveAll(path); err != nil {
				c.logf("remove orphan work dir %s: %v", path, err)
			}
		}
	}
}

func (c *Cleaner) now() time.Time {
	return time.Now().UTC()
}
