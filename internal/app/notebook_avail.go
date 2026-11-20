package app

import (
	"os"
	"sync"
	"time"
)

// notebookAvailability checks recent notebook folders in the background each
// time the switcher opens. Unknown (still checking or timed out) counts as
// available so a slow or sleeping drive never greys out a folder by mistake.
type notebookAvailability struct {
	mu      sync.Mutex
	missing map[string]bool
	pending int
	gen     int
}

const notebookStatTimeout = 2 * time.Second

func (a *notebookAvailability) Check(roots []string, redraw func()) {
	a.mu.Lock()
	a.gen++
	gen := a.gen
	a.pending = len(roots)
	a.mu.Unlock()
	for _, root := range roots {
		root := root
		go func() {
			done := make(chan bool, 1)
			go func() {
				st, err := os.Stat(root)
				done <- err != nil && os.IsNotExist(err) || err == nil && !st.IsDir()
			}()
			var gone, known bool
			select {
			case gone = <-done:
				known = true
			case <-time.After(notebookStatTimeout):
			}
			a.mu.Lock()
			if a.gen == gen {
				if a.missing == nil {
					a.missing = map[string]bool{}
				}
				if known {
					a.missing[root] = gone
				}
				a.pending--
			}
			a.mu.Unlock()
			if redraw != nil {
				redraw()
			}
		}()
	}
}

func (a *notebookAvailability) Missing(root string) bool {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.missing[root]
}

func (a *notebookAvailability) Checking() bool {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.pending > 0
}

func (a *notebookAvailability) Forget(root string) {
	a.mu.Lock()
	delete(a.missing, root)
	a.mu.Unlock()
}
