package grpcserver

import (
	"github.com/libp2p/go-libp2p-core/event"

	"github.com/spacemeshos/go-spacemesh/log"
)

const subscriptionChanBufSize = 1 << 16

func consumeEvents(inputCh <-chan interface{}) <-chan interface{} {
	outputCh := make(chan interface{}, subscriptionChanBufSize)

	go func(inputCh <-chan interface{}) {
		for e := range inputCh {
			outputCh <- e
		}

		close(outputCh)
	}(inputCh)

	return outputCh
}

func closeSubscription(accountSubscription event.Subscription) {
	if err := accountSubscription.Close(); err != nil {
		log.With().Panic("Failed to close account subscription: " + err.Error())
	}
}
