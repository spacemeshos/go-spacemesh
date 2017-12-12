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

	// STEP 1: Node1 generates handshake data and sends it to node2 ....
	data, session, err := p2p2.GenereateHandshakeRequestData(node1Local, node2Remote)

	assert.NoErr(t, err, "expected no error")
	assert.NotNil(t, session, "expected session")
	assert.NotNil(t, data, "expected session")

	log.Info("Node 1 session data: IV:%s, AES-KEY:%s", hex.EncodeToString(session.Iv()), hex.EncodeToString(session.KeyE()))

	// STEP 2: Node2 gets handshake data from node 1 and processes it to establish a session with a shared AES key
	resp, session1, err := p2p2.ProcessHandshakeRequest(node2Local, node1Remote, data)

	assert.NoErr(t, err, "expected no error")
	assert.NotNil(t, session1, "expected session")
	assert.NotNil(t, resp, "expected resp data")

	log.Info("Node 2 session data: IV:%s, AES-KEY:%s", hex.EncodeToString(session1.Iv()), hex.EncodeToString(session1.KeyE()))

	assert.Equal(t, string(session.Iv()), string(session1.Iv()), "epxected agreed IV")
	assert.Equal(t, string(session.KeyE()), string(session1.KeyE()), "epxected same shared AES enc key")
	assert.Equal(t, string(session.KeyM()), string(session1.KeyM()), "epxected same shared AES mac key")
	assert.Equal(t, string(session.SharedSecret()), string(session1.SharedSecret()), "epxected same shared secret")

	// STEP 3: Node2 sends data1 back to node1.... Node 1 validates the data and sets its network session to authenticated

	err = p2p2.ProcessHandshakeResponse(node1Local, node2Remote, session, resp)

	assert.NoErr(t, err, "failed to authenticate or process response")

}
