package nats

import (
	"encoding/json"
	"fmt"

	"github.com/libp2p/go-libp2p/core/event"
	"github.com/nats-io/nats.go"
	"github.com/spacemeshos/go-spacemesh/common/types"
	"github.com/spacemeshos/go-spacemesh/events"
	"github.com/spacemeshos/go-spacemesh/log"
)

type NatsConnector struct {
	nc                   *nats.Conn
	layersSubscription   event.Subscription
	rewardsSubscription  event.Subscription
	txSubscription       event.Subscription
	txResultSubscription event.Subscription
	atxSubscription      event.Subscription
}

func NewNatsConnector(config Config) (*NatsConnector, error) {
	natsURL := config.NatsUrl
	nc, err := nats.Connect(natsURL)
	if err != nil {
		log.With().Warning("failed to connect to nats")
		return &NatsConnector{}, err
	}
	js, err := nc.JetStream()
	if err != nil {
		log.With().Warning("failed to create jetstream")
		return &NatsConnector{}, err
	}
	layersSubscription := addLayers(js, config)
	rewardsSubscription := addRewards(js, config)
	txSubscription := addTransations(js, config)
	txResultSubscription := addTransationsResult(js, config)
	atxSubscription := addATXs(js, config)
	return &NatsConnector{
		nc:                   nc,
		layersSubscription:   layersSubscription,
		rewardsSubscription:  rewardsSubscription,
		txSubscription:       txSubscription,
		txResultSubscription: txResultSubscription,
		atxSubscription:      atxSubscription,
	}, err

}

func addLayers(js nats.JetStreamContext, config Config) event.Subscription {
	js.AddStream(&nats.StreamConfig{
		Name:     "layers",
		Subjects: []string{"layers"},
		MaxAge:   config.NatsMaxAge,
	})
	layersSubscription := events.SubscribeLayers()
	if layersSubscription != nil {
		fmt.Print("NATS subscribed to layers")
		go func() {
			for l := range layersSubscription.Out() {
				event, ok := l.(events.LayerUpdate)
				if !ok {
					log.With().Warning("received invalid event type - dropping")
					continue
				}
				jsonData, err := json.Marshal(event)
				if err != nil {
					log.With().Warning("failed to serialize event")
					continue
				}
				js.Publish("layers", jsonData)
			}
		}()
	}
	return layersSubscription
}

func addRewards(js nats.JetStreamContext, config Config) event.Subscription {
	js.AddStream(&nats.StreamConfig{
		Name:     "rewards",
		Subjects: []string{"rewards"},
		MaxAge:   config.NatsMaxAge,
	})
	rewardsSubscription := events.SubscribeRewards()
	if rewardsSubscription != nil {
		fmt.Print("NATS subscribed to rewards")
		go func() {
			for r := range rewardsSubscription.Out() {
				event, ok := r.(events.Reward)
				if !ok {
					log.With().Warning("received invalid event type - dropping")
					continue
				}
				jsonData, err := json.Marshal(event)
				if err != nil {
					log.With().Warning("failed to serialize event")
					continue
				}
				js.Publish("rewards", jsonData)
			}
		}()
	}
	return rewardsSubscription
}

func addTransations(js nats.JetStreamContext, config Config) event.Subscription {
	js.AddStream(&nats.StreamConfig{
		Name:     "transactions-created",
		Subjects: []string{"transactions.created"},
		MaxAge:   config.NatsMaxAge,
	})
	txSubscription := events.SubscribeTxs()
	if txSubscription != nil {
		fmt.Print("NATS subscribed to transactions")
		go func() {
			for r := range txSubscription.Out() {
				event, ok := r.(events.Transaction)
				if !ok {
					log.With().Warning("received invalid event type - dropping")
					continue
				}
				jsonData, err := json.Marshal(event)
				if err != nil {
					log.With().Warning("failed to serialize event")
					continue
				}
				js.Publish("transactions.created", jsonData)
			}
		}()
	}
	return txSubscription
}

func addTransationsResult(js nats.JetStreamContext, config Config) event.Subscription {
	js.AddStream(&nats.StreamConfig{
		Name:     "transactions-result",
		Subjects: []string{"transactions.result"},
		MaxAge:   config.NatsMaxAge,
	})
	txResultSubscription := events.SubscribeTxsResult()
	if txResultSubscription != nil {
		fmt.Print("NATS subscribed to transactions results")
		go func() {
			for r := range txResultSubscription.Out() {
				event, ok := r.(types.TransactionWithResult)
				if !ok {
					log.With().Warning("received invalid event type - dropping")
					continue
				}
				jsonData, err := json.Marshal(event)
				if err != nil {
					log.With().Warning("failed to serialize event")
					continue
				}
				js.Publish("transactions.result", jsonData)
			}
		}()
	}
	return txResultSubscription
}

func addATXs(js nats.JetStreamContext, config Config) event.Subscription {
	js.AddStream(&nats.StreamConfig{
		Name:     "atx",
		Subjects: []string{"atx"},
		MaxAge:   config.NatsMaxAge,
	})
	atxSubscription := events.SubscribeActivations()
	if atxSubscription != nil {
		fmt.Print("NATS subscribed to atx")
		go func() {
			for r := range atxSubscription.Out() {
				event, ok := r.(events.ActivationTx)
				if !ok {
					log.With().Warning("received invalid event type - dropping")
					continue
				}
				jsonData, err := json.Marshal(event)
				if err != nil {
					log.With().Warning("failed to serialize event")
					continue
				}
				js.Publish("atx", jsonData)
			}
		}()
	}
	return atxSubscription
}

func (n *NatsConnector) Close() {
	n.nc.Drain()
	if err := n.layersSubscription.Close(); err != nil {
		log.With().Panic("Failed to close subscription", log.Err(err))
	}
	if err := n.rewardsSubscription.Close(); err != nil {
		log.With().Panic("Failed to close subscription", log.Err(err))
	}
	if err := n.txSubscription.Close(); err != nil {
		log.With().Panic("Failed to close subscription", log.Err(err))
	}
	if err := n.txResultSubscription.Close(); err != nil {
		log.With().Panic("Failed to close subscription", log.Err(err))
	}
	if err := n.atxSubscription.Close(); err != nil {
		log.With().Panic("Failed to close subscription", log.Err(err))
	}
}
