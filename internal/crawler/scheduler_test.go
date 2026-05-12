package crawler

import (
	"testing"
	"time"

	"github.com/mathwro/DocuMcp/internal/config"
	"github.com/mathwro/DocuMcp/internal/testutil"
)

func TestNewSchedulerInitializesCancelableContext(t *testing.T) {
	store := testutil.OpenStore(t)
	s := NewScheduler(New(store, nil), store)
	defer func() {
		ctx := s.Stop()
		<-ctx.Done()
	}()

	if s.cron == nil {
		t.Fatal("cron is nil")
	}
	if s.crawler == nil {
		t.Fatal("crawler is nil")
	}
	if s.store == nil {
		t.Fatal("store is nil")
	}
	select {
	case <-s.ctx.Done():
		t.Fatal("scheduler context is canceled before Stop")
	default:
	}
}

func TestSchedulerLoadRegistersOnlyValidScheduledSources(t *testing.T) {
	store := testutil.OpenStore(t)
	s := NewScheduler(New(store, nil), store)
	defer func() {
		ctx := s.Stop()
		<-ctx.Done()
	}()

	s.Load(&config.Config{Sources: []config.SourceConfig{
		{Name: "unscheduled", Type: "web"},
		{Name: "scheduled", Type: "web", CrawlSchedule: "0 0 * * *"},
		{Name: "invalid", Type: "web", CrawlSchedule: "not cron"},
	}})

	if got := len(s.cron.Entries()); got != 1 {
		t.Fatalf("registered cron entries = %d, want 1", got)
	}
}

func TestSchedulerLoadReplacesExistingSchedule(t *testing.T) {
	store := testutil.OpenStore(t)
	s := NewScheduler(New(store, nil), store)
	defer func() {
		ctx := s.Stop()
		<-ctx.Done()
	}()

	s.Load(&config.Config{Sources: []config.SourceConfig{
		{Name: "first", Type: "web", CrawlSchedule: "0 0 * * *"},
	}})
	s.Load(&config.Config{Sources: []config.SourceConfig{
		{Name: "second", Type: "web", CrawlSchedule: "0 1 * * *"},
		{Name: "third", Type: "web", CrawlSchedule: "0 2 * * *"},
	}})

	if got := len(s.cron.Entries()); got != 2 {
		t.Fatalf("registered cron entries after reload = %d, want 2", got)
	}
}

func TestSchedulerStopCancelsBaseContext(t *testing.T) {
	store := testutil.OpenStore(t)
	s := NewScheduler(New(store, nil), store)

	done := s.Stop()
	select {
	case <-s.ctx.Done():
	case <-time.After(time.Second):
		t.Fatal("scheduler base context was not canceled")
	}
	select {
	case <-done.Done():
	case <-time.After(time.Second):
		t.Fatal("cron stop context was not canceled")
	}
}
