package service

import (
	"context"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/spacemeshos/go-spacemesh/common/types"
	"github.com/spacemeshos/go-spacemesh/log/logtest"
	"github.com/spacemeshos/go-spacemesh/p2p/config"
	"github.com/spacemeshos/go-spacemesh/priorityq"
	"github.com/stretchr/testify/assert"
)

type syncMock struct {
}

func (sm *syncMock) FetchBlock(context.Context, types.BlockID) error {
	return nil
}

func (sm *syncMock) FetchAtx(context.Context, types.ATXID) error {
	return nil
}

func (sm *syncMock) GetPoetProof(context.Context, types.Hash32) error {
	return nil
}

func (sm *syncMock) GetTxs(context.Context, []types.TransactionID) error {
	return nil
}

func (sm *syncMock) GetBlocks(context.Context, []types.BlockID) error {
	return nil
}

func (sm *syncMock) GetAtxs(context.Context, []types.ATXID) error {
	return nil
}

func Test_AddListener(t *testing.T) {
	net := NewSimulator()
	n1 := net.NewNode()
	l := NewListener(n1, &syncMock{}, func() bool { return true }, config.DefaultConfig(), logtest.New(t).WithName(n1.Info.ID.String()))
	defer l.Stop()

	var channelCount, secondChannel int32
	wg := sync.WaitGroup{}

	wg.Add(2)
	fun := func(ctx context.Context, data GossipMessage, syncer Fetcher) {
		atomic.AddInt32(&channelCount, 1)
		wg.Done()
	}

	fun2 := func(ctx context.Context, data GossipMessage, syncer Fetcher) {
		atomic.AddInt32(&secondChannel, 1)
		wg.Done()
	}

	l.AddListener(context.TODO(), "channel1", priorityq.Mid, fun)
	l.AddListener(context.TODO(), "channel2", priorityq.Mid, fun2)

	assert.NoError(t, n1.Broadcast(context.TODO(), "channel1", []byte{}))
	assert.NoError(t, n1.Broadcast(context.TODO(), "channel2", []byte{}))

	wg.Wait()
	assert.Equal(t, int32(1), atomic.LoadInt32(&channelCount))
	assert.Equal(t, int32(1), atomic.LoadInt32(&secondChannel))
}

func Test_AddListener_notSynced(t *testing.T) {
	net := NewSimulator()
	n1 := net.NewNode()
	l := NewListener(n1, &syncMock{}, func() bool { return true }, config.DefaultConfig(), logtest.New(t).WithName(n1.Info.ID.String()))
	defer l.Stop()

	var channelCount, secondChannel int32

	fun := func(ctx context.Context, data GossipMessage, syncer Fetcher) {
		atomic.AddInt32(&channelCount, 1)
	}

	fun2 := func(ctx context.Context, data GossipMessage, syncer Fetcher) {
		atomic.AddInt32(&secondChannel, 1)
	}

	l.AddListener(context.TODO(), "channel1", priorityq.Mid, fun)
	l.AddListener(context.TODO(), "channel2", priorityq.Mid, fun2)

	assert.NoError(t, n1.Broadcast(context.TODO(), "channel1", []byte{}))
	assert.NoError(t, n1.Broadcast(context.TODO(), "channel2", []byte{}))

	assert.Equal(t, int32(0), atomic.LoadInt32(&channelCount))
	assert.Equal(t, int32(0), atomic.LoadInt32(&secondChannel))
}

func TestListenerConcurrency(t *testing.T) {
	net := NewSimulator()
	n1 := net.NewNode()
	var listenCount int32
	conf := config.DefaultTestConfig()
	listenFn := func() bool {
		atomic.AddInt32(&listenCount, 1)
		return true
	}
	l := NewListener(n1, &syncMock{}, listenFn, conf, logtest.New(t).WithName(n1.Info.ID.String()))
	defer l.Stop()

	releaseChan := make(chan struct{})
	handlerFn := func(ctx context.Context, data GossipMessage, syncer Fetcher) {
		<-releaseChan
	}

	l.AddListener(context.TODO(), "channel1", priorityq.Mid, handlerFn)

	assert.Equal(t, 2, conf.MaxGossipRoutines)

	// broadcast several messages. expect the first two to call a handler, then the rest should block.
	assert.NoError(t, n1.Broadcast(context.TODO(), "channel1", []byte{}))
	assert.NoError(t, n1.Broadcast(context.TODO(), "channel1", []byte{}))
	assert.NoError(t, n1.Broadcast(context.TODO(), "channel1", []byte{}))
	assert.NoError(t, n1.Broadcast(context.TODO(), "channel1", []byte{}))

	checkVal := func(expectedVal int32) func() bool {
		return func() bool {
			return atomic.LoadInt32(&listenCount) == expectedVal
		}
	}

	// two processed completely (two checks each), one processed and blocking
	assert.Eventually(t, checkVal(5), time.Second, 10*time.Millisecond)

	// release one handler
	releaseChan <- struct{}{}
	// unblocked one checks again, another checks and is blocked
	assert.Eventually(t, checkVal(7), time.Second, 10*time.Millisecond)
	releaseChan <- struct{}{}
	// unblocked one checks again, no more blocking
	assert.Eventually(t, checkVal(8), time.Second, 10*time.Millisecond)
	releaseChan <- struct{}{}
	assert.Eventually(t, checkVal(8), time.Second, 10*time.Millisecond)
	releaseChan <- struct{}{}
	assert.Eventually(t, checkVal(8), time.Second, 10*time.Millisecond)
}
