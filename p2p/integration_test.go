package p2p

import (
	"context"
	"errors"
	"github.com/spacemeshos/go-spacemesh/common/types"
	"github.com/spacemeshos/go-spacemesh/p2p/service"
	"github.com/spacemeshos/go-spacemesh/rand"
	"github.com/stretchr/testify/require"
	"github.com/stretchr/testify/suite"
	"golang.org/x/sync/errgroup"
	"sync/atomic"
	"testing"
	"time"
)

func (its *IntegrationTestSuite) Test_SendingMessage() {
	exProto := RandString(10)
	exMsg := RandString(10)

	node1 := its.Instances[0]
	node2 := its.Instances[1]

	_ = node1.RegisterDirectProtocol(exProto)
	ch2 := node2.RegisterDirectProtocol(exProto)
	addr := types.IPAddr{node2.network.LocalAddr().String(), "80", "tcp"}
	conn, err := node1.cPool.GetConnection(addr, node2.lNode.PublicKey())
	require.NoError(its.T(), err)
	err = node1.SendMessage(node2.LocalNode().NodeInfo.PublicKey(), exProto, []byte(exMsg))
	require.NoError(its.T(), err)

	tm := time.After(1 * time.Second)

	select {
	case gotmessage := <-ch2:
		if string(gotmessage.Bytes()) != exMsg {
			its.T().Fatal("got wrong message")
		}
	case <-tm:
		its.T().Fatal("failed to deliver message within second")
	}
	conn.Close()
}

func (its *IntegrationTestSuite) Test_Gossiping() {

	msgChans := make([]chan service.GossipMessage, 0)
	exProto := RandString(10)

	its.ForAll(func(idx int, s NodeTestInstance) error {
		msgChans = append(msgChans, s.RegisterGossipProtocol(exProto))
		return nil
	}, nil)

	ctx, _ := context.WithTimeout(context.Background(), time.Minute*180)
	errg, ctx := errgroup.WithContext(ctx)
	MSGS := 100
	numgot := int32(0)
	for i := 0; i < MSGS; i++ {
		msg := []byte(RandString(108692))
		rnd := rand.Int31n(int32(len(its.Instances)))
		_ = its.Instances[rnd].Broadcast(exProto, []byte(msg))
		for _, mc := range msgChans {
			ctx := ctx
			mc := mc
			numgot := &numgot
			errg.Go(func() error {
				select {
				case got := <-mc:
					got.ReportValidation(exProto)
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

func Test_ReallySmallP2PIntegrationSuite(t *testing.T) {
	t.Skip("very long test") // TODO

	s := new(IntegrationTestSuite)

	s.BootstrappedNodeCount = 2
	s.BootstrapNodesCount = 1
	s.NeighborsCount = 1

	suite.Run(t, s)
}

func Test_SmallP2PIntegrationSuite(t *testing.T) {
	t.Skip("very long test") // TODO

	s := new(IntegrationTestSuite)

	s.BootstrappedNodeCount = 70
	s.BootstrapNodesCount = 1
	s.NeighborsCount = 8

	suite.Run(t, s)
}

func Test_BigP2PIntegrationSuite(t *testing.T) {
	t.Skip("very long test") // TODO

	s := new(IntegrationTestSuite)

	s.BootstrappedNodeCount = 100
	s.BootstrapNodesCount = 3
	s.NeighborsCount = 8

	suite.Run(t, s)
}
