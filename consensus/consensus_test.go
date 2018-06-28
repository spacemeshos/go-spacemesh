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
	"github.com/spacemeshos/go-spacemesh/consensus/pb"
	"github.com/gogo/protobuf/proto"
)

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

type MaliciousSender struct{
	DolevStrongConsensus
}

func (mal *MaliciousSender) StartInstance(msg OpaqueMessage, msg2 OpaqueMessage) []byte {
	mal.startTime = time.Now()
	go mal.SendMessage(msg2)
	go mal.SendMessage(msg)
	go mal.StartListening()
	mal.waitForConsensus()
	return msg.Data()
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

func (tm MyTimer) Since(sincetime time.Time) time.Duration {
	return time.Since(sincetime)
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

func createNodesAndIds(numOfNodes int) ([]string, []Node){
	nodeIds := make([]string, numOfNodes)
	nodes := make([]Node, numOfNodes)

	log.Info("creating new nodes")
	for i := 0; i < numOfNodes; i++ {
		node := NewNode()
		nodes[i] = node
		nodeIds[i] = node.publicKey.String()
		log.Info("creating new node %v", node.publicKey.String())
	}

	return nodeIds,nodes
}

func TestSanity(t *testing.T) {
	numOfNodes := 20
	nodeIds, nodes := createNodesAndIds(numOfNodes)
	var dsInstances []Algorithm

	cfg := config.DefaultConfig()
	cfg.NodesPerLayer = int32(numOfNodes)
	cfg.NumOfAdverseries = int32(numOfNodes / 2)
	timer := MyTimer{}

	mockNetwork := NewMockNetwork()
	quit := mockNetwork.routeMessages()
	zeroBuf := make([]byte, 0)



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
		validateDSC(t, dsc, arr, nodeIds[i], nodeIds[numOfNodes-1], true)
		i++
	}

}

func TestNodeNotInListJoins(t *testing.T) {
	numOfNodes := 10
	var dsInstances []Algorithm

	cfg := config.DefaultConfig()
	cfg.NodesPerLayer = int32(numOfNodes)
	cfg.NumOfAdverseries = int32(numOfNodes / 2)
	timer := MyTimer{}

	mockNetwork := NewMockNetwork()
	quit := mockNetwork.routeMessages()
	zeroBuf := make([]byte, 0)

	nodeIds, nodes := createNodesAndIds(numOfNodes)

	for i := 0; i < numOfNodes; i++ {
		ds := initDSC(mockNetwork, nodeIds[:numOfNodes-1], nodes[i], cfg, timer)
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
		validateDSC(t, dsc, arr, nodeIds[i], nodeIds[numOfNodes-1], true)
		i++
	}

}

func TestSenderSendsTwoMessages(t *testing.T) {
	numOfNodes := 8

	var dsInstances []Algorithm

	cfg := config.DefaultConfig()
	cfg.NodesPerLayer = int32(numOfNodes)
	cfg.NumOfAdverseries = int32(numOfNodes / 2)
	timer := MyTimer{}

	mockNetwork := NewMockNetwork()
	quit := mockNetwork.routeMessages()
	zeroBuf := make([]byte, 0)

	nodeIds, nodes := createNodesAndIds(numOfNodes)

	for i := 0; i < numOfNodes -1; i++ {
		ds := initDSC(mockNetwork, nodeIds, nodes[i], cfg, timer)
		dsInstances = append(dsInstances, ds)
	}

	for i := 0; i < numOfNodes-1; i++ {
		x := messageData(zeroBuf)
		//start instance blocks until protocol is finished
		go dsInstances[i].StartInstance(x)
	}

	node := nodes[numOfNodes -1]
	nodeMock := mockNetwork.createNewNodeNetMock(node.publicKey.String())
	mal, _ := initMaliciousDSC(cfg, timer, nodeIds, nodeMock, node.publicKey, node.privateKey)

	//mal, _ := initMaliciousDSC(mockNetwork, nodeIds, nodes[numOfNodes -1], cfg, timer)

	arr := []byte{'l', 'o', 'l'}
	msg := messageData(arr)
	msg2 := messageData([]byte("lol2"))
	mal.StartInstance(msg, msg2)

	log.Info("closing network")
	quit <- struct{}{}
	i := 0
	for _, dsc := range dsInstances {
		validateDSC(t, dsc, arr, nodeIds[i], nodeIds[numOfNodes-1], false)
		i++
	}

}

func initDolevStrongConsensus(numOfNodes int) *DolevStrongConsensus{
	cfg := config.DefaultConfig()
	cfg.NodesPerLayer = int32(numOfNodes)
	cfg.NumOfAdverseries = int32(numOfNodes / 2)
	timer := MyTimer{}

	mockNetwork := NewMockNetwork()
	//quit := mockNetwork.routeMessages()
	node := NewNode()

	nodeMock := mockNetwork.createNewNodeNetMock(node.publicKey.String())
	dolevStrong, _ := createDSC(cfg,timer,nil,nodeMock,node.publicKey,node.privateKey)
	return dolevStrong
}

func TestInvalidPublicKeyOfInitiatorMessage(t *testing.T){
	numOfNodes := 10
	dolevStrong := initDolevStrongConsensus(numOfNodes)

	arr := messageData([]byte("anton"))
	msg := createMessage(arr ,dolevStrong)

	// flip a byte in the pub key to fail
	msg.Msg.AuthPubKey[0] = ^msg.Msg.AuthPubKey[0]

	bt, _ := proto.Marshal(&msg)
	ms := messageData(bt)
	err := dolevStrong.handleMessage(ms)

	assert.Error(t, err)
}

func TestInvalidSignature(t *testing.T){
	numOfNodes := 10
	dolevStrong := initDolevStrongConsensus(numOfNodes)
	msg := createMessage(messageData([]byte("anton")), dolevStrong)
	dolevStrong.signMessageAndAppend(&msg)
	msg.Msg.Data[0] = ^msg.Msg.Data[0]
	instance := DolevStrongInstance{
		ds:dolevStrong,
		initiator:nil,
	}
	_, err := instance.findAndValidateSignatures(&msg)
	assert.Error(t,err)
}

func validateDSC(t *testing.T, dsc Algorithm, expected []byte, node string, senderInstance string, shouldExist bool) {
	outputs := dsc.GetOtherInstancesOutput()
	for key, out := range outputs {
		if key == senderInstance && node != senderInstance {
			if shouldExist{
				assert.True(t, bytes.Equal(out, expected), "buffer not found in any instance ", node)
			} else {
				assert.False(t, bytes.Equal(out, expected), "buffer found in instance ", node)
			}

		}
	}

}



func initDSC(mockNetwork *MockNetwork, activeNodes []string, node Node, cfg config.Config, timer MyTimer) Algorithm {
	nodeMock := mockNetwork.createNewNodeNetMock(node.publicKey.String())
	ds, _ := NewDSC(cfg, timer, activeNodes, nodeMock, node.publicKey, node.privateKey)
	return ds
}

// NewDSC creates a new instance of dolev strong agreement protocol
func createDSC(conf config.Config,
	timer Timer,
	nodes []string,
	network NetworkConnection,
	pubKey crypto.PublicKey,
	privKey crypto.PrivateKey) (*DolevStrongConsensus, error) {

	dsc := &DolevStrongConsensus{
		activeInstances: make(map[string]CAInstance),
		ingressChannel:  make(chan OpaqueMessage),
		publicKey:       pubKey,
		privateKey:      privKey,
		net:             network,
		dsConfig:        conf,
		timer:           timer,
		startTime:       time.Now(),
		abortInstance:   make(chan struct{}, 1),
		abortListening:  make(chan struct{}, 1),
	}
	for _, node := range nodes {
		dsc.activeInstances[node] = NewCAInstance(dsc)
	}

	return dsc, nil
}

func initMaliciousDSC(conf config.Config,
	timer Timer,
	nodes []string,
	network NetworkConnection,
	pubKey crypto.PublicKey,
	privKey crypto.PrivateKey) (*MaliciousSender, error) {

	dsc := DolevStrongConsensus{
		activeInstances: make(map[string]CAInstance),
		ingressChannel:  make(chan OpaqueMessage),
		publicKey:       pubKey,
		privateKey:      privKey,
		net:             network,
		dsConfig:        conf,
		timer:           timer,
		startTime:       time.Now(),
		abortInstance:   make(chan struct{}, 1),
		abortListening:  make(chan struct{}, 1),
	}

	mal := &MaliciousSender{
		dsc,
	}
	for _, node := range nodes {
		dsc.activeInstances[node] = NewCAInstance(&dsc)
	}

	return mal, nil
}


func createMessage(msg OpaqueMessage, impl *DolevStrongConsensus) pb.ConsensusMessage{
	dataMsg := pb.MessageData{
		Data:       msg.Data(),
		AuthPubKey: impl.publicKey.Bytes(),
	}
	protocolMsg := pb.ConsensusMessage{
		Msg:        &dataMsg,
		Validators: []*pb.Validator{},
	}
	err := impl.signMessageAndAppend(&protocolMsg)
	if err != nil {
		log.Error("cannot sign message %v", protocolMsg)
	}

	return protocolMsg
	//except := make(map[string]struct{})
	//except[impl.publicKey.String()] = struct{}{}

}