package p2p

import (
	"context"
	"errors"
	"github.com/spacemeshos/go-spacemesh/p2p/service"
	"github.com/spacemeshos/go-spacemesh/priorityq"
	"github.com/spacemeshos/go-spacemesh/rand"
	"github.com/stretchr/testify/require"
	"github.com/stretchr/testify/suite"
	"golang.org/x/sync/errgroup"
	_ "net/http/pprof"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

var exampleGossipProto = "exampleGossip"
var exampleDirectProto = "exampleDirect"

type P2PIntegrationSuite struct {
	localMtx        sync.Mutex
	gossipProtocols []chan service.GossipMessage
	directProtocols []chan service.DirectMessage
	*IntegrationTestSuite
}

func protocolsHelper(s *P2PIntegrationSuite) {
	s.gossipProtocols = make([]chan service.GossipMessage, 0, 100)
	s.directProtocols = make([]chan service.DirectMessage, 0, 100)
	s.BeforeHook = func(idx int, nd NodeTestInstance) {
		// these happen async so we need to lock
		s.localMtx.Lock()
		s.gossipProtocols = append(s.gossipProtocols, nd.RegisterGossipProtocol(exampleGossipProto, priorityq.High))
		s.directProtocols = append(s.directProtocols, nd.RegisterDirectProtocol(exampleDirectProto))
		s.localMtx.Unlock()
	}
	// from now on there are only reads so no locking needed
}

func (its *P2PIntegrationSuite) Test_SendingMessage() {
	exMsg := RandString(10)

	node1 := its.Instances[0]
	node2 := its.Instances[1]
	recvChan := node2.directProtocolHandlers[exampleDirectProto]
	require.NotNil(its.T(), recvChan)

	conn, err := node1.cPool.GetConnection(node2.network.LocalAddr(), node2.lNode.PublicKey())
	require.NoError(its.T(), err)
	err = node1.SendMessage(node2.LocalNode().PublicKey(), exampleDirectProto, []byte(exMsg))
	require.NoError(its.T(), err)

	tm := time.After(10 * time.Second)

	select {
	case gotmessage := <-recvChan:
		if string(gotmessage.Bytes()) != exMsg {
			its.T().Fatal("got wrong message")
		}
	case <-tm:
		its.T().Fatal("failed to deliver message within second")
	}
	conn.Close()
}

func (its *P2PIntegrationSuite) Test_Gossiping() {
	ctx, _ := context.WithTimeout(context.Background(), time.Minute*5)
	errg, ctx := errgroup.WithContext(ctx)

	donefor := make(map[string]struct{})
	_ = its.ForAll(func(idx int, s NodeTestInstance) error {
		donefor[s.LocalNode().PublicKey().String()] = struct{}{}
		return nil
	}, nil)

	var doneformtx sync.Mutex

	MSGS := 100
	MSGSIZE := 10
	tm := time.Now()
	testLog("%v Sending %v messages with size %v to %v miners", its.T().Name(), MSGS, MSGSIZE, (its.BootstrappedNodeCount + its.BootstrapNodesCount))
	numgot := int32(0)
	for i := 0; i < MSGS; i++ {
		msg := []byte(RandString(MSGSIZE))
		rnd := rand.Int31n(int32(len(its.Instances)))
		_ = its.Instances[rnd].Broadcast(exampleGossipProto, msg)
		for i, mc := range its.gossipProtocols {
			i := i
			ctx := ctx
			mc := mc
			numgot := &numgot
			errg.Go(func() error {
				select {
				case got := <-mc:
					atomic.AddInt32(numgot, 1)
					got.ReportValidation(exampleGossipProto)
					doneformtx.Lock()
					if i < len(its.boot) {
						delete(donefor, its.boot[i].LocalNode().PublicKey().String())
					} else {
						delete(donefor, its.Instances[i-(len(its.boot))].LocalNode().PublicKey().String())
					}
					doneformtx.Unlock()
					return nil
				case <-ctx.Done():
					return errors.New("timed out")
				}
			})
		}
	}

	testLog("%v Waiting for all messages to pass", its.T().Name())

	errs := errg.Wait()
	its.T().Log(errs)
	its.NoError(errs)
	its.Equal(int(numgot), (its.BootstrappedNodeCount+its.BootstrapNodesCount)*MSGS)
	its.Equal(len(donefor), 0)

	for k, _ := range donefor {
		testLog("%v didn't finish ", k)
	}

	testLog("%v All nodes got all messages in %v", its.T().Name(), time.Since(tm))
}

// TODO: Add more tests to the suite

func Test_ReallySmallP2PIntegrationSuite(t *testing.T) {
	s := new(P2PIntegrationSuite)
	s.IntegrationTestSuite = new(IntegrationTestSuite)

	protocolsHelper(s)

	s.BootstrappedNodeCount = 2
	s.BootstrapNodesCount = 1
	s.NeighborsCount = 1

	suite.Run(t, s)
}

func Test_SmallP2PIntegrationSuite(t *testing.T) {
	s := new(P2PIntegrationSuite)
	s.IntegrationTestSuite = new(IntegrationTestSuite)

	protocolsHelper(s)

	s.BootstrappedNodeCount = 70
	s.BootstrapNodesCount = 1
	s.NeighborsCount = 8

	suite.Run(t, s)
}

func Test_BigP2PIntegrationSuite(t *testing.T) {
	s := new(P2PIntegrationSuite)
	s.IntegrationTestSuite = new(IntegrationTestSuite)

	protocolsHelper(s)

	s.BootstrappedNodeCount = 100
	s.BootstrapNodesCount = 3
	s.NeighborsCount = 8

	suite.Run(t, s)
}
