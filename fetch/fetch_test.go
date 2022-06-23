package fetch

import (
	"context"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/spacemeshos/go-spacemesh/common/types"
	"github.com/spacemeshos/go-spacemesh/datastore"
	ftypes "github.com/spacemeshos/go-spacemesh/fetch/types"
	"github.com/spacemeshos/go-spacemesh/log/logtest"
	"github.com/spacemeshos/go-spacemesh/p2p"
	"github.com/spacemeshos/go-spacemesh/rand"
	"github.com/spacemeshos/go-spacemesh/sql"
)

func randomHash() (hash types.Hash32) {
	hash.SetBytes([]byte(rand.String(32)))
	return
}

type mockNet struct {
	Mu              sync.RWMutex
	SendCalled      map[types.Hash32]int
	TotalBatchCalls int
	ReturnError     bool
	Responses       map[types.Hash32]responseMessage
	AckChannel      chan struct{}
	AsyncChannel    chan struct{}
}

func (m *mockNet) GetPeers() []p2p.Peer {
	return []p2p.Peer{"test"}
}

func (m *mockNet) PeerCount() uint64 {
	return 1
}

func (m *mockNet) Close() error {
	return nil
}

func (m *mockNet) Request(_ context.Context, _ p2p.Peer, payload []byte, resHandler func(msg []byte), _ func(err error)) error {
	m.Mu.Lock()
	m.TotalBatchCalls++
	retErr := m.ReturnError
	m.Mu.Unlock()

	if retErr {
		m.Mu.RLock()
		ackCh := m.AckChannel
		m.Mu.RUnlock()

		if ackCh != nil {
			ackCh <- struct{}{}
		}
		return fmt.Errorf("mock error")
	}

	var r requestBatch
	err := types.BytesToInterface(payload, &r)
	if err != nil {
		panic("invalid message")
	}

	var res responseBatch
	for _, req := range r.Requests {
		m.Mu.Lock()
		m.SendCalled[req.Hash]++
		if r, ok := m.Responses[req.Hash]; ok {
			res.Responses = append(res.Responses, r)
		}
		m.Mu.Unlock()
	}
	res.ID = r.ID
	bts, _ := types.InterfaceToBytes(res)

	m.Mu.RLock()
	asyncCh := m.AsyncChannel
	m.Mu.RUnlock()

	if asyncCh != nil {
		go func(data []byte) {
			<-asyncCh
			resHandler(data)
		}(bts)
	} else {
		resHandler(bts)
	}

	m.Mu.RLock()
	ackCh := m.AckChannel
	m.Mu.RUnlock()

	if ackCh != nil {
		ackCh <- struct{}{}
	}
	return nil
}

func defaultFetch(tb testing.TB) (*Fetch, *mockNet) {
	cfg := Config{
		2000, // make sure we never hit the batch timeout
		3,
		3,
		3,
		3,
	}

	mckNet := &mockNet{
		SendCalled: make(map[types.Hash32]int),
		Responses:  make(map[types.Hash32]responseMessage),
	}
	lg := logtest.New(tb)
	f := NewFetch(cfg, nil, datastore.NewBlobStore(sql.InMemory()), lg)
	f.net = mckNet

	return f, mckNet
}

func customFetch(tb testing.TB, cfg Config) (*Fetch, *mockNet) {
	mckNet := &mockNet{
		SendCalled: make(map[types.Hash32]int),
		Responses:  make(map[types.Hash32]responseMessage),
	}
	lg := logtest.New(tb)
	f := NewFetch(cfg, nil, datastore.NewBlobStore(sql.InMemory()), lg)
	f.net = mckNet
	return f, mckNet
}

func TestFetch_GetHash(t *testing.T) {
	f, _ := defaultFetch(t)
	defer f.Stop()
	f.Start()
	h1 := randomHash()
	hint := datastore.POETDB
	hint2 := datastore.BallotDB

	// test hash aggregation
	f.GetHash(h1, hint, false)
	f.GetHash(h1, hint, false)

	h2 := randomHash()
	f.GetHash(h2, hint2, false)

	// test aggregation by hint
	f.activeReqM.RLock()
	assert.Equal(t, 2, len(f.activeRequests[h1]))
	f.activeReqM.RUnlock()
}

func TestFetch_requestHashBatchFromPeers_AggregateAndValidate(t *testing.T) {
	h1 := randomHash()
	f, net := defaultFetch(t)

	// set response mock
	res := responseMessage{
		Hash: h1,
		Data: []byte("a"),
	}

	net.Mu.Lock()
	net.Responses[h1] = res
	net.Mu.Unlock()

	request1 := request{
		hash:                 h1,
		validateResponseHash: false,
		hint:                 datastore.POETDB,
		returnChan:           make(chan ftypes.HashDataPromiseResult, 6),
	}

	f.activeRequests[h1] = []*request{&request1, &request1, &request1}
	f.requestHashBatchFromPeers()

	net.Mu.RLock()
	// test aggregation of messages before calling fetch from peer
	assert.Equal(t, 1, net.SendCalled[h1])
	net.Mu.RUnlock()

	// test incorrect hash fail
	request1.validateResponseHash = true
	f.activeRequests[h1] = []*request{&request1, &request1, &request1}
	f.requestHashBatchFromPeers()

	close(request1.returnChan)
	okCount, notOk := 0, 0
	for x := range request1.returnChan {
		if x.Err != nil {
			notOk++
		} else {
			okCount++
		}
	}

	net.Mu.RLock()
	assert.Equal(t, 2, net.SendCalled[h1])
	net.Mu.RUnlock()
	assert.Equal(t, 3, notOk)
	assert.Equal(t, 3, okCount)
}

func TestFetch_requestHashBatchFromPeers_NoDuplicates(t *testing.T) {
	h1 := randomHash()
	f, net := defaultFetch(t)

	// set response mock
	res := responseMessage{
		Hash: h1,
		Data: []byte("a"),
	}

	net.Mu.Lock()
	net.Responses[h1] = res
	net.AsyncChannel = make(chan struct{})
	net.Mu.Unlock()

	request1 := request{
		hash:                 h1,
		validateResponseHash: false,
		hint:                 datastore.POETDB,
		returnChan:           make(chan ftypes.HashDataPromiseResult, 3),
	}

	f.activeRequests[h1] = []*request{&request1, &request1, &request1}
	f.requestHashBatchFromPeers()
	f.requestHashBatchFromPeers()

	net.Mu.RLock()
	assert.Equal(t, 1, net.SendCalled[h1])
	asyncCh := net.AsyncChannel
	net.Mu.RUnlock()

	close(asyncCh)
}

func TestFetch_GetHash_StartStopSanity(t *testing.T) {
	f, _ := defaultFetch(t)
	f.Start()
	f.Stop()
}

func TestFetch_GetHash_failNetwork(t *testing.T) {
	h1 := randomHash()
	f, net := defaultFetch(t)

	// set response mock
	bts := responseMessage{
		Hash: h1,
		Data: []byte("a"),
	}

	net.Mu.Lock()
	net.Responses[h1] = bts
	net.ReturnError = true
	net.Mu.Unlock()

	request1 := request{
		hash:                 h1,
		validateResponseHash: false,
		hint:                 datastore.POETDB,
		returnChan:           make(chan ftypes.HashDataPromiseResult, f.cfg.MaxRetriesForPeer),
	}
	f.activeRequests[h1] = []*request{&request1, &request1, &request1}
	f.requestHashBatchFromPeers()

	// test aggregation of messages before calling fetch from peer
	assert.Equal(t, 3, len(request1.returnChan))
	net.Mu.RLock()
	assert.Equal(t, 0, net.SendCalled[h1])
	net.Mu.RUnlock()
}

func TestFetch_Loop_BatchRequestMax(t *testing.T) {
	h1 := randomHash()
	h2 := randomHash()
	h3 := randomHash()
	f, net := customFetch(t, Config{
		BatchTimeout:      1,
		MaxRetriesForPeer: 2,
		BatchSize:         2,
	})

	// set response mock
	bts := responseMessage{
		Hash: h1,
		Data: []byte("a"),
	}
	bts2 := responseMessage{
		Hash: h2,
		Data: []byte("a"),
	}
	bts3 := responseMessage{
		Hash: h3,
		Data: []byte("a"),
	}

	net.Mu.Lock()
	net.Responses[h1] = bts
	net.Responses[h2] = bts2
	net.Responses[h3] = bts3
	net.AckChannel = make(chan struct{})
	net.Mu.Unlock()

	hint := datastore.POETDB

	defer f.Stop()
	f.Start()
	// test hash aggregation
	r1 := f.GetHash(h1, hint, false)
	r2 := f.GetHash(h2, hint, false)
	r3 := f.GetHash(h3, hint, false)

	// since we have a batch of 2 we should call send twice - of not we should fail
	net.Mu.RLock()
	ackCh := net.AckChannel
	net.Mu.RUnlock()

	select {
	case <-ackCh:
		break
	case <-time.After(2 * time.Second):
		assert.Fail(t, "timeout getting")
	}

	net.Mu.RLock()
	ackCh = net.AckChannel
	net.Mu.RUnlock()

	select {
	case <-ackCh:
		break
	case <-time.After(2 * time.Second):
		assert.Fail(t, "timeout getting")
	}

	responses := []chan ftypes.HashDataPromiseResult{r1, r2, r3}
	for _, res := range responses {
		select {
		case data := <-res:
			assert.NoError(t, data.Err)
		case <-time.After(1 * time.Second):
			assert.Fail(t, "timeout getting")
		}
	}

	// test aggregation of messages before calling fetch from peer
	net.Mu.RLock()
	assert.Equal(t, 1, net.SendCalled[h1])
	assert.Equal(t, 1, net.SendCalled[h2])
	assert.Equal(t, 1, net.SendCalled[h3])
	assert.Equal(t, 2, net.TotalBatchCalls)
	net.Mu.RUnlock()
}

func TestFetch_GetRandomPeer(t *testing.T) {
	myPeers := make([]p2p.Peer, 1000)
	for i := 0; i < len(myPeers); i++ {
		buf := make([]byte, 20)
		_, err := rand.Read(buf)
		require.NoError(t, err)
		myPeers[i] = p2p.Peer(buf)
	}
	allTheSame := true
	for i := 0; i < 20; i++ {
		peer1 := GetRandomPeer(myPeers)
		peer2 := GetRandomPeer(myPeers)
		if peer1 != peer2 {
			allTheSame = false
		}
	}
	assert.False(t, allTheSame)
}
