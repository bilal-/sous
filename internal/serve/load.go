package serve

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"time"
)

type boardLoad struct {
	done     chan struct{}
	cancel   context.CancelFunc
	err      error // guarded by Server.loadMu
	finished bool
}
type loadStatus struct {
	Loading bool   `json:"loading"`
	Ready   bool   `json:"ready"`
	Error   string `json:"error,omitempty"`
}

func (s *Server) startBoardLoad(w http.ResponseWriter, r *http.Request) {
	// A bodyless request can still use chunked framing through an HTTP proxy.
	// Bound the read and inspect bytes rather than rejecting that valid framing.
	controller := http.NewResponseController(w)
	_ = controller.SetReadDeadline(time.Now().Add(5 * time.Second))
	body, err := io.ReadAll(io.LimitReader(r.Body, 1))
	if err != nil || len(body) != 0 {
		http.Error(w, "board loading takes no body", http.StatusBadRequest)
		return
	}
	_ = controller.SetReadDeadline(time.Time{})
	s.loadMu.Lock()
	if s.closed || s.LoadBoard == nil {
		s.loadMu.Unlock()
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusServiceUnavailable)
		json.NewEncoder(w).Encode(loadStatus{Error: "Board loading is unavailable. Update sous and reopen its page."})
		return
	}
	if s.load == nil || s.load.finished {
		root := s.Context
		if root == nil {
			root = context.Background()
		}
		ctx, cancel := context.WithTimeout(root, 2*time.Minute)
		work := &boardLoad{done: make(chan struct{}), cancel: cancel}
		s.load = work
		go func() {
			defer cancel()
			err := s.LoadBoard(ctx)
			if err == nil {
				err = ctx.Err()
			}
			s.loadMu.Lock()
			work.err = err
			work.finished = true
			close(work.done)
			s.loadMu.Unlock()
		}()
	}
	s.loadMu.Unlock()
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(http.StatusAccepted)
	json.NewEncoder(w).Encode(loadStatus{Loading: true})
}
func (s *Server) boardLoadStatus(w http.ResponseWriter) {
	s.loadMu.Lock()
	status := loadStatus{}
	if s.load != nil {
		status.Loading = !s.load.finished
		status.Ready = s.load.finished && s.load.err == nil
		if s.load.err != nil {
			status.Error = "Board loading stopped or failed. Try again; sous doctor has more details."
		}
	}
	s.loadMu.Unlock()
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	json.NewEncoder(w).Encode(status)
}

// WaitLoad waits for owned work to be reaped. Its outcome stays in the status
// endpoint; an error here means shutdown exceeded its budget.
func (s *Server) WaitLoad(ctx context.Context) error {
	s.loadMu.Lock()
	work := s.load
	s.loadMu.Unlock()
	if work == nil {
		return nil
	}
	select {
	case <-work.done:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}
func (s *Server) Close(ctx context.Context) error {
	s.loadMu.Lock()
	s.closed = true
	if s.load != nil {
		s.load.cancel()
	}
	s.loadMu.Unlock()
	return s.WaitLoad(ctx)
}
