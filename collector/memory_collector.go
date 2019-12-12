package collector

import (
	"github.com/spacemeshos/go-spacemesh/common/types"
	"github.com/spacemeshos/go-spacemesh/events"
	"sync"
)

type MemoryCollector struct {
	events                 map[events.ChannelId][]Event
	doneCreatingBlockEvent map[uint64][]*events.DoneCreatingBlock
	doneCreatingAtxEvent   map[uint64][]*events.AtxCreated
	createdAtxs            map[uint64][]string
	gotBlockEvent          map[uint64][]*events.NewBlock
	gotAtxEvent            map[uint64][]*events.NewAtx
	Atxs                   map[string]uint64

	lck sync.RWMutex
}

func NewMemoryCollector() *MemoryCollector {
	return &MemoryCollector{
		events:                 make(map[events.ChannelId][]Event),
		doneCreatingBlockEvent: make(map[uint64][]*events.DoneCreatingBlock),
		doneCreatingAtxEvent:   make(map[uint64][]*events.AtxCreated),
		gotBlockEvent:          make(map[uint64][]*events.NewBlock),
		gotAtxEvent:            make(map[uint64][]*events.NewAtx),
		Atxs:                   make(map[string]uint64),
		createdAtxs:            make(map[uint64][]string),
	}
}

type Event interface {
	GetChannel() events.ChannelId
}

func (c *MemoryCollector) StoreBlock(event *events.NewBlock) error {
	c.lck.Lock()
	c.events[event.GetChannel()] = append(c.events[event.GetChannel()], event)
	c.gotBlockEvent[event.Layer] = append(c.gotBlockEvent[event.Layer], event)
	c.lck.Unlock()
	return nil
}

func (c *MemoryCollector) StoreBlockValid(event *events.ValidBlock) error {
	/*c.lck.Lock()
	c.events[event.GetChannel()] = append(c.events[event.GetChannel()], event)
	c.lck.Unlock()*/
	return nil
}

func (c *MemoryCollector) StoreTx(event *events.NewTx) error {
	/*c.lck.Lock()
	c.events[event.GetChannel()] = append(c.events[event.GetChannel()], event)
	c.lck.Unlock()*/
	return nil
}

func (c *MemoryCollector) StoreTxValid(event *events.ValidTx) error {
	/*c.lck.Lock()
	c.events[event.GetChannel()] = append(c.events[event.GetChannel()], event)
	c.lck.Unlock()*/
	return nil
}

func (c *MemoryCollector) StoreAtx(event *events.NewAtx) error {
	c.lck.Lock()
	c.events[event.GetChannel()] = append(c.events[event.GetChannel()], event)
	c.gotAtxEvent[event.LayerId] = append(c.gotAtxEvent[event.LayerId], event)
	c.Atxs[event.Id]++
	c.lck.Unlock()
	return nil
}

func (c *MemoryCollector) StoreAtxValid(event *events.ValidAtx) error {
	/*c.lck.Lock()
	c.events[event.GetChannel()] = append(c.events[event.GetChannel()], event)
	c.lck.Unlock()*/
	return nil
}

func (c *MemoryCollector) StoreReward(event *events.RewardReceived) error {
	/*c.lck.Lock()
	c.events[event.GetChannel()] = append(c.events[event.GetChannel()], event)
	c.lck.Unlock()*/
	return nil
}

func (c *MemoryCollector) StoreBlockCreated(event *events.DoneCreatingBlock) error {
	c.lck.Lock()
	c.events[event.GetChannel()] = append(c.events[event.GetChannel()], event)
	c.doneCreatingBlockEvent[event.Layer] = append(c.doneCreatingBlockEvent[event.Layer], event)
	c.lck.Unlock()
	return nil
}

func (c *MemoryCollector) StoreAtxCreated(event *events.AtxCreated) error {
	c.lck.Lock()
	c.events[event.GetChannel()] = append(c.events[event.GetChannel()], event)
	c.doneCreatingAtxEvent[event.Layer] = append(c.doneCreatingAtxEvent[event.Layer], event)
	if event.Created {
		c.createdAtxs[event.Layer] = append(c.createdAtxs[event.Layer], event.Id)
	}
	c.lck.Unlock()
	return nil
}

func (c *MemoryCollector) GetAtxCreationDone(layer types.EpochId) int {
	c.lck.RLock()
	defer c.lck.RUnlock()
	return len(c.doneCreatingAtxEvent[uint64(layer)])
}

func (c *MemoryCollector) GetCreatedAtx(epoch types.EpochId) []string {
	c.lck.RLock()
	defer c.lck.RUnlock()
	return c.createdAtxs[uint64(epoch)]
}

func (c *MemoryCollector) GetNumOfCreatedATXs(layer types.LayerID) int {
	c.lck.RLock()
	defer c.lck.RUnlock()
	return len(c.createdAtxs[uint64(layer)])
}

func (c *MemoryCollector) GetReceivedATXsNum(layer types.LayerID) int {
	c.lck.RLock()
	defer c.lck.RUnlock()
	return len(c.gotAtxEvent[uint64(layer)])
}

func (c *MemoryCollector) GetBlockCreationDone(layer types.LayerID) int {
	c.lck.RLock()
	defer c.lck.RUnlock()
	return len(c.doneCreatingBlockEvent[uint64(layer)])
}

func (c *MemoryCollector) GetNumOfCreatedBlocks(layer types.LayerID) int {
	c.lck.RLock()
	defer c.lck.RUnlock()
	created := 0
	for _, atx := range c.doneCreatingBlockEvent[uint64(layer)] {
		if atx.Eligible {
			created++
		}
	}
	return created
}

func (c *MemoryCollector) GetReceivedBlocks(layer types.LayerID) int {
	c.lck.RLock()
	defer c.lck.RUnlock()
	return len(c.gotBlockEvent[uint64(layer)])
}
