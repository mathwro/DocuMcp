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
	ctx     context.Context    // base context for scheduled crawls
	cancel  context.CancelFunc // cancels ctx on Stop
}

// NewScheduler returns a new Scheduler. Call Load to register jobs and start the cron runner.
func NewScheduler(c *Crawler, store *db.Store) *Scheduler {
	ctx, cancel := context.WithCancel(context.Background())
	return &Scheduler{
		cron:    cron.New(),
		crawler: c,
		store:   store,
		ctx:     ctx,
		cancel:  cancel,
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
					if err := s.crawler.Crawl(s.ctx, dbSrc); err != nil {
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
// once all in-flight jobs have finished. The base scheduler context is also
// cancelled so any long-running crawl observes ctx.Done() and unwinds promptly
// rather than stalling shutdown.
func (s *Scheduler) Stop() context.Context {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.cancel()
	return s.cron.Stop()
}
