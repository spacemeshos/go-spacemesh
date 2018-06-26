package consensus

import (
	"bytes"
	"github.com/spacemeshos/go-spacemesh/consensus/config"
	"github.com/spacemeshos/go-spacemesh/crypto"
	"github.com/spacemeshos/go-spacemesh/log"
	"github.com/stretchr/testify/assert"
	"sync"
	"testing"
	"time"
)

func TestDolevStrongConsensus_GetRound(t *testing.T) {

}

type messageData []byte

type MockNetworkMessage struct {
	data   messageData
	sender string
}

type MockNetwork struct {
	connections  map[string]chan OpaqueMessage
	inputChannel chan MockNetworkMessage
	mutex        sync.Mutex
}

func (msg messageData) Data() []byte {
	return msg
}

func NewMockNetwork() *MockNetwork {
	val := &MockNetwork{
		connections:  make(map[string]chan OpaqueMessage),
		inputChannel: make(chan MockNetworkMessage, 0),
		mutex:        sync.Mutex{},
	}
	return val
}

func (net *MockNetwork) createNewNodeNetMock(id string) *NodeNetMock {
	nodeNet := &NodeNetMock{
		receiveChannel: make(chan OpaqueMessage),
		sendChannel:    net.inputChannel,
		id:             id,
	}
	net.mutex.Lock()
	net.connections[id] = nodeNet.receiveChannel
	net.mutex.Unlock()

	return nodeNet
}

func (net *MockNetwork) routeMessages() chan struct{} {
	quit := make(chan struct{})
	go func() {
		for {
			select {
			case msg := <-net.inputChannel:
				log.Info("recevied message")
				x, ok := net.connections[msg.sender]
				if !ok {
					log.Error("Peer not found")
				} else {
					x <- msg.data
				}
			case <-quit:
				log.Info("QUIT")
				break

			}
		}
	}()
	return quit
}

type NodeNetMock struct {
	receiveChannel chan OpaqueMessage
	sendChannel    chan MockNetworkMessage
	id             string
}

func (net *NodeNetMock) SendMessage(message []byte, addr string) (int, error) {
	log.Info("node %v sent data to %v", net.id, addr)
	data := messageData(message)
	networkMessage := MockNetworkMessage{data, addr}
	go func() {
		net.sendChannel <- networkMessage
		log.Info("finished sending %v <-> %v", net.id, addr)
	}()

	return 0, nil
}

func (net *NodeNetMock) RegisterProtocol(protocolName string) chan OpaqueMessage {
	return net.receiveChannel
}

type MyTimer struct{}

func (tm MyTimer) GetTime() time.Time {
	return time.Now()
}

type Node struct {
	publicKey  crypto.PublicKey
	privateKey crypto.PrivateKey
}

func NewNode() Node {
	priv, pub, err := crypto.GenerateKeyPair()
	if err != nil {
		log.Error("failed to crate pub priv key", err)
	}
	return Node{pub, priv}
}

func TestSanity(t *testing.T) {
	numOfNodes := 20
	nodeIds := make([]string, numOfNodes)
	nodes := make([]Node, numOfNodes)
	var dsInstances []ConsensusAlgorithm

	cfg := config.DefaultConfig()
	cfg.NodesPerLayer = int32(numOfNodes)
	cfg.NumOfAdverseries = int32(numOfNodes / 2)
	timer := MyTimer{}

	mockNetwork := NewMockNetwork()
	quit := mockNetwork.routeMessages()
	zeroBuf := make([]byte, 0)

	log.Info("creating new nodes")
	for i := 0; i < numOfNodes; i++ {
		node := NewNode()
		nodes[i] = node
		nodeIds[i] = node.publicKey.String()
		log.Info("creating new node %v", node.publicKey.String())
	}

	for i := 0; i < numOfNodes; i++ {
		ds := initDSC(mockNetwork, nodeIds, nodes[i], cfg, timer)
		dsInstances = append(dsInstances, ds)
	}

	for i := 0; i < numOfNodes-1; i++ {
		x := messageData(zeroBuf)
		//start instance blocks until protocol is finished
		go dsInstances[i].StartInstance(x)
	}

	arr := []byte{'l', 'o', 'l'}
	msg := messageData(arr)
	dsInstances[numOfNodes-1].StartInstance(msg)

	log.Info("closing network")
	quit <- struct{}{}
	i := 0
	for _, dsc := range dsInstances {
		validateDSC(t, dsc, arr, nodeIds[i], nodeIds[numOfNodes-1])
		i++
	}

}

func validateDSC(t *testing.T, dsc ConsensusAlgorithm, expected []byte, node string, senderInstance string) {
	outputs := dsc.GetOtherInstancesOutput()
	for key, out := range outputs {
		if key == senderInstance && node != senderInstance {
			assert.True(t, bytes.Equal(out, expected), "buffer not found in any instance ", node)
		}
	}

}

func initDSC(mockNetwork *MockNetwork, activeNodes []string, node Node, cfg config.Config, timer MyTimer) ConsensusAlgorithm {
	nodeMock := mockNetwork.createNewNodeNetMock(node.publicKey.String())
	ds, _ := NewDSC(cfg, timer, activeNodes, nodeMock, node.publicKey, node.privateKey)
	return ds
}
