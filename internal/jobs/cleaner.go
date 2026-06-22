package jobs

import (
	"context"
	"log"
	"os"
	"path/filepath"
	"time"

	"github.com/vladtrc/pdtt/internal/config"
)

// Cleaner periodically deletes expired render jobs (and their videos),
// expired generation logs, and orphaned work directories.
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
		// Debugger frames + vars.json live in debug/{id}; same retention as the job.
		if err := os.RemoveAll(filepath.Join(c.cfg.DataDir, "debug", e.ID)); err != nil {
			c.logf("remove debug dir %s: %v", e.ID, err)
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
