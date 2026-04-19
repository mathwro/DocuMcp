package crawler

import (
	"context"
	"log/slog"
	"sync"

	"github.com/mathwro/DocuMcp/internal/config"
	"github.com/mathwro/DocuMcp/internal/db"
	"github.com/robfig/cron/v3"
)

// Scheduler manages cron-based crawl jobs derived from a Config.
type Scheduler struct {
	mu      sync.Mutex
	cron    *cron.Cron
	crawler *Crawler
	store   *db.Store
}

// NewScheduler returns a new Scheduler. Call Load to register jobs and start the cron runner.
func NewScheduler(c *Crawler, store *db.Store) *Scheduler {
	return &Scheduler{
		cron:    cron.New(),
		crawler: c,
		store:   store,
	}
}

// Load replaces the running schedule with the one derived from cfg.
// Any existing schedule is stopped first and drained before the new schedule starts,
// preventing overlapping crawls of the same source during a config reload.
func (s *Scheduler) Load(cfg *config.Config) {
	s.mu.Lock()
	defer s.mu.Unlock()

	ctx := s.cron.Stop()
	<-ctx.Done() // wait for in-flight jobs to finish before replacing
	s.cron = cron.New()

	for _, src := range cfg.Sources {
		if src.CrawlSchedule == "" {
			continue
		}
		srcCopy := src // avoid loop-variable capture
		if _, err := s.cron.AddFunc(src.CrawlSchedule, func() {
			sources, err := s.store.ListSources()
			if err != nil {
				slog.Error("scheduler: list sources", "err", err)
				return
			}
			for _, dbSrc := range sources {
				if dbSrc.Name == srcCopy.Name {
					slog.Info("scheduled crawl", "source", dbSrc.Name)
					if err := s.crawler.Crawl(context.Background(), dbSrc); err != nil {
						slog.Error("scheduled crawl failed", "source", dbSrc.Name, "err", err)
					}
					return
				}
			}
			slog.Warn("scheduler: source not found in db", "source", srcCopy.Name)
		}); err != nil {
			slog.Error("scheduler: invalid cron expression",
				"source", src.Name, "schedule", src.CrawlSchedule, "err", err)
		}
	}

	s.cron.Start()
}

// Stop halts all scheduled crawl jobs and returns a context that is cancelled
// once all in-flight jobs have finished. Callers may block on ctx.Done() to
// wait for a clean shutdown.
func (s *Scheduler) Stop() context.Context {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.cron.Stop()
}
