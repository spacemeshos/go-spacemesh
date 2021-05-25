package shutdown

import (
	"github.com/spacemeshos/go-spacemesh/log"
	"sync"
)

type todo struct {
	component string
	routine   func()
}

type Handler struct {
	logger   log.Log
	shutdown chan struct{}
	// keeps the shutdown routines for each p2p component
	todos []todo
	mu    sync.Mutex
}

func NewHandler(logger log.Log) *Handler {
	return &Handler{
		logger:   logger,
		shutdown: make(chan struct{}),
		todos:    make([]todo, 0),
	}
}

// IsShuttingDown returns true if the shutdown channel has been closed.
func (h *Handler) IsShuttingDown() bool {
	select {
	case <-h.shutdown:
		return true
	default:
	}
	return false
}

func (h *Handler) Channel() chan struct{} {
	return h.shutdown
}

// RegisterRoutine register a shutdown routine for a component
func (h *Handler) RegisterRoutine(c string, f func()) {
	h.mu.Lock()
	defer h.mu.Unlock()
	// overwrite the previous one for the same component, if any
	h.todos = append(h.todos, todo{
		component: c,
		routine:   f,
	})
}

func (h *Handler) Shutdown() {
	if h.IsShuttingDown() {
		return
	}
	// signals all p2p components the node is shutting down
	close(h.shutdown)
	// runs the shutdown routines
	h.mu.Lock()
	defer h.mu.Unlock()
	// overwrite the previous one for the same component, if any
	for _, t := range h.todos {
		h.logger.Info("running shutdown routine for %h", t.component)
		var once sync.Once
		once.Do(t.routine)
	}
}

