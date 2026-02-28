package crawler

import (
	"context"
	"log/slog"

	"github.com/documcp/documcp/internal/config"
	"github.com/documcp/documcp/internal/db"
	"github.com/robfig/cron/v3"
)

// Scheduler manages cron-based crawl jobs derived from a Config.
type Scheduler struct {
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
// Any existing schedule is stopped first, then a fresh cron instance is started.
func (s *Scheduler) Load(cfg *config.Config) {
	s.cron.Stop()
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

// Stop halts all scheduled crawl jobs.
func (s *Scheduler) Stop() { s.cron.Stop() }
