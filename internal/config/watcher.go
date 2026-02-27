package config

import (
	"log/slog"
	"sync"

	"github.com/fsnotify/fsnotify"
)

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
		for {
			select {
			case event, ok := <-fsw.Events:
				if !ok {
					return
				}
				if event.Has(fsnotify.Write) || event.Has(fsnotify.Create) {
					cfg, err := Load(path)
					if err != nil {
						slog.Error("reload config", "path", path, "err", err)
						continue
					}
					onChange(cfg)
				}
			case err, ok := <-fsw.Errors:
				if !ok {
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
