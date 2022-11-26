package grpcserver

import (
	"context"

	"github.com/libp2p/go-libp2p/core/event"

	"github.com/spacemeshos/go-spacemesh/log"
)

const subscriptionChanBufSize = 1 << 16

// Errors for cases with a full event buffer.
var (
	errTxBufferFull          = "tx buffer is full"
	errLayerBufferFull       = "layer buffer is full"
	errAccountBufferFull     = "account buffer is full"
	errRewardsBufferFull     = "rewards buffer is full"
	errActivationsBufferFull = "activations buffer is full"
	errStatusBufferFull      = "status buffer is full"
	errErrorsBufferFull      = "errors buffer is full"
)

func consumeEvents(ctx context.Context, subscription event.Subscription) (out <-chan any, bufFull <-chan struct{}) {
	outCh := make(chan any, subscriptionChanBufSize)
	bufFullCh := make(chan struct{})

	go func() {
		defer closeSubscription(subscription)

		for e := range subscription.Out() {
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
