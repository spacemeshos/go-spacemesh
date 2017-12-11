package p2p2

import (
	"fmt"
	//"math/rand"
	"github.com/UnrulyOS/go-unruly/assert"
	"github.com/UnrulyOS/go-unruly/log"
	"math/rand"
	"testing"
	"time"
)

func TestReadWrite(t *testing.T) {

	var msg = []byte("hello world")
	port := uint(rand.Intn(100) + 10000)
	address := fmt.Sprintf("localhost:%d", port)

	done := make(chan bool, 1)

	n, err := NewNetwork(address)
	assert.Nil(t, err, "failed to create tcp server")

	// a simple network events processor
	process := func () {
	Loop:
		for {
			select {
			case <-done:
				// todo: gracefully stop the swarm - close all connections to remote nodes
				break Loop

			case c := <-n.GetNewConnections():
				log.Info("Remote client connected. %v", c)

			case m := <-n.GetIncomingMessage():
				log.Info("Got remote message: %s", string(m.Message))
				m.Connection.Close()
				done <- true

			case err := <-n.GetConnectionErrors():
				t.Fatalf("Connection error: %v", err)

			case err := <-n.GetMessageSendErrors():
				t.Fatalf("Failed to send message to connection: %v", err)

			case c := <-n.GetClosingConnections():
				log.Info("Connection closed. %v", c)
			}
		}
	}

	go process ()

	c, err := n.DialTCP(address, time.Duration(10*time.Second))
	assert.Nil(t, err, "failed to connect to tcp server")

	log.Info("Sending message...")
	c.Send(msg)
	//assert.Nil(t, err, "Failed to send message to server")
	//assert.Equal(t, l, len(msg) + 4, "Expected message to be written to stream")
	log.Info("Message sent.")

	// todo: test callbacks for messages

	log.Info("Waiting for incoming messages...")

	<-done

}
