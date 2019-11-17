package collector

import (
	"github.com/spacemeshos/go-spacemesh/common/types"
	"github.com/spacemeshos/go-spacemesh/events"
	"github.com/spacemeshos/go-spacemesh/log"
	"unsafe"
)

// eventsCollector collects events from node and writes them to DB
type eventsCollector struct {
	url  string
	stop chan struct{}
	db   Db
}

// NewCollector created a new instance of the collector listening on url for events and writing them to the provided Db
func NewCollector(db Db, url string) *eventsCollector {
	return &eventsCollector{url, make(chan struct{}), db}
}

type Db interface {
	StoreBlock(event *events.NewBlock) error
	StoreBlockValid(event *events.ValidBlock) error
	StoreTx(event *events.NewTx) error
	StoreTxValid(event *events.ValidTx) error
	StoreAtx(event *events.NewAtx) error
	StoreAtxValid(event *events.ValidAtx) error
	StoreReward(event *events.RewardReceived) error
}

// Starts collecting events
func (c *eventsCollector) Start(blocking bool) {
	if blocking {
		c.collectEvents(c.url)
	} else {
		go c.collectEvents(c.url)
	}

}

func (c *eventsCollector) Stop() {
	c.stop <- struct{}{}
}

func (c *eventsCollector) collectEvents(url string) {
	sub, err := events.NewSubscriber(url)
	if err != nil {
		log.Info("cannot start subscriber")
		return
	}
	blocks, err := sub.Subscribe(events.EventNewBlock)
	blocksValid, err := sub.Subscribe(events.EventBlockValid)
	txs, err := sub.Subscribe(events.EventNewTx)
	txValid, err := sub.Subscribe(events.EventTxValid)
	atxs, err := sub.Subscribe(events.EventNewAtx)
	atxsValid, err := sub.Subscribe(events.EventAtxValid)
	reward, err := sub.Subscribe(events.EventRewardReceived)
	sub.StartListening()
	if err != nil {
		log.Info("cannot start subscriber")
		return
	}
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
			log.Info("got new block %v", e)
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
			log.Info("got new block validation %v", e)
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
			log.Info("got new tx %v", e)
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
			log.Info("got new tx validation %v", e)
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
			log.Info("got new atx %v", e)
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
			log.Info("got new atx validation %v", e)
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
			log.Info("got new reward %v", e)
			err = c.db.StoreReward(&e)
			if err != nil {
				log.Error("cannot write message %v", err)
			}
		case <-c.stop:
			break loop
		}
	}
}
