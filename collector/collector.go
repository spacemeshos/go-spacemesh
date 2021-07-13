// Package collector implements event collecting from pubsub
package collector

import (
	"unsafe"

	"github.com/spacemeshos/go-spacemesh/common/types"
	"github.com/spacemeshos/go-spacemesh/events"
	"github.com/spacemeshos/go-spacemesh/log"
)

// EventsCollector collects events from node and writes them to DB
type EventsCollector struct {
	url  string
	stop chan struct{}
	db   DB
}

// NewCollector created a new instance of the collector listening on url for events and writing them to the provided DB
func NewCollector(db DB, url string) *EventsCollector {
	return &EventsCollector{url, make(chan struct{}), db}
}

// DB defines which events should be stores by any db that the collector uses
type DB interface {
	StoreBlock(event *events.NewBlock) error
	StoreBlockValid(event *events.ValidBlock) error
	StoreTx(event *events.NewTx) error
	StoreTxValid(event *events.ValidTx) error
	StoreAtx(event *events.NewAtx) error
	StoreAtxValid(event *events.ValidAtx) error
	StoreReward(event *events.RewardReceived) error
	StoreBlockCreated(event *events.DoneCreatingBlock) error
	StoreAtxCreated(event *events.AtxCreated) error
	StoreTortoiseBeaconCalculated(event *events.TortoiseBeaconCalculated) error
}

// Start starts collecting events
func (c *EventsCollector) Start(blocking bool) {
	if blocking {
		c.collectEvents(c.url)
	} else {
		go c.collectEvents(c.url)
	}

}

// Stop stops collecting events.
func (c *EventsCollector) Stop() {
	c.stop <- struct{}{}
}

func (c *EventsCollector) collectEvents(url string) {
	sub, err := events.NewSubscriber(url)
	if err != nil {
		log.Debug("cannot start subscriber")
		return
	}
	blocks, err := sub.Subscribe(events.EventNewBlock)
	if err != nil {
		log.Error("cannot start subscriber")
		return
	}

	blocksValid, err := sub.Subscribe(events.EventBlockValid)
	if err != nil {
		log.Error("cannot start subscriber %v", events.EventBlockValid)
		return
	}
	txs, err := sub.Subscribe(events.EventNewTx)
	if err != nil {
		log.Error("cannot start subscriber %v", events.EventNewTx)
		return
	}
	txValid, err := sub.Subscribe(events.EventTxValid)
	if err != nil {
		log.Error("cannot start subscriber %v", events.EventTxValid)
		return
	}
	atxs, err := sub.Subscribe(events.EventNewAtx)
	if err != nil {
		log.Error("cannot start subscriber %v", events.EventNewAtx)
		return
	}
	atxsValid, err := sub.Subscribe(events.EventAtxValid)
	if err != nil {
		log.Error("cannot start subscriber %v", events.EventAtxValid)
		return
	}
	reward, err := sub.Subscribe(events.EventRewardReceived)
	if err != nil {
		log.Error("cannot start subscriber %v", events.EventRewardReceived)
		return
	}
	created, err := sub.Subscribe(events.EventCreatedBlock)
	if err != nil {
		log.Error("cannot start subscriber %v", events.EventCreatedBlock)
		return
	}
	createdAtx, err := sub.Subscribe(events.EventCreatedAtx)
	if err != nil {
		log.Error("cannot start subscriber %v", events.EventCreatedAtx)
		return
	}
	tortoiseBeacons, err := sub.Subscribe(events.EventCalculatedTortoiseBeacon)
	if err != nil {
		log.Error("cannot start subscriber %v", events.EventCalculatedTortoiseBeacon)
		return
	}
	sub.StartListening()
	// get the size of message header
	size := unsafe.Sizeof(events.EventTxValid)

loop:
	for {
		select {
		case data := <-blocks:
			var e events.NewBlock
			err := types.BytesToInterface(data[size:], &e)
			if err != nil {
				log.Error("cannot parse received message %v", err)
			}
			log.Debug("got new block %v", e)
			err = c.db.StoreBlock(&e)
			if err != nil {
				log.Error("cannot write message %v", err)
			}
		case data := <-blocksValid:
			var e events.ValidBlock
			err := types.BytesToInterface(data[size:], &e)
			if err != nil {
				log.Error("cannot parse received message %v", err)
			}
			log.Debug("got new block validation %v", e)
			err = c.db.StoreBlockValid(&e)
			if err != nil {
				log.Error("cannot write message %v", err)
			}
		case data := <-txs:
			var e events.NewTx
			err := types.BytesToInterface(data[size:], &e)
			if err != nil {
				log.Error("cannot parse received message %v", err)
			}
			log.Debug("got new tx %v", e)
			err = c.db.StoreTx(&e)
			if err != nil {
				log.Error("cannot write message %v", err)
			}
		case data := <-txValid:
			var e events.ValidTx
			err := types.BytesToInterface(data[size:], &e)
			if err != nil {
				log.Error("cannot parse received message %v", err)
			}
			log.Debug("got new tx validation %v", e)
			err = c.db.StoreTxValid(&e)
			if err != nil {
				log.Error("cannot write message %v", err)
			}
		case data := <-atxs:
			var e events.NewAtx
			err := types.BytesToInterface(data[size:], &e)
			if err != nil {
				log.Error("cannot parse received message %v", err)
			}
			log.Debug("got new atx %v", e)
			err = c.db.StoreAtx(&e)
			if err != nil {
				log.Error("cannot write message %v", err)
			}
		case data := <-atxsValid:
			var e events.ValidAtx
			err := types.BytesToInterface(data[size:], &e)
			if err != nil {
				log.Error("cannot parse received message %v", err)
			}
			log.Debug("got new atx validation %v", e)
			err = c.db.StoreAtxValid(&e)
			if err != nil {
				log.Error("cannot write message %v", err)
			}
		case data := <-reward:
			var e events.RewardReceived
			err := types.BytesToInterface(data[size:], &e)
			if err != nil {
				log.Error("cannot parse received message %v", err)
			}
			log.Debug("got new reward %v", e)
			err = c.db.StoreReward(&e)
			if err != nil {
				log.Error("cannot write message %v", err)
			}
		case data := <-created:
			var e events.DoneCreatingBlock
			err := types.BytesToInterface(data[size:], &e)
			if err != nil {
				log.Error("cannot parse received message %v", err)
			}
			log.Debug("got new block created %v", e)
			err = c.db.StoreBlockCreated(&e)
			if err != nil {
				log.Error("cannot write message %v", err)
			}
		case data := <-createdAtx:
			var e events.AtxCreated
			err := types.BytesToInterface(data[size:], &e)
			if err != nil {
				log.Error("cannot parse received message %v", err)
			}
			log.Debug("got new atx created %v", e)
			err = c.db.StoreAtxCreated(&e)
			if err != nil {
				log.Error("cannot write message %v", err)
			}
		case data := <-tortoiseBeacons:
			var e events.TortoiseBeaconCalculated
			err := types.BytesToInterface(data[size:], &e)
			if err != nil {
				log.Error("cannot parse received message %v", err)
			}
			log.Debug("got new tortoise beacon calculated %v", e)
			err = c.db.StoreTortoiseBeaconCalculated(&e)
			if err != nil {
				log.Error("cannot write message %v", err)
			}
		case <-c.stop:
			break loop
		}
	}
}
