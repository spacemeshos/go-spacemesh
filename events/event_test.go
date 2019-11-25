package events

import (
	"github.com/spacemeshos/go-spacemesh/common/types"
	"github.com/stretchr/testify/assert"
	"testing"
	"time"
)

func TestNewBlockEvent(t *testing.T) {
	url := "tcp://localhost:56565"

	eventPublisher, err := NewEventPublisher(url)
	assert.NoError(t, err)
	defer func() {
		assert.NoError(t, eventPublisher.Close())
	}()

	s, err := NewSubscriber(url)
	assert.NoError(t, err)
	defer func() {
		assert.NoError(t, s.Close())
	}()
	c, err := s.Subscribe(EventNewBlock)
	assert.NoError(t, err)
	s.StartListening()
	time.Sleep(5 * time.Second)

	orig := NewBlock{Layer: 1, Id: "234"}
	err = eventPublisher.PublishEvent(orig)
	assert.NoError(t, err)

	tm := time.NewTimer(7 * time.Second)

	select {
	case <-tm.C:
		assert.Fail(t, "didnt receive message")
	case rec := <-c:
		e := NewBlock{}

		err := types.BytesToInterface(rec[1:], &e)
		assert.NoError(t, err)

		assert.Equal(t, orig, e)
	}

}
