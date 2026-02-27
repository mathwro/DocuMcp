package config

import (
	"log/slog"

	"github.com/fsnotify/fsnotify"
)

// Watcher watches a config file for changes and calls onChange with the new config.
type Watcher struct {
	fsw *fsnotify.Watcher
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
	go func() {
		for {
			select {
			case event, ok := <-fsw.Events:
				if !ok {
					return
				}
				if event.Has(fsnotify.Write) || event.Has(fsnotify.Create) {
					cfg, err := Load(path)
					if err != nil {
						slog.Error("reload config", "err", err)
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
	return &Watcher{fsw: fsw}, nil
}

// Stop shuts down the watcher. Safe to call multiple times.
func (w *Watcher) Stop() {
	w.fsw.Close()
}
