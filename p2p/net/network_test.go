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

func waitForCallbackOrTimeout(t *testing.T, outchan chan Connectioner, expectedSession NetworkSession){
	select {
	case res := <- outchan :
		assert.Equal(t, expectedSession.ID(),res.Session().ID(),"wrong session received")
	case <-time.After(1 * time.Second):
		assert.Nil(t,expectedSession, "Didn't get channel notification")
	}
}

func TestHandlePreSessionIncomingMessage(t *testing.T){
	port := 0
	address := fmt.Sprintf("0.0.0.0:%d", port)
	//cfg := config.DefaultConfig()
	localNode, err := node.NewLocalNode(config.ConfigValues, address, false)
	assert.NoError(t, err, "should be able to create localnode")
	remoteNode, err := node.NewLocalNode(config.ConfigValues, address, false)
	assert.NoError(t, err, "should be able to create localnode")
	con := NewConnectionMock(remoteNode.PublicKey(), Remote)
	remoteNet, _ := NewNet(config.ConfigValues, remoteNode)
	outchan := remoteNet.SubscribeOnNewRemoteConnections()
	out, session, er := GenerateHandshakeRequestData(localNode.PublicKey(), localNode.PrivateKey(),con.RemotePublicKey(), remoteNet.GetNetworkId())
	assert.NoError(t, er, "cant generate handshake message")
	data, err := proto.Marshal(out)
	assert.NoError(t, err, "cannot marshal obj")
	err = remoteNet.HandlePreSessionIncomingMessage(con, data)
	assert.NoError(t, err, "handle session failed")
	waitForCallbackOrTimeout(t, outchan, session)
	assert.Equal(t, int32(1), con.SendCount())

	con.remotePub = nil
	err = remoteNet.HandlePreSessionIncomingMessage(con, data)
	assert.NoError(t, err, "handle session failed")
	waitForCallbackOrTimeout(t, outchan, session)
	assert.Equal(t,remoteNode.PublicKey().String(),con.remotePub.String(),"Remote connection was not updated properly")

	othercon := NewConnectionMock(remoteNode.PublicKey(), Remote)
	othercon.SetSendResult(fmt.Errorf("error or whatever"))
	err = remoteNet.HandlePreSessionIncomingMessage(othercon, data)
	assert.Error(t, err, "handle session failed")
	assert.Nil(t, othercon.Session())
	waitForCallbackOrTimeout(t, outchan, nil)
}
