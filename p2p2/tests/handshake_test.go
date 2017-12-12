package tests

import (
	"encoding/hex"
	"github.com/UnrulyOS/go-unruly/assert"
	"github.com/UnrulyOS/go-unruly/log"
	"github.com/UnrulyOS/go-unruly/p2p2"
	"testing"
)

func TestHnadshake(t *testing.T) {

	// node 1
	priv, pub, _ := p2p2.GenerateKeyPair()
	node1Local := p2p2.NewLocalNode(pub, priv, "127.0.0.1:3030")
	node1Remote, _ := p2p2.NewRemoteNode(pub.String() ,"127.0.0.1:3030")

	// node 2
	priv1, pub1, _ := p2p2.GenerateKeyPair()
	node2Remote, _ := p2p2.NewRemoteNode(pub1.String(), "127.0.0.1:3033")
	node2Local := p2p2.NewLocalNode(pub1, priv1, "127.0.0.1:3033")

	// Node1 generates handshake data and sends it to node2 ....
	data, session, err := p2p2.GenereateHandshakeRequestData(node1Local, node2Remote)

	assert.NoErr(t, err, "expected no error")
	assert.NotNil(t, session, "expected session")
	assert.NotNil(t, data, "expected session")

	log.Info("Node 1 session data: IV:%s, AES-KEY:%s", hex.EncodeToString(session.Iv()), hex.EncodeToString(session.Key()))

	// Node2 gets handshake data from node 1 and processes it to establish a session with a shared AES key
	data1, session1, err := p2p2.ProcessHandshakeRequest(node2Local, node1Remote, data)

	assert.NoErr(t, err, "expected no error")
	assert.NotNil(t, session1, "expected session")
	assert.NotNil(t, data1, "expected session")

	log.Info("Node 2 session data: IV:%s, AES-KEY:%s", hex.EncodeToString(session1.Iv()), hex.EncodeToString(session1.Key()))

	assert.Equal(t, string(session.Iv()), string(session1.Iv()), "Epxected agreed IV")
	assert.Equal(t, string(session.Key()), string(session1.Key()), "Epxected same shared AES key")

}
