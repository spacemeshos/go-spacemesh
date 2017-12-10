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

	n.OnConnectionClosed(func(c Connection) {
		log.Info("Connection closed")
	})

	n.OnConnectionError(func(c Connection, err error) {
		t.Fatalf("Connection error: %v", err)
	})

	n.OnMessageSendError(func(c Connection, err error) {
		t.Fatalf("Failed to send message to connection: %v", err)
	})

	var remoteConn Connection

	n.OnRemoteClientConnected(func(c Connection) {
		log.Info("Remote client connected.")
		remoteConn = c
	})

	n.OnRemoteClientMessage(func(c Connection, m []byte) {
		log.Info("Got remote message: %s", string(m))

		// close the connection
		c.Close()
		done <- true
	})

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
