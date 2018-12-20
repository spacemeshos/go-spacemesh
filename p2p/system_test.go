package p2p

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"github.com/davecgh/go-spew/spew"
	"github.com/spacemeshos/go-spacemesh/p2p/service"
	"github.com/stretchr/testify/assert"
	"golang.org/x/sync/errgroup"
	"math/rand"
	"sync/atomic"
	"testing"
	"time"
)

type systemTest struct {
	networkSize       int
	bootNodes         int
	randomConnections int
}

func (st *systemTest) Run(t testing.TB, testFunc func(t testing.TB, network *P2PSwarm)) {
	fmt.Println("===============================================================================================")
	fmt.Println("Running a test with these parameters :" + spew.Sdump(st))
	tim := time.Now()
	network := NewP2PSwarm(nil, nil)
	network.Start(t, st.bootNodes, st.networkSize, st.randomConnections)
	testFunc(t, network)
	fmt.Println("===============================================================================================")
	fmt.Println("Took " + time.Now().Sub(tim).String() + " to run a test with these options: " + spew.Sdump(st))
	fmt.Println("===============================================================================================")
}

func Test_SendingMessage(t *testing.T) {

	if testing.Short() {
		t.Skip()
	}

	test := new(systemTest)

	test.networkSize = 30
	test.bootNodes = 3
	test.randomConnections = 8

	testFunc := func(t testing.TB, network *P2PSwarm) {
		exProto := RandString(10)
		exMsg := RandString(10)

		rnd := rand.Int31n(int32(len(network.Swarm) - 1))
		rnd2 := rand.Int31n(int32(len(network.Swarm) - 1))

		node1 := network.Swarm[rnd]
		node2 := network.Swarm[rnd2]
		_ = node1.RegisterProtocol(exProto)
		ch2 := node2.RegisterProtocol(exProto)

		err := node1.SendMessage(node2.lNode.Node.String(), exProto, []byte(exMsg))
		if err != nil {
			t.Fatal("", err)
		}

		tm := time.After(1 * time.Second)

		select {
		case gotmessage := <-ch2:
			if string(gotmessage.Bytes()) != exMsg {
				t.Fatal("got wrong message")
			}
		case <-tm:
			t.Fatal("failed to deliver message within second")
		}
	}

	test.Run(t, testFunc)
}

func Test_Gossiping(t *testing.T) {
	if testing.Short() {
		t.Skip()
	}
	test := new(systemTest)

	test.networkSize = 30
	test.bootNodes = 3
	test.randomConnections = 8

	testFunc := func(t testing.TB, network *P2PSwarm) {
		msgChans := make([]chan service.Message, 0)
		exProto := RandString(10)

		rnd := rand.Int31n(int32(len(network.Swarm) - 1))
		node1 := network.Swarm[rnd]

		network.ForAll(func(s *swarm) error {
			if node1.lNode.PublicKey().String() == s.lNode.PublicKey().String() {
				s.RegisterProtocol(exProto)
				return nil
			}
			msgChans = append(msgChans, s.RegisterProtocol(exProto))
			return nil
		}, nil)

		msg := []byte(RandString(10))
		node1.Broadcast(exProto, []byte(msg))
		numgot := int32(0)

		ctx, _ := context.WithTimeout(context.Background(), time.Second*10)
		errg, ctx := errgroup.WithContext(ctx)
		for _, mc := range msgChans {
			ctx := ctx
			mc := mc
			numgot := &numgot
			errg.Go(func() error {
				select {
				case got := <-mc:
					if !bytes.Equal(got.Bytes(), msg) {
						return fmt.Errorf("Wrong msg, got :%s, want:%s", got, msg)
					}
					atomic.AddInt32(numgot, 1)
					return nil
				case <-ctx.Done():
					return errors.New("Timeouted")
				}
				return errors.New("none")
			})
		}

		errs := errg.Wait()
		t.Log(errs)
		assert.NoError(t, errs)
		assert.Equal(t, int(numgot), 30-1)
	}

	test.Run(t, testFunc)
}
