package events

import (
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"

	"github.com/spacemeshos/go-spacemesh/common/types"
	"github.com/spacemeshos/go-spacemesh/log"
	"github.com/spacemeshos/go-spacemesh/log/logtest"
	"github.com/spacemeshos/go-spacemesh/signing"
)

func TestNewBlockEvent(t *testing.T) {
	url := "tcp://localhost:12321"

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
		return nil
	}
	return tx
}

func TestEventReporter(t *testing.T) {
	// There should be no error reporting an event before initializing the reporter
	ReportNewTx(types.LayerID{}, globalTx)

	// Stream is nil before we initialize it
	txStream := GetNewTxChannel()
	require.Nil(t, txStream, "expected tx stream not to be initialized")

	err := InitializeEventReporter("")
	require.NoError(t, err)
	txStream = GetNewTxChannel()
	require.NotNil(t, txStream, "expected tx stream to be initialized")

	// This will not be received as no one is listening
	// This also makes sure that this call is nonblocking.
	ReportNewTx(types.LayerID{}, globalTx)

	// listen on the channel
	wgListening := sync.WaitGroup{}
	wgListening.Add(1)
	wgDone := sync.WaitGroup{}
	wgDone.Add(1)
	go func() {
		defer wgDone.Done()
		// report that we're listening
		wgListening.Done()
		txWithValidity := <-txStream
		require.Equal(t, globalTx, txWithValidity.Transaction, "expected same input and output tx")
	}()

	// Wait until goroutine is listening
	wgListening.Wait()
	ReportNewTx(types.LayerID{}, globalTx)

	// Wait for goroutine to finish
	wgDone.Wait()

	// This should also not cause an error
	CloseEventReporter()
	ReportNewTx(types.LayerID{}, globalTx)
}

func TestReportError(t *testing.T) {
	logger := logtest.New(t, zap.ErrorLevel)

	nodeErr := NodeError{
		Msg:   "hi there",
		Trace: "<trace goes here>",
		Level: zapcore.ErrorLevel,
	}

	// There should be no error reporting an event before initializing the reporter
	ReportError(nodeErr)

	// Stream is nil before we initialize it
	stream := GetErrorChannel()
	require.Nil(t, stream, "expected stream not to be initialized")

	err := InitializeEventReporterWithOptions("", 1, false)
	require.NoError(t, err)
	stream = GetErrorChannel()
	require.NotNil(t, stream, "expected stream to be initialized")

	// This one will be buffered
	// This also makes sure that this call is nonblocking.
	ReportError(nodeErr)

	// listen on the channel
	wgListening := sync.WaitGroup{}
	wgListening.Add(1)
	wgDone := sync.WaitGroup{}
	wgDone.Add(1)
	errMsg := "abracadabra"
	go func() {
		defer wgDone.Done()
		// report that we're listening
		wgListening.Done()

		// check the error sent directly
		require.Equal(t, nodeErr, <-stream, "expected same input and output tx")
		require.Equal(t, nodeErr, <-stream, "expected same input and output tx")

		// now check errors sent through logging
		msg := <-stream
		require.Equal(t, zapcore.ErrorLevel, msg.Level)
		require.Equal(t, errMsg, msg.Msg)
	}()

	// Wait until goroutine is listening
	wgListening.Wait()
	ReportError(nodeErr)

	// Try reporting using log
	logger = log.RegisterHooks(logger, EventHook())
	logger.Error(errMsg)

	// Wait for goroutine to finish
	wgDone.Wait()

	// This should also not cause an error
	CloseEventReporter()
	ReportError(nodeErr)
}

func TestReportNodeStatus(t *testing.T) {
	// There should be no error reporting an event before initializing the reporter
	ReportNodeStatusUpdate()

	// Stream is nil before we initialize it
	stream := GetStatusChannel()
	require.Nil(t, stream, "expected stream not to be initialized")

	err := InitializeEventReporter("")
	require.NoError(t, err)
	stream = GetStatusChannel()
	require.NotNil(t, stream, "expected stream to be initialized")

	// This will not be received as no one is listening
	// This also makes sure that this call is nonblocking.
	ReportNodeStatusUpdate()

	// listen on the channel
	commChannel := make(chan struct{}, 1)
	go func() {
		// report that we're listening
		commChannel <- struct{}{}

		status := <-stream
		require.Equal(t, struct{}{}, status)

		close(commChannel)
	}()

	// Wait until goroutine is listening
	<-commChannel
	ReportNodeStatusUpdate()

	// Wait for goroutine to finish
	<-commChannel

	// This should also not cause an error
	CloseEventReporter()
	ReportNodeStatusUpdate()
}
