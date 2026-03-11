package config

import (
	"log/slog"
	"time"

	"github.com/fsnotify/fsnotify"
	"github.com/kalvis/mesh/internal/model"
)

const debounceDelay = 100 * time.Millisecond

// Watcher watches a WORKFLOW.md file for changes and reloads the config.
// On a successful reload the onChange callback receives the new WorkflowDefinition.
// On a failed reload the onError callback receives the error; the caller is
// responsible for preserving the last known good configuration.
type Watcher struct {
	path      string
	fsWatcher *fsnotify.Watcher
	onChange  func(wf *model.WorkflowDefinition)
	onError   func(err error)
	logger    *slog.Logger
	stopCh    chan struct{}
	doneCh    chan struct{}
}

// NewWatcher creates a file watcher for the given WORKFLOW.md path.
// The watcher is created but not started; call Start to begin watching.
func NewWatcher(
	path string,
	onChange func(*model.WorkflowDefinition),
	onError func(error),
	logger *slog.Logger,
) (*Watcher, error) {
	fw, err := fsnotify.NewWatcher()
	if err != nil {
		return nil, err
	}
	return &Watcher{
		path:      path,
		fsWatcher: fw,
		onChange:  onChange,
		onError:   onError,
		logger:    logger,
		stopCh:    make(chan struct{}),
		doneCh:    make(chan struct{}),
	}, nil
}

// Start begins watching the file for changes. It is non-blocking; a background
// goroutine handles events until Stop is called.
func (w *Watcher) Start() error {
	if err := w.fsWatcher.Add(w.path); err != nil {
		return err
	}
	go w.loop()
	return nil
}

// Stop stops the watcher and waits for the background goroutine to exit.
func (w *Watcher) Stop() {
	close(w.stopCh)
	<-w.doneCh
	_ = w.fsWatcher.Close()
}

// loop is the main event loop running in its own goroutine.
func (w *Watcher) loop() {
	defer close(w.doneCh)

	var debounce *time.Timer

	for {
		select {
		case <-w.stopCh:
			if debounce != nil {
				debounce.Stop()
			}
			return

		case event, ok := <-w.fsWatcher.Events:
			if !ok {
				return
			}
			// Only react to Write and Create events.
			if !event.Has(fsnotify.Write) && !event.Has(fsnotify.Create) {
				continue
			}
			w.logger.Debug("file event", "op", event.Op.String(), "path", event.Name)

			// Debounce: reset the timer on every qualifying event.
			if debounce != nil {
				debounce.Stop()
			}
			debounce = time.AfterFunc(debounceDelay, func() {
				w.reload()
			})

		case err, ok := <-w.fsWatcher.Errors:
			if !ok {
				return
			}
			w.logger.Error("fsnotify error", "err", err)
			if w.onError != nil {
				w.onError(err)
			}
		}
	}
}

// reload re-parses the workflow file and dispatches to the appropriate callback.
func (w *Watcher) reload() {
	wf, err := LoadWorkflow(w.path)
	if err != nil {
		w.logger.Error("reload failed", "path", w.path, "err", err)
		if w.onError != nil {
			w.onError(err)
		}
		return
	}
	w.logger.Info("workflow reloaded", "path", w.path)
	if w.onChange != nil {
		w.onChange(wf)
	}
}
