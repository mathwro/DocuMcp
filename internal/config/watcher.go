package config

import (
	"log/slog"
	"sync"
	"time"

	"github.com/fsnotify/fsnotify"
)

// debounceWindow collapses the burst of events that editors and os.WriteFile
// emit for a single logical save (e.g. TRUNCATE + WRITE) so we only Load once
// the file has settled — otherwise we can read it mid-write and see empty YAML.
const debounceWindow = 150 * time.Millisecond

// Watcher watches a config file for changes and calls onChange with the new config.
type Watcher struct {
	fsw *fsnotify.Watcher
	wg  sync.WaitGroup
}

// Watch starts watching path. On each write event, loads the config and calls onChange.
// Returns a Watcher that must be stopped with Stop() when no longer needed.
func Watch(path string, onChange func(*Config)) (*Watcher, error) {
	fsw, err := fsnotify.NewWatcher()
	if err != nil {
		return nil, err
	}
	if err := fsw.Add(path); err != nil {
		fsw.Close()
		return nil, err
	}
	w := &Watcher{fsw: fsw}
	w.wg.Add(1)
	go func() {
		defer w.wg.Done()

		var timer *time.Timer
		var timerC <-chan time.Time
		for {
			select {
			case event, ok := <-fsw.Events:
				if !ok {
					if timer != nil {
						timer.Stop()
					}
					return
				}
				if event.Has(fsnotify.Write) || event.Has(fsnotify.Create) {
					if timer == nil {
						timer = time.NewTimer(debounceWindow)
						timerC = timer.C
					} else {
						if !timer.Stop() {
							select {
							case <-timer.C:
							default:
							}
						}
						timer.Reset(debounceWindow)
					}
				}
			case <-timerC:
				timer = nil
				timerC = nil
				cfg, err := Load(path)
				if err != nil {
					slog.Error("reload config", "path", path, "err", err)
					continue
				}
				onChange(cfg)
			case err, ok := <-fsw.Errors:
				if !ok {
					if timer != nil {
						timer.Stop()
					}
					return
				}
				slog.Error("watcher error", "err", err)
			}
		}
	}()
	return w, nil
}

// Stop shuts down the watcher and waits for the goroutine to exit.
func (w *Watcher) Stop() {
	w.fsw.Close()
	w.wg.Wait()
}
