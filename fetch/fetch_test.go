package fetch

import (
	"fmt"
	"github.com/spacemeshos/go-spacemesh/common/types"
	"github.com/spacemeshos/go-spacemesh/log"
	"github.com/spacemeshos/go-spacemesh/p2p/p2pcrypto"
	p2ppeers "github.com/spacemeshos/go-spacemesh/p2p/peers"
	"github.com/spacemeshos/go-spacemesh/p2p/server"
	"github.com/spacemeshos/go-spacemesh/rand"
	"github.com/stretchr/testify/assert"
	"testing"
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

func (m *mockRequest) OkCallback(hash types.Hash32, buf []byte) {
	m.OkCalled[hash] = buf
	m.OkCalledNum[hash]++
}

func (m *mockRequest) ErrCallback(hash types.Hash32, err error) {
	m.ErrCalled[hash] = err
	m.ErrCalledNum[hash]++
}

type mockNet struct {
	SendCalled  map[types.Hash32]int
	ReturnError bool
	Responses   map[types.Hash32][]byte
}

func (m mockNet) GetPeers() []p2ppeers.Peer {
	_, pub1, _ := p2pcrypto.GenerateKeyPair()
	return []p2ppeers.Peer{pub1}
}

func (m *mockNet) SendRequest(msgType server.MessageType, payload []byte, address p2pcrypto.PublicKey, resHandler func(msg []byte)) error {
	if m.ReturnError {
		return fmt.Errorf("mock error")
	}
	var r requestMessage
	err := types.BytesToInterface(payload, &r)
	if err != nil {
		panic("invalid message")
	}
	m.SendCalled[r.Hash]++
	if payload, ok := m.Responses[r.Hash]; ok {
		resHandler(payload)
	}

	return nil
}

func defaultFetch() (*Fetch, *mockNet) {
	cfg := Config{
		3,
		3,
	}

	mckNet := &mockNet{make(map[types.Hash32]int), false, make(map[types.Hash32][]byte)}

	lg := log.NewDefault("fetch")
	return NewFetch(cfg, mckNet, lg), mckNet
}

func TestFetch_GetHash(t *testing.T) {
	f, _ := defaultFetch()
	defer f.Stop()
	f.Start()
	f.cfg.BatchTimeout = 2000 // make sure we never hit the batch timeout
	h1 := randomHash()
	req := newRequestMock()
	hint := Hint("db")
	hint2 := Hint("db2")

	//test hash aggregation
	f.GetHash(h1, hint, req.OkCallback, req.ErrCallback, true)
	f.GetHash(h1, hint, req.OkCallback, req.ErrCallback, true)

	h2 := randomHash()
	f.GetHash(h2, hint2, req.OkCallback, req.ErrCallback, true)

	//test aggregation by hint
	assert.Equal(t, 2, len(f.requests))
	assert.Equal(t, 2, len(f.requests[hint]))
}

func TestFetch_requestHashFromPeers_AggregateAndValidate(t *testing.T) {
	h1 := randomHash()
	f, net := defaultFetch()
	req := newRequestMock()

	// set response mock
	net.Responses[h1] = []byte{}

	hint := Hint("db")
	request1 := request{
		success:          req.OkCallback,
		fail:             req.ErrCallback,
		hash:             h1,
		priority:         0,
		validateResponse: false,
		hint:             hint,
	}
	requests := []request{request1, request1, request1}
	f.requestHashFromPeers(hint, requests)

	// test aggregation of messages before calling fetch from peer
	assert.Equal(t, 3, req.OkCalledNum[h1])
	assert.Equal(t, 1, net.SendCalled[h1])

	// test incorrect hash fail
	request1.validateResponse = true
	requests = []request{request1, request1, request1}
	f.requestHashFromPeers(hint, requests)

	assert.Equal(t, 3, req.ErrCalledNum[h1])
	assert.Equal(t, 2, net.SendCalled[h1])
}

func TestFetch_GetHash_StartStopSanity(t *testing.T) {
	f, _ := defaultFetch()
	f.Start()
	f.Stop()
}

func TestFetch_GetHash_failNetwork(t *testing.T) {
	h1 := randomHash()
	f, net := defaultFetch()
	req := newRequestMock()

	// set response mock
	net.Responses[h1] = []byte{}

	net.ReturnError = true

	hint := Hint("db")
	request1 := request{
		success:          req.OkCallback,
		fail:             req.ErrCallback,
		hash:             h1,
		priority:         0,
		validateResponse: false,
		hint:             hint,
	}
	requests := []request{request1, request1, request1}
	f.requestHashFromPeers(hint, requests)

	// test aggregation of messages before calling fetch from peer
	assert.Equal(t, 3, req.ErrCalledNum[h1])
	assert.Equal(t, 0, net.SendCalled[h1])
}
