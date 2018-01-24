package net

import (
	"fmt"
	"github.com/spacemeshos/go-spacemesh/assert"
	"github.com/spacemeshos/go-spacemesh/crypto"
	"github.com/spacemeshos/go-spacemesh/log"
	"github.com/spacemeshos/go-spacemesh/p2p/nodeconfig"
	"testing"
	"time"
)

func TestReadWrite(t *testing.T) {

	msg := []byte("hello world")
	msgId := crypto.UUID()
	port := crypto.GetRandomUInt32(1000) + 10000
	address := fmt.Sprintf("0.0.0.0:%d", port)
	done := make(chan bool, 1)

	n, err := NewNet(address, nodeconfig.ConfigValues)
	assert.Nil(t, err, "failed to create tcp server")

	_, err = NewNet(address, nodeconfig.ConfigValues)
	assert.Err(t, err, "Should not be able to create a new net on same address")

	// run a simple network events processor go routine
	go func() {
	Loop:
		for {
			select {
			case <-done:
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
	}()

	// we use the network to dial to itself over the local loop
	c, err := n.DialTCP(address, time.Duration(10*time.Second), time.Duration(48*time.Hour))
	assert.Nil(t, err, "failed to connect to tcp server")

	log.Info("Sending message...")

	t1 := c.LastOpTime()

	c.Send(msg, msgId)
	//assert.Nil(t, err, "Failed to send message to server")
	//assert.Equal(t, l, len(msg) + 4, "Expected message to be written to stream")
	log.Info("Message sent.")

	// todo: test callbacks for messages

	log.Info("Waiting for incoming messages...")

	<-done

	n.Shutdown()
	_, err = n.DialTCP(address, time.Duration(10*time.Second), time.Duration(48*time.Hour))
	assert.Err(t, err, "expected to fail dialing after calling shutdown")

	t2 := c.LastOpTime()

	// verify connection props
	id := c.Id()
	assert.True(t, len(id) > 0, "failed to get connection id")
	assert.True(t, t2.Sub(t1) > 0, "invalid last op time")
	err = c.Close()
	assert.NoErr(t, err, "error closing connection")

}
