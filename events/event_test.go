package events

import (
	"github.com/spacemeshos/go-spacemesh/common/types"
	"github.com/spacemeshos/go-spacemesh/log"
	"github.com/spacemeshos/go-spacemesh/signing"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"sync"
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

	orig := NewBlock{Layer: 1, ID: "234"}
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

const (
	defaultGasLimit = 10
	defaultFee      = 1
)

var (
	addr1    = types.HexToAddress("33333")
	globalTx = MakeTx(1, addr1, signing.NewEdSigner())
)

func MakeTx(nonce uint64, recipient types.Address, signer *signing.EdSigner) *types.Transaction {
	tx, err := types.NewSignedTx(nonce, recipient, 1, defaultGasLimit, defaultFee, signer)
	if err != nil {
		log.Error("error creating new signed tx: %v", err)
		return nil
	}
	return tx
}

func TestEventReporter(t *testing.T) {
	// There should be no error reporting an event before initializing the reporter
	ReportNewTx(globalTx)

	// Stream is nil before we initialize it
	txStream := GetNewTxStream()
	require.Nil(t, txStream, "expected tx stream not to be initialized")

	InitializeEventReporter()
	txStream = GetNewTxStream()
	require.NotNil(t, txStream, "expected tx stream to be initialized")

	// This will not be received as no one is listening
	// This also makes sure that this call is nonblocking.
	ReportNewTx(globalTx)

	// listen on the channel
	wgListening := sync.WaitGroup{}
	wgListening.Add(1)
	wgDone := sync.WaitGroup{}
	wgDone.Add(1)
	go func() {
		defer wgDone.Done()
		// report that we're listening
		wgListening.Done()
		require.Equal(t, globalTx, <-txStream, "expected same input and output tx")
	}()

	// Wait until goroutine is listening
	wgListening.Wait()
	ReportNewTx(globalTx)

	// Wait for goroutine to finish
	wgDone.Wait()

	// This should also not cause an error
	CloseEventReporter()
	ReportNewTx(globalTx)
}
