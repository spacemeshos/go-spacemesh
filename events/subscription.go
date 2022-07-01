package events

import (
	"context"
)

func newSubconf(opts ...SubOpt) *subconf {
	conf := &subconf{buffer: 1 << 10}
	for _, opt := range opts {
		opt(conf)
	}
	return conf
}

type subconf struct {
	buffer int
}

// SubOpt for changing subscribe options.
type SubOpt func(*subconf)

// WithBuffer changes subscription buffer size.
func WithBuffer(n int) SubOpt {
	return func(conf *subconf) {
		conf.buffer = n
	}
}

func subscribe[T any](matcher func(*T) bool, opts ...SubOpt) (*BufferedSubscription[T], error) {
	sub, err := reporter.bus.Subscribe(new(T))
	if err != nil {
		return nil, err
	}
	ctx, cancel := context.WithCancel(context.Background())
	conf := newSubconf(opts...)
	bs := &BufferedSubscription[T]{
		cancel: cancel,
		result: make(chan T, conf.buffer),
		full:   make(chan struct{}),
	}
	go bs.run(ctx, sub, matcher)
	return bs, nil
}

// Subscribe to the objects of type T.
func Subscribe[T any](opts ...SubOpt) (*BufferedSubscription[T], error) {
	return subscribe[T](nil, opts...)
}

// SubscribeMatched subscribes and filters results before adding them to the Out channel.
func SubscribeMatched[T any](matcher func(*T) bool, opts ...SubOpt) (*BufferedSubscription[T], error) {
	return subscribe(matcher, opts...)
}

// BufferedSubscription is meant to be used by API subscribers.
//
// Majority of the events are published on the critical consensus path, and they
// can't block consensus progress if consumer is slow.
// To account for slow consumer each subscription maintains an internal buffer
// with specific events, if this buffer overflows consumer should be dropped
// and stream restarted.
type BufferedSubscription[T any] struct {
	cancel func()
	result chan T
	full   chan struct{}
}

// Close non-blocking close of the subscription.
func (sub *BufferedSubscription[T]) Close() {
	sub.cancel()
}

// Out is a channel with subscription results.
func (sub *BufferedSubscription[T]) Out() <-chan T {
	return sub.result
}

// Full is closed if subscriptions buffer overflows.
func (sub *BufferedSubscription[T]) Full() <-chan struct{} {
	return sub.full
}

func (sub *BufferedSubscription[T]) run(ctx context.Context, s Subscription, matcher func(*T) bool) {
	defer s.Close()
	for {
		select {
		case <-ctx.Done():
			return
		case evt := <-s.Out():
			typed := evt.(T)
			if matcher != nil && !matcher(&typed) {
				break
			}
			select {
			case sub.result <- typed:
			default:
				close(sub.full)
				return
			}
		}
	}
}
