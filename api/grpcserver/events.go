package grpcserver

import (
	"context"

	"github.com/libp2p/go-libp2p-core/event"

	"github.com/spacemeshos/go-spacemesh/log"
)

const subscriptionChanBufSize = 1 << 16

func consumeEvents(ctx context.Context, in <-chan interface{}) <-chan interface{} {
	out := make(chan interface{}, subscriptionChanBufSize)

	go func() {
		defer close(out)

		for e := range in {
			select {
			case <-ctx.Done():
				return
			case out <- e:
			default:
				log.With().Debug("subscriber's event buffer is full")
				return
			}
		}
	}()

	return out
}

func closeSubscription(accountSubscription event.Subscription) {
	if err := accountSubscription.Close(); err != nil {
		log.With().Panic("Failed to close account subscription: " + err.Error())
	}
}
