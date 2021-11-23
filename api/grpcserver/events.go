package grpcserver

import (
	"context"

	"github.com/libp2p/go-libp2p-core/event"

	"github.com/spacemeshos/go-spacemesh/log"
)

const subscriptionChanBufSize = 1 << 16

func consumeEvents(ctx context.Context, in <-chan interface{}) (out <-chan interface{}, bufFull <-chan struct{}) {
	outCh := make(chan interface{}, subscriptionChanBufSize)
	bufFullCh := make(chan struct{})

	go func() {
		defer close(outCh)

		for e := range in {
			select {
			case <-ctx.Done():
				return
			case outCh <- e:
			default:
				log.With().Debug("subscriber's event buffer is full")
				close(bufFullCh)
				return
			}
		}
	}()

	return outCh, bufFullCh
}

func closeSubscription(accountSubscription event.Subscription) {
	if err := accountSubscription.Close(); err != nil {
		log.With().Panic("Failed to close account subscription: " + err.Error())
	}
}
