package net

import (
	"fmt"
	"testing"
	"time"

	"github.com/spacemeshos/go-spacemesh/crypto"
	"github.com/spacemeshos/go-spacemesh/log"
	"github.com/spacemeshos/go-spacemesh/p2p/nodeconfig"
	"github.com/stretchr/testify/assert"
	"gopkg.in/op/go-logging.v1"
)

func testLogger(id string) *logging.Logger {
	// empty loggers so no files will be kept
	return log.CreateLogger(id, "", "")
}

func TestReadWrite(t *testing.T) {

	msg := []byte("hello spacemesh")
	msgID := crypto.UUID()

	port, err := GetUnboundedPort()
	assert.NoError(t, err, "Should be able to establish a connection on a port")

	address := fmt.Sprintf("0.0.0.0:%d", port)
	done := make(chan bool, 1)

	n, err := NewNet(address, nodeconfig.ConfigValues, testLogger("TEST-net"))
	assert.Nil(t, err, "failed to create tcp server")

	_, err = NewNet(address, nodeconfig.ConfigValues, testLogger("TEST-net2"))
	assert.Error(t, err, "Should not be able to create a new net on same address")

	// run a simple network events processor go routine
	go func() {
	Loop:
		for {
			select {
			case <-done:
				break Loop

			case c := <-n.GetNewConnections():
				log.Debug("Remote client connected. %v", c)

			case m := <-n.GetIncomingMessage():
				log.Debug("Got remote message: %s", string(m.Message))
				m.Connection.Close()
				done <- true

			case err := <-n.GetConnectionErrors():
				t.Fatalf("Connection error: %v", err)

			case err := <-n.GetMessageSendErrors():
				t.Fatalf("Failed to send message to connection: %v", err)

			case c := <-n.GetClosingConnections():
				log.Debug("Connection closed. %v", c)

			case <-time.After(time.Second * 30):
				t.Fatalf("Test timed out")
			}
		}
	}()

	// we use the network to dial to itself over the local loop
	c, err := n.Dial(address, time.Duration(10*time.Second), time.Duration(48*time.Hour))
	assert.Nil(t, err, "failed to connect to tcp server")

	log.Debug("Sending message...")

	c.Send(msg, msgID)
	log.Debug("Message sent.")

	// todo: test callbacks for messages

	log.Debug("Waiting for incoming messages...")

	<-done

	n.Shutdown()
	_, err = n.Dial(address, time.Duration(10*time.Second), time.Duration(48*time.Hour))
	assert.Error(t, err, "expected to fail dialing after calling shutdown")
	//
	//
	//// verify connection props
	id := c.ID()
	assert.True(t, len(id) > 0, "failed to get connection id")
	err = c.Close()
	assert.NoError(t, err, "error closing connection")
}

func TestGetUnboundedPort(t *testing.T) {
	port, err := GetUnboundedPort()
	_ = port // to make lint pass
	assert.NoError(t, err, "Should be able to get an unbounded port")
}
