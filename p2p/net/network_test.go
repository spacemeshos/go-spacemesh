package net

import (
	"github.com/spacemeshos/go-spacemesh/log"
	"testing"
	"fmt"
	"github.com/spacemeshos/go-spacemesh/p2p/node"
	"github.com/stretchr/testify/assert"
	"github.com/spacemeshos/go-spacemesh/p2p/config"
	"github.com/gogo/protobuf/proto"
	"time"
)

func testLogger(id string) log.Log {
	// empty loggers so no files will be kept
	return log.New(id, "", "")
}
/*
func TestReadWrite(t *testing.T) {

	msg := []byte("hello spacemesh")
	msgID := crypto.NewUUID()

	port, err := GetUnboundedPort()
	assert.NoError(t, err, "Should be able to establish a connection on a port")

	address := fmt.Sprintf("0.0.0.0:%d", port)
	done := make(chan bool, 1)

	n, err := NewNet(address, config.ConfigValues, testLogger("TEST-net").Logger)
	assert.Nil(t, err, "failed to create tcp server")

	_, err = NewNet(address, config.ConfigValues, testLogger("TEST-net2").Logger)
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
*/

type NetMessage struct {
	data 	[]byte
	err		error
}

func (msg NetMessage) Message() []byte {
	return msg.data
}

func (msg NetMessage) Error() error {
	return msg.err
}

type ConnectionWithErrorOnSend struct {
	ConnectionMock
}

func (impl *ConnectionWithErrorOnSend) Send(bt []byte) error{
	return fmt.Errorf("error or whatever")
}

func waitForCallbackOrTimeout(t *testing.T, outchan chan Connectioner, expectedSession NetworkSession){
	select {
	case res := <- outchan :
		assert.Equal(t, expectedSession.ID(),res.Session().ID(),"wrong session received")
	case <-time.After(1 * time.Second):
		assert.False(t,false, "Didn't get channel notification")
	}
}

func TestHandlePreSessionIncomingMessage(t *testing.T){
	port := 0
	address := fmt.Sprintf("0.0.0.0:%d", port)
	//cfg := config.DefaultConfig()
	localNode, err := node.NewLocalNode(config.ConfigValues, address, false)
	remoteNode, err := node.NewLocalNode(config.ConfigValues, address, false)
	assert.NoError(t, err, "Should be able to create localnode")
	con := NewConnectionMock(remoteNode.PublicKey(), Remote)
	//n, _ := NewNet(config.ConfigValues, localNode)
	remoteNet, _ := NewNet(config.ConfigValues, remoteNode)
	outchan := remoteNet.SubscribeOnNewRemoteConnections()
	out, session, er := GenerateHandshakeRequestData(localNode.PublicKey(), localNode.PrivateKey(),con.RemotePublicKey(), remoteNet.GetNetworkId())
	assert.NoError(t, er, "cant generate handshake message")
	msg := NetMessage{}
	data, err := proto.Marshal(out)
	assert.NoError(t, err, "cannot marshal obj")
	msg.data = data
	err = remoteNet.HandlePreSessionIncomingMessage(con, msg)
	assert.NoError(t, err, "handle session failed")
	waitForCallbackOrTimeout(t, outchan, session)

	con.remotePub = nil
	err = remoteNet.HandlePreSessionIncomingMessage(con, msg)
	assert.NoError(t, err, "handle session failed")
	waitForCallbackOrTimeout(t, outchan, session)
	assert.Equal(t,remoteNode.PublicKey().String(),con.remotePub.String(),"Remote connection was not updated properly")

	othercon := ConnectionWithErrorOnSend{*con}
	err = remoteNet.HandlePreSessionIncomingMessage(&othercon, msg)
	assert.Error(t, err, "handle session failed")
	waitForCallbackOrTimeout(t, outchan, session)
}
