package net

import (
	"fmt"
	"github.com/gogo/protobuf/proto"
	"github.com/spacemeshos/go-spacemesh/log"
	"github.com/spacemeshos/go-spacemesh/p2p/config"
	"github.com/spacemeshos/go-spacemesh/p2p/node"
	"github.com/stretchr/testify/assert"
	"math/rand"
	"sync"
	"testing"
	"time"
)

func testLogger(id string) log.Log {
	// empty loggers so no files will be kept
	return log.New(id, "", "")
}

type NetMessage struct {
	data []byte
	err  error
}

func (msg NetMessage) Message() []byte {
	return msg.data
}

func (msg NetMessage) Error() error {
	return msg.err
}

func waitForCallbackOrTimeout(t *testing.T, outchan chan NewConnectionEvent, expectedSession NetworkSession) {
	select {
	case res := <-outchan:
		assert.Equal(t, expectedSession.ID(), res.Conn.Session().ID(), "wrong session received")
	case <-time.After(1 * time.Second):
		assert.Nil(t, expectedSession, "Didn't get channel notification")
	}
}

func Test_sumByteArray(t *testing.T) {
	bytez := sumByteArray([]byte{0x1, 0x1, 0x1, 0x1, 0x1, 0x1, 0x1, 0x1, 0x1, 0x1, 0x1, 0x1, 0x1, 0x1, 0x1, 0x1, 0x1, 0x1, 0x1, 0x1})
	assert.Equal(t, bytez, uint(20))
	bytez2 := sumByteArray([]byte{0x5, 0x5, 0x5, 0x5, 0x5, 0x5, 0x5, 0x5, 0x5, 0x5, 0x5, 0x5, 0x5, 0x5, 0x5, 0x5, 0x5, 0x5, 0x5, 0x5})
	assert.Equal(t, bytez2, uint(100))
}

func TestNet_EnqueueMessage(t *testing.T) {
	testnodes := 100
	cfg := config.DefaultConfig()
	ln, err := node.NewNodeIdentity(cfg, "0.0.0.0:0000", false)
	assert.NoError(t, err)
	n, err := NewNet(cfg, ln)
	assert.NoError(t, err)
	rnd := rand.New(rand.NewSource(time.Now().UnixNano()))
	var wg sync.WaitGroup
	for i := 0; i < testnodes; i++ {
		wg.Add(1)
		go func() {
			rnode := node.GenerateRandomNodeData()
			sum := sumByteArray(rnode.PublicKey().Bytes())
			msg := make([]byte, 10, 10)
			rnd.Read(msg)
			n.EnqueueMessage(IncomingMessageEvent{NewConnectionMock(rnode.PublicKey()), msg})
			s := <-n.IncomingMessages()[sum%n.queuesCount]
			assert.Equal(t, s.Message, msg)
			assert.Equal(t, s.Conn.RemotePublicKey(), rnode.PublicKey())
			wg.Done()
		}()
	}
	wg.Wait()
}

func TestHandlePreSessionIncomingMessage(t *testing.T) {
	//cfg := config.DefaultConfig()
	localNode, _ := node.GenerateTestNode(t)
	remoteNode, _ := node.GenerateTestNode(t)
	con := NewConnectionMock(localNode.PublicKey())
	remoteNet, _ := NewNet(config.ConfigValues, remoteNode)
	outchan := remoteNet.SubscribeOnNewRemoteConnections()
	out, session, er := GenerateHandshakeRequestData(localNode.PublicKey(), localNode.PrivateKey(), remoteNode.PublicKey(), remoteNet.NetworkID(), getPort(t, remoteNode.Node))
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
	assert.Equal(t, localNode.PublicKey().String(), con.remotePub.String(), "Remote connection was not updated properly")

	othercon := NewConnectionMock(remoteNode.PublicKey())
	othercon.SetSendResult(fmt.Errorf("error or whatever"))
	err = remoteNet.HandlePreSessionIncomingMessage(othercon, data)
	assert.Error(t, err, "handle session failed")
	assert.Nil(t, othercon.Session())
	waitForCallbackOrTimeout(t, outchan, nil)

	out.NetworkID = out.NetworkID + 1
	data, err = proto.Marshal(out)
	assert.NoError(t, err, "cannot marshal obj")
	err = remoteNet.HandlePreSessionIncomingMessage(con, data)
	assert.Error(t, err, "Sent message with wrong networkID")
	//,_, er = GenerateHandshakeRequestData(localNode.PublicKey(), localNode.PrivateKey(),con.RemotePublicKey(), remoteNet.NetworkID() +1)
}
