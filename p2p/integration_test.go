package p2p

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"github.com/spacemeshos/go-spacemesh/p2p/service"
	"github.com/stretchr/testify/suite"
	"golang.org/x/sync/errgroup"
	"sync/atomic"
	"testing"
	"time"
)


func (its *IntegrationTestSuite) Test_SendingMessage()  {
	exProto := RandString(10)
	exMsg := RandString(10)

	node1 := its.Instances[0]
	node2 := its.Instances[1]

	_ = node1.RegisterDirectProtocol(exProto)
	ch2 := node2.RegisterDirectProtocol(exProto)

	err := node1.SendMessage(node2.LocalNode().Node.PublicKey(), exProto, []byte(exMsg))
	if err != nil {
		its.T().Fatal("", err)
	}

	tm := time.After(1 * time.Second)

	select {
	case gotmessage := <-ch2:
		if string(gotmessage.Bytes()) != exMsg {
			its.T().Fatal("got wrong message")
		}
	case <-tm:
		its.T().Fatal("failed to deliver message within second")
	}
}


func (its *IntegrationTestSuite) Test_Gossiping() {

	msgChans := make([]chan service.GossipMessage, 0)
	exProto := RandString(10)

	node1 := its.Instances[0]

	its.ForAll(func(idx int, s NodeTestInstance) error {
		msgChans = append(msgChans, s.RegisterGossipProtocol(exProto))
		return nil
	}, nil)

	msg := []byte(RandString(10))

	_ = node1.Broadcast(exProto, []byte(msg))
	numgot := int32(0)

	ctx, _ := context.WithTimeout(context.Background(), time.Second*100)
	errg, ctx := errgroup.WithContext(ctx)
	for _, mc := range msgChans {
		ctx := ctx
		mc := mc
		numgot := &numgot
		errg.Go(func() error {
			select {
			case got := <-mc:
				if !bytes.Equal(got.Bytes(), msg) {
					return fmt.Errorf("wrong msg, got: %s, want: %s", got, msg)
				}
				got.ValidationCompletedChan() <- service.NewMessageValidation(got.Bytes(), exProto, true)
				atomic.AddInt32(numgot, 1)
				return nil
			case <-ctx.Done():
				return errors.New("timed out")
			}
		})
	}

	errs := errg.Wait()
	its.T().Log(errs)
	its.NoError(errs)
	its.Equal(int(numgot), its.BootstrappedNodeCount + its.BootstrapNodesCount)
}

func Test_SmallP2PIntegrationSuite(t *testing.T) {
	if testing.Short() {
		t.Skip()
	}

	s := new(IntegrationTestSuite)

	s.BootstrappedNodeCount = 10
	s.BootstrapNodesCount = 1
	s.NeighborsCount = 3

	suite.Run(t, s)
}

func Test_BigP2PIntegrationSuite(t *testing.T) {
	if testing.Short() {
		t.Skip()
	}

	s := new(IntegrationTestSuite)

	s.BootstrappedNodeCount = 30
	s.BootstrapNodesCount = 3
	s.NeighborsCount = 8

	suite.Run(t, s)
}
