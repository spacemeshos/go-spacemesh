package collector

import (
	"github.com/spacemeshos/go-spacemesh/events"
	"github.com/spacemeshos/go-spacemesh/log"
	"github.com/stretchr/testify/assert"
	"testing"
	"time"
)

type MockDb struct {
	msgs  map[byte]int
	total int
}

func (m *MockDb) StoreBlockCreated(event *events.DoneCreatingBlock) error {
	m.msgs[9]++
	m.total++
	return nil
}

func (m *MockDb) StoreAtxCreated(event *events.AtxCreated) error {
	m.msgs[8]++
	m.total++
	return nil
}

func (m *MockDb) StoreReward(event *events.RewardReceived) error {
	m.msgs[7]++
	m.total++
	return nil
}

func (m *MockDb) StoreBlock(event *events.NewBlock) error {
	m.msgs[1]++
	m.total++
	return nil
}

func (m *MockDb) StoreBlockValid(event *events.ValidBlock) error {
	m.msgs[2]++
	m.total++
	return nil
}

func (m *MockDb) StoreTx(event *events.NewTx) error {
	m.msgs[3]++
	m.total++
	return nil
}

func (m *MockDb) StoreTxValid(event *events.ValidTx) error {
	m.msgs[4]++
	m.total++
	return nil
}

func (m *MockDb) StoreAtx(event *events.NewAtx) error {
	m.msgs[5]++
	m.total++
	return nil
}

func (m *MockDb) StoreAtxValid(event *events.ValidAtx) error {
	m.msgs[6]++
	m.total++
	return nil
}

func TestCollectEvents(t *testing.T) {
	url := "tcp://localhost:56565"
	m := &MockDb{make(map[byte]int), 0}
	c := NewCollector(m, url)

	eventPublisher, err := events.NewEventPublisher(url)
	assert.NoError(t, err)
	defer func() {
		assert.NoError(t, eventPublisher.Close())
	}()

	c.Start(false)
	time.Sleep(2 * time.Second)
	for i := 0; i < 10010; i++ {
		orig := events.NewBlock{Layer: 1, ID: "234"}
		err = eventPublisher.PublishEvent(orig)
	}

	orig1 := events.ValidBlock{ID: "234", Valid: true}
	err = eventPublisher.PublishEvent(orig1)

	orig2 := events.NewAtx{ID: "1234"}
	err = eventPublisher.PublishEvent(orig2)

	orig3 := events.ValidAtx{ID: "1234", Valid: true}
	err = eventPublisher.PublishEvent(orig3)

	orig4 := events.NewTx{ID: "4321", Amount: 400, Destination: "1234567", Origin: "876543"}
	err = eventPublisher.PublishEvent(orig4)

	orig5 := events.ValidTx{ID: "4321", Valid: true}
	err = eventPublisher.PublishEvent(orig5)

	time.Sleep(1 * time.Second)
	c.Stop()

	log.Info("got %v", len(m.msgs))
	assert.Equal(t, 10015, m.total)
	assert.Equal(t, 6, len(m.msgs))
}
