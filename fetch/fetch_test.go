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
	"github.com/spacemeshos/go-spacemesh/database"
	"github.com/spacemeshos/go-spacemesh/log/logtest"
	"github.com/spacemeshos/go-spacemesh/p2p"
	"github.com/spacemeshos/go-spacemesh/rand"
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

func (m *mockNet) Request(_ context.Context, pid p2p.Peer, payload []byte, resHandler func(msg []byte), failHandler func(err error)) error {
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
	dbs := LocalDataSource{"db": database.NewMemDatabase(), "db2": database.NewMemDatabase()}
	f := NewFetch(context.TODO(), cfg, nil, dbs, lg)
	f.net = mckNet

	return f, mckNet
}

func customFetch(tb testing.TB, cfg Config) (*Fetch, *mockNet) {
	mckNet := &mockNet{
		SendCalled: make(map[types.Hash32]int),
		Responses:  make(map[types.Hash32]responseMessage),
	}
	lg := logtest.New(tb)
	f := NewFetch(context.TODO(), cfg, nil, LocalDataSource{"db": database.NewMemDatabase()}, lg)
	f.net = mckNet
	return f, mckNet
}

func TestFetch_GetHash(t *testing.T) {
	f, _ := defaultFetch(t)
	defer f.Stop()
	f.Start()
	h1 := randomHash()
	hint := Hint("db")
	hint2 := Hint("db2")

	const testPeer = "test_peer"

	// test hash aggregation
	f.GetHash(testPeer, h1, hint, false)
	f.GetHash(testPeer, h1, hint, false)

	h2 := randomHash()
	f.GetHash(testPeer, h2, hint2, false)

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

	hint := Hint("db")
	request1 := request{
		hash:                 h1,
		priority:             0,
		validateResponseHash: false,
		hint:                 hint,
		returnChan:           make(chan HashDataPromiseResult, 6),
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

	hint := Hint("db")
	request1 := request{
		hash:                 h1,
		priority:             0,
		validateResponseHash: false,
		hint:                 hint,
		returnChan:           make(chan HashDataPromiseResult, 3),
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

	hint := Hint("db")
	request1 := request{
		hash:                 h1,
		priority:             0,
		validateResponseHash: false,
		hint:                 hint,
		returnChan:           make(chan HashDataPromiseResult, f.cfg.MaxRetriesForPeer),
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

	hint := Hint("db")

	defer f.Stop()
	f.Start()
	// test hash aggregation
	const testPeer = "test_peer"
	r1 := f.GetHash(testPeer, h1, hint, false)
	r2 := f.GetHash(testPeer, h2, hint, false)
	r3 := f.GetHash(testPeer, h3, hint, false)

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

	responses := []chan HashDataPromiseResult{r1, r2, r3}
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

func makeRequest(h types.Hash32, p priority, hint Hint) *request {
	return &request{
		hash:                 h,
		priority:             p,
		validateResponseHash: false,
		hint:                 hint,
		returnChan:           make(chan HashDataPromiseResult, 1),
	}
}

func TestFetch_handleNewRequest_MultipleReqsForSameHashHighPriority(t *testing.T) {
	hint := Hint("db")
	hash1 := randomHash()
	req1 := makeRequest(hash1, High, hint)
	req2 := makeRequest(hash1, High, hint)
	hash3 := randomHash()
	req3 := makeRequest(hash3, High, hint)
	req4 := makeRequest(hash3, Low, hint)
	req5 := makeRequest(hash3, High, hint)
	f, net := customFetch(t, Config{
		BatchTimeout:      1,
		MaxRetriesForPeer: 2,
		BatchSize:         2,
	})

	// set response
	resp1 := responseMessage{
		Hash: hash1,
		Data: []byte("a"),
	}
	resp3 := responseMessage{
		Hash: hash3,
		Data: []byte("d"),
	}

	net.Mu.Lock()
	net.Responses[hash1] = resp1
	net.Responses[req3.hash] = resp3
	net.AckChannel = make(chan struct{}, 2)
	net.AsyncChannel = make(chan struct{}, 2)
	net.Mu.Unlock()

	// req1 is high priority and will cause a send right away
	assert.True(t, f.handleNewRequest(req1))

	net.Mu.RLock()
	ackCh := net.AckChannel
	net.Mu.RUnlock()

	select {
	case <-ackCh:
		break
	case <-time.After(2 * time.Second):
		assert.Fail(t, "timeout sending req1")
	}

	net.Mu.RLock()
	assert.Equal(t, 1, net.SendCalled[hash1])
	net.Mu.RUnlock()

	// each high priority request should cause a send immediately, but because req1 has not received response yet
	// req2 will not cause another send and will be notified after req1 receives a response.
	assert.False(t, f.handleNewRequest(req2))

	net.Mu.RLock()
	assert.Equal(t, 1, net.SendCalled[hash1])
	net.Mu.RUnlock()

	// req3 is high priority and has a different hash. it causes a send right away
	assert.True(t, f.handleNewRequest(req3))

	net.Mu.RLock()
	ackCh = net.AckChannel
	net.Mu.RUnlock()

	select {
	case <-ackCh:
		break
	case <-time.After(2 * time.Second):
		assert.Fail(t, "timeout sending req3")
	}

	net.Mu.RLock()
	assert.Equal(t, 1, net.SendCalled[req3.hash])
	net.Mu.RUnlock()

	// req4 is the same hash as req3. it won't cause a send
	assert.False(t, f.handleNewRequest(req4))

	net.Mu.RLock()
	assert.Equal(t, 1, net.SendCalled[req3.hash])
	net.Mu.RUnlock()

	// req5 is high priority, but has the same hash as req3h. it won't cause a send either
	assert.False(t, f.handleNewRequest(req5))

	net.Mu.RLock()
	assert.Equal(t, 1, net.SendCalled[req3.hash])
	net.Mu.RUnlock()

	// let both hashes receives response
	net.Mu.RLock()
	asyncCh := net.AsyncChannel
	net.Mu.RUnlock()

	asyncCh <- struct{}{}

	net.Mu.RLock()
	asyncCh = net.AsyncChannel
	net.Mu.RUnlock()

	asyncCh <- struct{}{}

	for i, req := range []*request{req1, req2, req3, req4, req5} {
		select {
		case resp := <-req.returnChan:
			if i < 2 {
				assert.Equal(t, resp1.Data, resp.Data)
			} else {
				assert.Equal(t, resp3.Data, resp.Data)
			}
		case <-time.After(2 * time.Second):
			assert.Fail(t, "timeout getting resp for %v", req)
		}
	}

	net.Mu.RLock()
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
