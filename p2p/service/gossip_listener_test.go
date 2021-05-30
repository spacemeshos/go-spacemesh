package service

import (
	"context"
	"github.com/spacemeshos/go-spacemesh/common/types"
	"github.com/spacemeshos/go-spacemesh/log"
	"github.com/spacemeshos/go-spacemesh/priorityq"
	"github.com/stretchr/testify/assert"
	"sync"
	"sync/atomic"
	"testing"
)

type syncMock struct {
	Synced bool
}

func (m *syncMock) FetchBlock(ctx context.Context, ID types.BlockID) error {
	return nil
}

func (m *syncMock) FetchAtx(ctx context.Context, ID types.ATXID) error {
	return nil
}

func (m *syncMock) GetPoetProof(ctx context.Context, ID types.Hash32) error {
	return nil
}

func (m *syncMock) GetBlock(ID types.BlockID) error {
	return nil
}

func (m *syncMock) GetTxs(ctx context.Context, IDs []types.TransactionID) error {
	return nil
}

func (m *syncMock) GetBlocks(ctx context.Context, IDs []types.BlockID) error {
	return nil
}

func (m *syncMock) GetAtxs(ctx context.Context, IDs []types.ATXID) error {
	return nil
}

func (*syncMock) FetchAtxReferences(ctx context.Context, atx *types.ActivationTx) error {
	return nil
}

func (*syncMock) FetchPoetProof(ctx context.Context, poetProofRef []byte) error {
	panic("implement me")
}

func (*syncMock) ListenToGossip() bool {
	return true
}

func (*syncMock) IsSynced(context.Context) bool {
	return true
}

func Test_AddListener(t *testing.T) {
	net := NewSimulator()
	n1 := net.NewNode()
	l := NewListener(n1, &syncMock{true}, func() bool { return true }, log.NewDefault(n1.Info.ID.String()))

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
	assert.Equal(t, atomic.LoadInt32(&channelCount), int32(1))
	assert.Equal(t, atomic.LoadInt32(&secondChannel), int32(1))

	l.Stop()
}

func Test_AddListener_notSynced(t *testing.T) {
	net := NewSimulator()
	n1 := net.NewNode()
	l := NewListener(n1, &syncMock{false}, func() bool { return true }, log.NewDefault(n1.Info.ID.String()))

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

	assert.Equal(t, atomic.LoadInt32(&channelCount), int32(0))
	assert.Equal(t, atomic.LoadInt32(&secondChannel), int32(0))

	l.Stop()
}
