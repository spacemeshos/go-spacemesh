package util

import "context"

// Closer adds the ability to close objects.
type Closer struct {
	channel chan struct{} // closeable go routines listen to this channel
}

// NewCloser creates a new (not closed) closer.
func NewCloser() Closer {
	return Closer{make(chan struct{})}
}

// Close signals all listening instances to close.
// Note: should be called only once.
func (closer *Closer) Close() {
	close(closer.channel)
}

// CloseChannel returns the channel to wait on for close signal.
func (closer *Closer) CloseChannel() chan struct{} {
	return closer.channel
}

// Context returns a context which is done when channel closes.
func (closer *Closer) Context() context.Context {
	ctx, cancel := context.WithCancel(context.Background())

	go func() {
		select {
		case <-closer.channel:
			cancel()
		case <-ctx.Done():
		}
	}()

	return ctx
}

// IsClosed returns whether the channel is closed.
func (closer *Closer) IsClosed() bool {
	select {
	case <-closer.channel:
		return true
	default:
		return false
	}
}
