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
	"sync/atomic"
	"testing"
	"time"
)

var exampleGossipProto = "exampleGossip"
var exampleDirectProto = "exampleDirect"

type P2PIntegrationSuite struct {
	gossipProtocols []chan service.GossipMessage
	directProtocols []chan service.DirectMessage
	*IntegrationTestSuite
}

func protocolsHelper(s *P2PIntegrationSuite) {
	s.gossipProtocols = make([]chan service.GossipMessage, 0, 100)
	s.directProtocols = make([]chan service.DirectMessage, 0, 100)
	s.BeforeHook = func(idx int, nd NodeTestInstance) {
		s.gossipProtocols = append(s.gossipProtocols, nd.RegisterGossipProtocol(exampleGossipProto, priorityq.High))
		s.directProtocols = append(s.directProtocols, nd.RegisterDirectProtocol(exampleDirectProto))
	}
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
	ctx, _ := context.WithTimeout(context.Background(), time.Minute*180)
	errg, ctx := errgroup.WithContext(ctx)
	MSGS := 100
	numgot := int32(0)
	for i := 0; i < MSGS; i++ {
		msg := []byte(RandString(108692))
		rnd := rand.Int31n(int32(len(its.Instances)))
		_ = its.Instances[rnd].Broadcast(exampleGossipProto, []byte(msg))
		for _, mc := range its.gossipProtocols {
			ctx := ctx
			mc := mc
			numgot := &numgot
			errg.Go(func() error {
				select {
				case got := <-mc:
					got.ReportValidation(exampleGossipProto)
					atomic.AddInt32(numgot, 1)
					return nil
				case <-ctx.Done():
					return errors.New("timed out")
				}
			})
		}
	}

	errs := errg.Wait()
	its.T().Log(errs)
	its.NoError(errs)
	its.Equal(int(numgot), (its.BootstrappedNodeCount+its.BootstrapNodesCount)*MSGS)
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
