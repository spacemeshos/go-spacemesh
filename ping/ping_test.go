package ping

import (
	"github.com/spacemeshos/go-spacemesh/log"
	"github.com/spacemeshos/go-spacemesh/p2p/service"
	"github.com/spacemeshos/go-spacemesh/ping/pb"
	"github.com/stretchr/testify/assert"
	"testing"
	"time"
)

func TestPing_Ping(t *testing.T) {

	sim := service.NewSimulator()
	node1 := sim.NewNode()
	node2 := sim.NewNode()
	node3 := sim.NewNode()

	p := New(node1.Node, node1, log.New("1_"+node1.String(), "", ""))
	p2 := New(node2.Node, node2, log.New("2_"+node2.String(), "", ""))

	p.RegisterCallback(func(ping *pb.Ping) error {
		//pk, _ := p2pcrypto.NewPubkeyFromBytes(ping.ID)
		go p.Ping(node3.PublicKey())
		return nil
	})

	p2.RegisterCallback(func(ping *pb.Ping) error {
		go p2.Ping(node3.PublicKey())
		return nil
	})

	err := p.Ping(node2.PublicKey())
	assert.NoError(t, err)

	err = p2.Ping(node1.PublicKey())
	assert.NoError(t, err)
	//
	//err = p.Ping(node3.PublicKey())
	//assert.Error(t, err)
	time.Sleep(10 * time.Second)

}

func TestPing_Ping_Concurrency(t *testing.T) {
	sim := service.NewSimulator()
	node1 := sim.NewNode()
	node2 := sim.NewNode()
	node3 := sim.NewNode()
	node4 := sim.NewNode()

	p := New(node1.Node, node1, log.New("1_"+node1.String(), "", ""))
	_ = New(node2.Node, node2, log.New("1_"+node2.String(), "", ""))
	_ = New(node3.Node, node3, log.New("1_"+node3.String(), "", ""))
	_ = New(node4.Node, node4, log.New("1_"+node4.String(), "", ""))

	done := make(chan struct{})

	go func() {
		err := p.Ping(node2.PublicKey())
		assert.NoError(t, err)
		done <- struct{}{}
	}()

	go func() {
		err := p.Ping(node3.PublicKey())
		assert.NoError(t, err)
		done <- struct{}{}
	}()

	go func() {
		err := p.Ping(node4.PublicKey())
		assert.NoError(t, err)
		done <- struct{}{}
	}()

	<-done
	<-done
	<-done
}
