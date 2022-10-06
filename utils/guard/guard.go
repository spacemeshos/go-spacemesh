package guard

import "sync"

// Guard is a thread-safe wrapper around T.
// It guarantees a thread-safe access to the underlying value T.
type Guard[T any] struct {
	value T
	mutex sync.Mutex
}

func New[T any](t T) Guard[T] {
	return Guard[T]{value: t}
}

// Apply safely runs f under a lock, passing it a pointer to the
// guarded value.
func (g *Guard[T]) Apply(f func(*T)) {
	g.mutex.Lock()
	defer g.mutex.Unlock()
	f(&g.value)
}
