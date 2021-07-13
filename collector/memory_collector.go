package collector

import (
	"sync"

	"github.com/spacemeshos/go-spacemesh/common/types"
	"github.com/spacemeshos/go-spacemesh/events"
)

// MemoryCollector is an in memory database to store collected events
type MemoryCollector struct {
	events                 map[events.ChannelID][]Event
	doneCreatingBlockEvent map[uint64][]*events.DoneCreatingBlock
	doneCreatingAtxEvent   map[uint64][]*events.AtxCreated
	createdAtxs            map[uint64][]string
	gotBlockEvent          map[uint64][]*events.NewBlock
	gotAtxEvent            map[uint64][]*events.NewAtx
	Atxs                   map[string]uint64
	tortoiseBeacons        map[types.EpochID][]string

	lck sync.RWMutex
}

// NewMemoryCollector initializes a new memory collector that stores collected events
func NewMemoryCollector() *MemoryCollector {
	return &MemoryCollector{
		events:                 make(map[events.ChannelID][]Event),
		doneCreatingBlockEvent: make(map[uint64][]*events.DoneCreatingBlock),
		doneCreatingAtxEvent:   make(map[uint64][]*events.AtxCreated),
		gotBlockEvent:          make(map[uint64][]*events.NewBlock),
		gotAtxEvent:            make(map[uint64][]*events.NewAtx),
		Atxs:                   make(map[string]uint64),
		createdAtxs:            make(map[uint64][]string),
		tortoiseBeacons:        make(map[types.EpochID][]string),
	}
}

// Event defines global interface for all events to help identify their types, which is channel id
type Event interface {
	GetChannel() events.ChannelID
}

// StoreBlock stores block received event by layer id
func (c *MemoryCollector) StoreBlock(event *events.NewBlock) error {
	c.lck.Lock()
	c.events[event.GetChannel()] = append(c.events[event.GetChannel()], event)
	c.gotBlockEvent[event.Layer] = append(c.gotBlockEvent[event.Layer], event)
	c.lck.Unlock()
	return nil
}

// StoreBlockValid stores block validity event
func (c *MemoryCollector) StoreBlockValid(event *events.ValidBlock) error {
	/*c.lck.Lock()
	c.events[event.GetChannel()] = append(c.events[event.GetChannel()], event)
	c.lck.Unlock()*/
	return nil
}

// StoreTx stores tx received event, currently commented out because not used
func (c *MemoryCollector) StoreTx(event *events.NewTx) error {
	/*c.lck.Lock()
	c.events[event.GetChannel()] = append(c.events[event.GetChannel()], event)
	c.lck.Unlock()*/
	return nil
}

// StoreTxValid stored tx validity event. currently commented out because not used
func (c *MemoryCollector) StoreTxValid(event *events.ValidTx) error {
	/*c.lck.Lock()
	c.events[event.GetChannel()] = append(c.events[event.GetChannel()], event)
	c.lck.Unlock()*/
	return nil
}

// StoreAtx stores created ATX by epoch
func (c *MemoryCollector) StoreAtx(event *events.NewAtx) error {
	c.lck.Lock()
	c.events[event.GetChannel()] = append(c.events[event.GetChannel()], event)
	c.gotAtxEvent[event.LayerID] = append(c.gotAtxEvent[event.LayerID], event)
	c.Atxs[event.ID]++
	c.lck.Unlock()
	return nil
}

// StoreAtxValid stores an atx valididty event. not used thus commented out
func (c *MemoryCollector) StoreAtxValid(event *events.ValidAtx) error {
	/*c.lck.Lock()
	c.events[event.GetChannel()] = append(c.events[event.GetChannel()], event)
	c.lck.Unlock()*/
	return nil
}

// StoreReward should store reward received but for now is is commented out since not used
func (c *MemoryCollector) StoreReward(event *events.RewardReceived) error {
	/*c.lck.Lock()
	c.events[event.GetChannel()] = append(c.events[event.GetChannel()], event)
	c.lck.Unlock()*/
	return nil
}

// StoreBlockCreated stores block created events by layer
func (c *MemoryCollector) StoreBlockCreated(event *events.DoneCreatingBlock) error {
	c.lck.Lock()
	c.events[event.GetChannel()] = append(c.events[event.GetChannel()], event)
	c.doneCreatingBlockEvent[event.Layer] = append(c.doneCreatingBlockEvent[event.Layer], event)
	c.lck.Unlock()
	return nil
}

// StoreAtxCreated stores atx created events received by epoch
func (c *MemoryCollector) StoreAtxCreated(event *events.AtxCreated) error {
	c.lck.Lock()
	c.events[event.GetChannel()] = append(c.events[event.GetChannel()], event)
	c.doneCreatingAtxEvent[event.Layer] = append(c.doneCreatingAtxEvent[event.Layer], event)
	if event.Created {
		c.createdAtxs[event.Layer] = append(c.createdAtxs[event.Layer], event.ID)
	}
	c.lck.Unlock()
	return nil
}

// StoreTortoiseBeaconCalculated stores tortoise beacon calculated events received by epoch.
func (c *MemoryCollector) StoreTortoiseBeaconCalculated(event *events.TortoiseBeaconCalculated) error {
	c.lck.Lock()

	c.events[event.GetChannel()] = append(c.events[event.GetChannel()], event)
	c.tortoiseBeacons[event.Epoch] = append(c.tortoiseBeacons[event.Epoch], event.Beacon)

	c.lck.Unlock()

	return nil
}

// GetAtxCreationDone get number of atxs that were crated by this miner events for the provided epoch
func (c *MemoryCollector) GetAtxCreationDone(epoch types.EpochID) int {
	c.lck.RLock()
	defer c.lck.RUnlock()
	return len(c.doneCreatingAtxEvent[uint64(epoch)])
}

// GetCreatedAtx returns the number of created atx events received for the epoch
func (c *MemoryCollector) GetCreatedAtx(epoch types.EpochID) []string {
	c.lck.RLock()
	defer c.lck.RUnlock()
	return c.createdAtxs[uint64(epoch)]
}

// GetNumOfCreatedATXs returns numbe of created atxs per layer
func (c *MemoryCollector) GetNumOfCreatedATXs(layer types.LayerID) int {
	c.lck.RLock()
	defer c.lck.RUnlock()
	return len(c.createdAtxs[uint64(layer)])
}

// GetReceivedATXsNum returns the number of atx received events received for the provided layer
func (c *MemoryCollector) GetReceivedATXsNum(layer types.LayerID) int {
	c.lck.RLock()
	defer c.lck.RUnlock()
	return len(c.gotAtxEvent[uint64(layer)])
}

// GetBlockCreationDone returns number of blocks created events for the given layer
func (c *MemoryCollector) GetBlockCreationDone(layer types.LayerID) int {
	c.lck.RLock()
	defer c.lck.RUnlock()
	return len(c.doneCreatingBlockEvent[uint64(layer)])
}

// GetNumOfCreatedBlocks returns number of blocks created events received by this miner
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

// GetReceivedBlocks returns number of blocks received events received by this miner
func (c *MemoryCollector) GetReceivedBlocks(layer types.LayerID) int {
	c.lck.RLock()
	defer c.lck.RUnlock()
	return len(c.gotBlockEvent[uint64(layer)])
}

// AtxIDExists checks if an ATX with a certain ID exists.
func (c *MemoryCollector) AtxIDExists(atxID string) bool {
	c.lck.RLock()
	_, found := c.Atxs[atxID]
	c.lck.RUnlock()
	return found
}

// GetTortoiseBeacon returns a list of tortoise beacons for a given epoch.
func (c *MemoryCollector) GetTortoiseBeacon(epoch types.EpochID) []string {
	c.lck.RLock()
	defer c.lck.RUnlock()

	return c.tortoiseBeacons[epoch]
}
