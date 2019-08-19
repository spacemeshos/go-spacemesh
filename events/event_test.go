package events

import (
	"github.com/spacemeshos/go-spacemesh/types"
	"github.com/stretchr/testify/assert"
	"testing"
	"time"
)

func TestNewBlockEvent(t *testing.T){
	url := "tcp://localhost:56565"

	eventPublisher, err := newEventPublisher(url)
	assert.NoError(t, err)
	defer func() {
		assert.NoError(t, eventPublisher.Close())
	}()

	s, err := newSubscriber(url)
	assert.NoError(t, err)
	defer func() {
		assert.NoError(t, s.close())
	}()
	c, err := s.subscribe(NewBlock)
	assert.NoError(t, err)
	s.startListening()
	time.Sleep(5 * time.Second)

	orig := NewBlockEvent{Layer:1, Block:234}
	err = eventPublisher.PublishEvent(orig)
	assert.NoError(t, err)

	tm := time.NewTimer(7 * time.Second)

	select {
	case <-tm.C:
		assert.Fail(t, "didnt receive message")
	case rec := <-c:
		e := NewBlockEvent{}

		err := types.BytesToInterface(rec[1:], &e)
		assert.NoError(t, err)

		assert.Equal(t, orig,e)
	}


}