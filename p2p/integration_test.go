package p2p

import (
	"context"
	"errors"
	"github.com/spacemeshos/go-spacemesh/p2p/service"
	"github.com/spacemeshos/go-spacemesh/priorityq"
	//"github.com/spacemeshos/go-spacemesh/priorityq"
	"github.com/spacemeshos/go-spacemesh/rand"
	"github.com/stretchr/testify/suite"
	"golang.org/x/sync/errgroup"
	"sync/atomic"
	"testing"
	"time"
)

var exampleProto = "example"

type P2PIntegrationSuite struct {
	protocols []chan service.GossipMessage
	*IntegrationTestSuite
}

func (its *P2PIntegrationSuite) Test_Gossiping() {

	ctx, _ := context.WithTimeout(context.Background(), time.Minute*180)
	errg, ctx := errgroup.WithContext(ctx)
	MSGS := 100
	numgot := int32(0)
	for i := 0; i < MSGS; i++ {
		msg := []byte(RandString(108692))
		rnd := rand.Int31n(int32(len(its.Instances)))
		_ = its.Instances[rnd].Broadcast(exampleProto, []byte(msg))
		for _, mc := range its.protocols {
			ctx := ctx
			mc := mc
			numgot := &numgot
			errg.Go(func() error {
				select {
				case got := <-mc:
					got.ReportValidation(exampleProto)
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
	s.protocols = make([]chan service.GossipMessage, 0, 100)
	s.BeforeHook = func(idx int, nd NodeTestInstance) {
		s.protocols = append(s.protocols, nd.RegisterGossipProtocol(exampleProto, priorityq.High))
	}

	s.BootstrappedNodeCount = 2
	s.BootstrapNodesCount = 1
	s.NeighborsCount = 1

	suite.Run(t, s)
}

func Test_SmallP2PIntegrationSuite(t *testing.T) {
	s := new(P2PIntegrationSuite)
	s.IntegrationTestSuite = new(IntegrationTestSuite)

	s.protocols = make([]chan service.GossipMessage, 0, 100)
	s.BeforeHook = func(idx int, nd NodeTestInstance) {
		s.protocols = append(s.protocols, nd.RegisterGossipProtocol(exampleProto, priorityq.High))
	}

	s.BootstrappedNodeCount = 70
	s.BootstrapNodesCount = 1
	s.NeighborsCount = 8

	suite.Run(t, s)
}

func Test_BigP2PIntegrationSuite(t *testing.T) {
	s := new(P2PIntegrationSuite)
	s.IntegrationTestSuite = new(IntegrationTestSuite)

	s.protocols = make([]chan service.GossipMessage, 0, 100)
	s.BeforeHook = func(idx int, nd NodeTestInstance) {
		s.protocols = append(s.protocols, nd.RegisterGossipProtocol(exampleProto, priorityq.High))
	}

	s.BootstrappedNodeCount = 100
	s.BootstrapNodesCount = 3
	s.NeighborsCount = 8

	suite.Run(t, s)
}
