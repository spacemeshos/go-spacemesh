package fetch

import (
	"fmt"
	"testing"
	"time"

	"github.com/spacemeshos/go-spacemesh/common/types"
	"github.com/spacemeshos/go-spacemesh/database"
	"github.com/spacemeshos/go-spacemesh/log"
	"github.com/spacemeshos/go-spacemesh/p2p/p2pcrypto"
	p2ppeers "github.com/spacemeshos/go-spacemesh/p2p/peers"
	"github.com/spacemeshos/go-spacemesh/p2p/server"
	"github.com/spacemeshos/go-spacemesh/p2p/service"
	"github.com/spacemeshos/go-spacemesh/priorityq"
	"github.com/spacemeshos/go-spacemesh/rand"
	"github.com/stretchr/testify/assert"
)

func randomHash() (hash types.Hash32) {
	hash.SetBytes([]byte(rand.String(32)))
	return
}

func newRequestMock() *mockRequest {
	return &mockRequest{
		OkCalled:     make(map[types.Hash32][]byte),
		OkCalledNum:  make(map[types.Hash32]int),
		ErrCalled:    make(map[types.Hash32]error),
		ErrCalledNum: make(map[types.Hash32]int),
	}
}

type mockRequest struct {
	OkCalled     map[types.Hash32][]byte
	ErrCalled    map[types.Hash32]error
	OkCalledNum  map[types.Hash32]int
	ErrCalledNum map[types.Hash32]int
}

func (m *mockRequest) OkCallback(hash types.Hash32, buf []byte) error {
	m.OkCalled[hash] = buf
	m.OkCalledNum[hash]++
	return nil
}

func (m *mockRequest) ErrCallback(hash types.Hash32, err error) {
	m.ErrCalled[hash] = err
	m.ErrCalledNum[hash]++
}

type mockNet struct {
	SendCalled  map[types.Hash32]int
	ReturnError bool
	Responses   map[types.Hash32]responseMessage
	SendAck     bool
	AckChannel  chan struct{}
}

func (m mockNet) RegisterBytesMsgHandler(msgType server.MessageType, reqHandler func([]byte) []byte) {
}

func (m mockNet) GetRandomPeer() p2ppeers.Peer {
	_, pub1, _ := p2pcrypto.GenerateKeyPair()
	return pub1
}

func (m mockNet) Start() error {
	return nil
}

func (m mockNet) RegisterGossipProtocol(protocol string, prio priorityq.Priority) chan service.GossipMessage {
	return nil
}

func (m mockNet) RegisterDirectProtocol(protocol string) chan service.DirectMessage {
	return nil
}

func (m mockNet) GossipReady() <-chan struct{} {
	c := make(chan struct{})
	close(c)
	return c
}

func (m mockNet) SubscribePeerEvents() (new chan p2pcrypto.PublicKey, del chan p2pcrypto.PublicKey) {
	return nil, nil
}

func (m mockNet) Broadcast(protocol string, payload []byte) error {
	return nil
}

func (m mockNet) Shutdown() {
}

func (m mockNet) RegisterDirectProtocolWithChannel(protocol string, ingressChannel chan service.DirectMessage) chan service.DirectMessage {
	return nil
}

func (m mockNet) SendWrappedMessage(nodeID p2pcrypto.PublicKey, protocol string, payload *service.DataMsgWrapper) error {
	return nil
}

func (m mockNet) GetPeers() []p2ppeers.Peer {
	_, pub1, _ := p2pcrypto.GenerateKeyPair()
	return []p2ppeers.Peer{pub1}
}

func (m *mockNet) SendRequest(msgType server.MessageType, payload []byte, address p2pcrypto.PublicKey, resHandler func(msg []byte), failHandler func(err error)) error {
	if m.ReturnError {
		if m.SendAck {
			m.AckChannel <- struct{}{}
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
		m.SendCalled[req.Hash]++
		if r, ok := m.Responses[req.Hash]; ok {
			res.Responses = append(res.Responses, r)
		}
	}
	res.ID = r.ID
	bts, _ := types.InterfaceToBytes(res)
	resHandler(bts)

	if m.SendAck {
		m.AckChannel <- struct{}{}
	}
	return nil
}

var _ service.Service = (*mockNet)(nil)

func defaultFetch() (*Fetch, *mockNet) {
	cfg := Config{
		3,
		3,
		3,
		3,
	}

	mckNet := &mockNet{make(map[types.Hash32]int),
		false,
		make(map[types.Hash32]responseMessage),
		false,
		make(chan struct{}),
	}
	lg := log.NewDefault("fetch")
	f := NewFetch(cfg, mckNet, lg)
	f.net = mckNet
	f.AddDB("db", database.NewMemDatabase())
	f.AddDB("db2", database.NewMemDatabase())

	return f, mckNet
}

func customFetch(cfg Config) (*Fetch, *mockNet) {
	mckNet := &mockNet{make(map[types.Hash32]int),
		false,
		make(map[types.Hash32]responseMessage),
		false,
		make(chan struct{}),
	}

	lg := log.NewDefault("fetch")

	f := NewFetch(cfg, mckNet, lg)
	f.net = mckNet
	f.AddDB("db", database.NewMemDatabase())
	return f, mckNet
}

func TestFetch_GetHash(t *testing.T) {
	f, _ := defaultFetch()
	defer f.Stop()
	f.Start()
	f.cfg.BatchTimeout = 2000 // make sure we never hit the batch timeout
	h1 := randomHash()
	hint := Hint("db")
	hint2 := Hint("db2")

	//test hash aggregation
	f.GetHash(h1, hint, false)
	f.GetHash(h1, hint, false)

	h2 := randomHash()
	f.GetHash(h2, hint2, false)

	//test aggregation by hint

	assert.Equal(t, 2, len(f.activeRequests[h1]))
}

func TestFetch_requestHashFromPeers_AggregateAndValidate(t *testing.T) {
	h1 := randomHash()
	f, net := defaultFetch()

	// set response mock
	res := responseMessage{
		Hash: h1,
		data: []byte("a"),
	}
	net.Responses[h1] = res

	hint := Hint("db")
	request1 := request{
		//successCallback:      req.OkCallback,
		hash:                 h1,
		priority:             0,
		validateResponseHash: false,
		hint:                 hint,
		returnChan:           make(chan HashDataPromiseResult, 6),
	}

	f.activeRequests[h1] = []request{request1, request1, request1}
	f.requestHashBatchFromPeers()

	// test aggregation of messages before calling fetch from peer
	//assert.Equal(t, 3, req.OkCalledNum[h1])
	assert.Equal(t, 1, net.SendCalled[h1])

	// test incorrect hash fail
	request1.validateResponseHash = true
	f.activeRequests[h1] = []request{request1, request1, request1}
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
	assert.Equal(t, 2, net.SendCalled[h1])
	assert.Equal(t, 3, notOk)
	assert.Equal(t, 3, okCount)
}

func TestFetch_GetHash_StartStopSanity(t *testing.T) {
	f, _ := defaultFetch()
	f.Start()
	f.Stop()
}

func TestFetch_GetHash_failNetwork(t *testing.T) {
	h1 := randomHash()
	f, net := defaultFetch()

	// set response mock
	bts := responseMessage{
		Hash: h1,
		data: []byte("a"),
	}
	net.Responses[h1] = bts

	net.ReturnError = true

	hint := Hint("db")
	request1 := request{
		//successCallback:      req.OkCallback,
		hash:                 h1,
		priority:             0,
		validateResponseHash: false,
		hint:                 hint,
		returnChan:           make(chan HashDataPromiseResult, f.cfg.MaxRetiresForPeer),
	}
	f.activeRequests[h1] = []request{request1, request1, request1}
	f.requestHashBatchFromPeers()

	// test aggregation of messages before calling fetch from peer
	assert.Equal(t, 3, len(request1.returnChan))
	assert.Equal(t, 0, net.SendCalled[h1])
}

func TestFetch_requestHashFromPeers_BatchRequestMax(t *testing.T) {
	h1 := randomHash()
	h2 := randomHash()
	h3 := randomHash()
	f, net := customFetch(Config{
		BatchTimeout:      1,
		MaxRetiresForPeer: 2,
		BatchSize:         2,
	})

	// set response mock
	bts := responseMessage{
		Hash: h1,
		data: []byte("a"),
	}
	bts2 := responseMessage{
		Hash: h2,
		data: []byte("a"),
	}
	bts3 := responseMessage{
		Hash: h3,
		data: []byte("a"),
	}
	net.Responses[h1] = bts
	net.Responses[h2] = bts2
	net.Responses[h3] = bts3
	net.SendAck = true

	hint := Hint("db")

	defer f.Stop()
	f.Start()
	//test hash aggregation
	r1 := f.GetHash(h1, hint, false)
	r2 := f.GetHash(h2, hint, false)
	r3 := f.GetHash(h3, hint, false)

	//since we have a batch of 2 we should call send twice - of not we should fail
	select {
	case <-net.AckChannel:
		break
	case <-time.After(2 * time.Second):
		assert.Fail(t, "timeout getting")
	}

	select {
	case <-net.AckChannel:
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
	assert.Equal(t, 1, net.SendCalled[h1])
	assert.Equal(t, 1, net.SendCalled[h2])
	assert.Equal(t, 1, net.SendCalled[h1])
}
