package lifecycle

import (
	"net/http"
	"sync"
)

// Requests closes admission before waiting, so no handler can add database or
// child-process work after shutdown has passed its final barrier.
type Requests struct {
	mu     sync.Mutex
	closed bool
	work   sync.WaitGroup
}

func (r *Requests) Handler(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		r.mu.Lock()
		if r.closed {
			r.mu.Unlock()
			http.Error(w, "service stopping", http.StatusServiceUnavailable)
			return
		}
		r.work.Add(1)
		r.mu.Unlock()
		defer r.work.Done()
		next.ServeHTTP(w, req)
	})
}
func (r *Requests) StopAdmission() { r.mu.Lock(); r.closed = true; r.mu.Unlock() }
func (r *Requests) Wait()          { r.work.Wait() }
