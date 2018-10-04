package net

import (
	"fmt"
	"github.com/spacemeshos/go-spacemesh/p2p/config"
	"github.com/spacemeshos/go-spacemesh/p2p/node"
	"github.com/spacemeshos/go-spacemesh/p2p/pb"
	"github.com/stretchr/testify/assert"
	"testing"
)

func TestGenerateHandshakeRequestData(t *testing.T) {
	port := 0
	address := fmt.Sprintf("0.0.0.0:%d", port)
	localNode, err := node.NewLocalNode(config.ConfigValues, address, false)
	assert.NoError(t, err, "should be able to create localnode")
	remoteNode, err := node.NewLocalNode(config.ConfigValues, address, false)
	assert.NoError(t, err, "should be able to create localnode")
	con := NewConnectionMock(remoteNode.PublicKey())
	remoteNet, _ := NewNet(config.ConfigValues, remoteNode)
	//outchan := remoteNet.SubscribeOnNewRemoteConnections()
	_, _, er := GenerateHandshakeRequestData(localNode.PublicKey(), localNode.PrivateKey(), con.RemotePublicKey(), remoteNet.NetworkID())
	assert.NoError(t, er, "Sanity failed")

}

func generateRequestData(t *testing.T) (*pb.HandshakeData, node.LocalNode, node.LocalNode, int8) {

	localNode, _ := node.GenerateTestNode(t)
	remoteNode, _ := node.GenerateTestNode(t)
	netId := int8(1)
	con := NewConnectionMock(remoteNode.PublicKey())
	//outchan := remoteNet.SubscribeOnNewRemoteConnections()
	out, _, err := GenerateHandshakeRequestData(localNode.PublicKey(), localNode.PrivateKey(), con.RemotePublicKey(), netId)
	assert.NoError(t, err, "Failed to generate request")
	return out, *localNode, *remoteNode, netId
}

func TestProcessHandshakeRequest(t *testing.T) {
	//Sanity
	data, localNode, remoteNet, netId := generateRequestData(t)
	//processing request in remoteNet from local node
	_, _, err := ProcessHandshakeRequest(netId, remoteNet.PublicKey(), remoteNet.PrivateKey(), localNode.PublicKey(), data)
	assert.NoError(t, err, "Sanity processing request failed", err)

	_, _, err = ProcessHandshakeRequest(netId, remoteNet.PublicKey(), remoteNet.PrivateKey(), localNode.PublicKey(), data)
	assert.NoError(t, err, "Data modified during test")

	data.NetworkID = data.NetworkID + 1
	_, _, err = ProcessHandshakeRequest(netId, remoteNet.PublicKey(), remoteNet.PrivateKey(), localNode.PublicKey(), data)
	assert.Error(t, err, "Didnt receive error on network id incomaptible with request")
	data.NetworkID = int32(netId)

	//remoteNode, _ := node.GenerateTestNode(t)

	_, _, err = ProcessHandshakeRequest(netId, remoteNet.PublicKey(), remoteNet.PrivateKey(), remoteNet.PublicKey(), data)
	assert.Error(t, err, "Didnt receive error on remote public key incomaptible with request")

}

func TestProcessHandshakeResponse(t *testing.T) {
	//Sanity
	data, localNode, remoteNet, netId := generateRequestData(t)
	reqMsg, session, err := ProcessHandshakeRequest(netId, remoteNet.PublicKey(), remoteNet.PrivateKey(), localNode.PublicKey(), data)
	assert.NoError(t, err, "Sanity creating request failed")

	er := ProcessHandshakeResponse(remoteNet.PublicKey(), session, reqMsg)
	assert.NoError(t, er, "Sanity processing response failed")

	er = ProcessHandshakeResponse(localNode.PublicKey(), session, reqMsg)
	assert.Error(t, er, "remote key signing verification of response failed")

	er = ProcessHandshakeResponse(remoteNet.PublicKey(), session, reqMsg)
	assert.NoError(t, er, "Sanity processing response failed")
}
