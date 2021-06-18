package service

import (
	"context"
	"github.com/spacemeshos/go-spacemesh/common/types"
	"github.com/spacemeshos/go-spacemesh/log"
	"github.com/spacemeshos/go-spacemesh/p2p/config"
	"github.com/spacemeshos/go-spacemesh/priorityq"
	"github.com/stretchr/testify/assert"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

type syncMock struct {
	Synced           bool
	listenToGossipFn func() bool
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

func (sm *syncMock) GetBlock(types.BlockID) error {
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

func (*syncMock) FetchAtxReferences(context.Context, *types.ActivationTx) error {
	return nil
}

func (*syncMock) FetchPoetProof(context.Context, []byte) error {
	panic("implement me")
}

func (sm *syncMock) ListenToGossip() bool {
	if sm.listenToGossipFn != nil {
		return sm.listenToGossipFn()
	}
	return true
}

func (*syncMock) IsSynced(context.Context) bool {
	return true
}

func Test_AddListener(t *testing.T) {
	net := NewSimulator()
	n1 := net.NewNode()
	l := NewListener(n1, &syncMock{Synced: true}, func() bool { return true }, config.DefaultConfig(), log.NewDefault(n1.Info.ID.String()))
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
	l := NewListener(n1, &syncMock{Synced: false}, func() bool { return true }, config.DefaultConfig(), log.NewDefault(n1.Info.ID.String()))
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
	listenFn := func() bool {
		atomic.AddInt32(&listenCount, 1)
		return true
	}
	conf := config.DefaultTestConfig()
	l := NewListener(n1, &syncMock{true, listenFn}, func() bool { return true }, conf, log.NewDefault(n1.Info.ID.String()))
	defer l.Stop()

	var channelCount int32

	releaseChan := make(chan struct{})
	handlerFn := func(ctx context.Context, data GossipMessage, syncer Fetcher) {
		<-releaseChan
		atomic.AddInt32(&channelCount, 1)
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
	assert.Eventually(t, checkVal(2), time.Second, 10*time.Millisecond)

	// release one handler
	releaseChan <- struct{}{}
	assert.Eventually(t, checkVal(3), time.Second, 10*time.Millisecond)
	releaseChan <- struct{}{}
	assert.Eventually(t, checkVal(4), time.Second, 10*time.Millisecond)
	releaseChan <- struct{}{}
	assert.Eventually(t, checkVal(4), time.Second, 10*time.Millisecond)
	releaseChan <- struct{}{}
	assert.Eventually(t, checkVal(4), time.Second, 10*time.Millisecond)
}
