package envstore

import (
	"context"
	"os"
	"sort"
	"sync"
	"time"

	"envbridge/internal/envconfig"
	"envbridge/internal/logger"
)

// Store holds the parsed .env values behind an RWMutex so the proxy can read
// them concurrently while a watcher goroutine hot-reloads the file.
type Store struct {
	mu    sync.RWMutex
	vars  map[string]string
	path  string
	mtime time.Time
	size  int64
	log   *logger.Logger
}

// New loads path if it exists. A missing file is not an error: the store
// starts empty and will pick it up when the watcher sees it appear.
func New(path string, log *logger.Logger) (*Store, error) {
	s := &Store{path: path, vars: map[string]string{}, log: log}
	if _, err := os.Stat(path); err != nil {
		if os.IsNotExist(err) {
			return s, nil
		}
		return nil, err
	}
	if err := s.reloadLocked(); err != nil {
		return nil, err
	}
	return s, nil
}

// Get implements the proxy.VarSource interface.
func (s *Store) Get(name string) (string, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	v, ok := s.vars[name]
	return v, ok
}

// Len returns the number of loaded variables.
func (s *Store) Len() int {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return len(s.vars)
}

// Names returns the sorted variable names (never their values), safe to show
// on the dashboard.
func (s *Store) Names() []string {
	s.mu.RLock()
	defer s.mu.RUnlock()
	names := make([]string, 0, len(s.vars))
	for k := range s.vars {
		names = append(names, k)
	}
	sort.Strings(names)
	return names
}

// Path returns the .env path being watched.
func (s *Store) Path() string { return s.path }

// Reload re-reads the file when its mtime/size changed. It reports whether a
// reload actually happened.
func (s *Store) Reload() (bool, error) {
	fi, err := os.Stat(s.path)
	if err != nil {
		if os.IsNotExist(err) {
			return false, nil
		}
		return false, err
	}
	s.mu.RLock()
	unchanged := fi.Size() == s.size && fi.ModTime().Equal(s.mtime)
	s.mu.RUnlock()
	if unchanged {
		return false, nil
	}
	if err := s.reloadLocked(); err != nil {
		return false, err
	}
	return true, nil
}

func (s *Store) reloadLocked() error {
	fi, err := os.Stat(s.path)
	if err != nil {
		return err
	}
	vars, err := envconfig.Load(s.path)
	if err != nil {
		return err
	}
	s.mu.Lock()
	s.vars = vars
	s.mtime = fi.ModTime()
	s.size = fi.Size()
	s.mu.Unlock()
	if s.log != nil {
		// Don't treat HOST/PORT as secrets — they need to appear in logs.
		filtered := make(map[string]string, len(vars))
		for k, v := range vars {
			if k == "HOST" || k == "PORT" || k == "host" || k == "port" {
				continue
			}
			filtered[k] = v
		}
		s.log.RegisterSecretsFrom(filtered)
	}
	return nil
}

// Watch polls the file until ctx is cancelled. mtime polling keeps the binary
// dependency-free (no fsnotify).
func (s *Store) Watch(ctx context.Context, interval time.Duration) {
	if interval <= 0 {
		interval = 1500 * time.Millisecond
	}
	t := time.NewTicker(interval)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			changed, err := s.Reload()
			if err != nil {
				s.log.Warn("env reload failed: %v", err)
				continue
			}
			if changed {
				s.log.Info("hot-reloaded %d variables from %s", s.Len(), s.path)
			}
		}
	}
}
