package node

import (
	"github.com/UnrulyOS/go-unruly/log"
	"math/rand"
	"testing"

	ps "gx/ipfs/QmPgDWmTmuzvP7QE5zwo1TmjbJme9pmZHNujB2453jkCTr/go-libp2p-peerstore"

)

func TestP2pProtocols(t *testing.T) {

	// Choose random ports between 10000-10100
	rand.Seed(666)
	port1 := rand.Intn(100) + 10000
	port2 := port1 + 1

	done := make(chan bool, 1)

	// Make 2 nodes
	h1 := NewLocalNode(port1, done)
	h2 := NewLocalNode(port2, done)

	// let the nodes know about each other
	h1.Peerstore().AddAddrs(h2.ID(), h2.Addrs(), ps.PermanentAddrTTL)
	h2.Peerstore().AddAddrs(h1.ID(), h1.Addrs(), ps.PermanentAddrTTL)

	log.Info("This is a conversation between %s and %s\n", h1.ID(), h2.ID())

	// send messages using the protocols
	h1.Ping(h2.Host)
	h2.Ping(h1.Host)
	h1.Echo(h2.Host)
	h2.Echo(h1.Host)

	// block until all responses have been processed
	for i := 0; i < 4; i++ {
		<-done
	}
}
