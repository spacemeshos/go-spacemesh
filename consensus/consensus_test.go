package consensus

import (
	"bytes"
	"fmt"
	"github.com/gogo/protobuf/proto"
	"github.com/spacemeshos/go-spacemesh/consensus/config"
	"github.com/spacemeshos/go-spacemesh/consensus/pb"
	"github.com/spacemeshos/go-spacemesh/crypto"
	"github.com/spacemeshos/go-spacemesh/log"
	"github.com/stretchr/testify/assert"
	"math/rand"
	"sync"
	"testing"
	"time"
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
	delayMs      int32
}

type MaliciousTwoValuesSender struct {
	DolevStrongMultiInstanceConsensus
}

func (mal *MaliciousTwoValuesSender) StartInstance(msg OpaqueMessage, msg2 OpaqueMessage) []byte {
	mal.startTime = time.Now()
	go mal.SendMessage(msg2)
	go mal.SendMessage(msg)
	go mal.StartListening()
	mal.waitForConsensus()
	return msg.Data()
}

type MaliciousSplitMessageSender struct {
	DolevStrongMultiInstanceConsensus
}

func (mal *MaliciousSplitMessageSender) StartInstance(msg1 OpaqueMessage, group1 []string, msg2 OpaqueMessage, group2 []string) []byte {
	mal.startTime = time.Now()
	log.Info("sending group1 : %v, group2: %v", group1, group2)
	go mal.SendMessage(msg1, group1)
	go mal.SendMessage(msg2, group2)
	go mal.StartListening()
	mal.waitForConsensus()
	return nil
}

// SendMessage sends this message in order to reach consensus
func (mal *MaliciousSplitMessageSender) SendMessage(msg OpaqueMessage, group1 []string) error {
	if len(msg.Data()) == 0 {
		return nil
	}
	dataMsg := pb.MessageData{
		Data:       msg.Data(),
		AuthPubKey: mal.publicKey.Bytes(),
	}
	protocolMsg := pb.ConsensusMessage{
		Msg:        &dataMsg,
		Validators: []*pb.Validator{},
	}
	err := mal.signMessageAndAppend(&protocolMsg)
	if err != nil {
		log.Error("cannot sign message %v", protocolMsg)
		return err
	}
	data, ok := proto.Marshal(&protocolMsg)
	if ok != nil {
		log.Error("could not marshal output message", protocolMsg)
	}
	for _, key := range group1 {
		//don't send to signed validators
		_, err := mal.net.SendMessage(data, key)
		if err != nil {
			//todo: should we break the loop now?
			//return err
		}
	}
	return nil
}

type MaliciousSignedTwoTimesSender struct {
	DolevStrongMultiInstanceConsensus
}

// SendMessage sends this message in order to reach consensus
func (impl *MaliciousSignedTwoTimesSender) SendMessage(msg OpaqueMessage) error {
	if len(msg.Data()) == 0 {
		return nil
	}
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
		return err
	}
	err = impl.signMessageAndAppend(&protocolMsg)
	if err != nil {
		log.Error("cannot sign message %v", protocolMsg)
		return err
	}
	except := make(map[string]struct{})
	except[impl.publicKey.String()] = struct{}{}
	impl.sendToRemainingNodes(&protocolMsg, except)
	return nil
}

type MaliciousDelayedSecondMessageSender struct {
	DolevStrongMultiInstanceConsensus
}

func (mal *MaliciousDelayedSecondMessageSender) StartInstance(msg OpaqueMessage, msg2 OpaqueMessage) []byte {
	mal.startTime = time.Now()
	go mal.SendMessage(msg2)
	time.Sleep(time.Duration(mal.dsConfig.NumOfAdversaries/2) * mal.dsConfig.RoundTime)
	go mal.SendMessage(msg)
	go mal.StartListening()
	mal.waitForConsensus()
	return msg.Data()
}

type MaliciousChangeMessageReceiver struct {
	DolevStrongInstance
}

// ReceiveMessage is the main receiver handler for a message received from a dolev strong instance running in the network
func (dsci *MaliciousChangeMessageReceiver) ReceiveMessage(message *pb.ConsensusMessage) error {
	//calculates round according to local clock
	round := dsci.ds.getRound()
	log.Info("received message in round %v num of signatures: %v", round, len(message.Validators))

	if dsci.abortProcessingInstance {
		return fmt.Errorf("message received on aborted isntance, round: %v", round)
	}

	if round > dsci.ds.dsConfig.NumOfAdversaries+1 {
		return fmt.Errorf("round out of order: %v", round)
	}

	publicKeys, err := dsci.findAndValidateSignatures(message)
	if err != nil {
		return err
	}
	/*initiatorPubKey, err := crypto.NewPublicKey(message.Msg.AuthPubKey)
	if _, ok := publicKeys[initiatorPubKey.String()]; !ok || err != nil {
		return fmt.Errorf("initiator did not sign the message - aborting")

	}
	//check if there are enough signature to match round number
	if len(publicKeys) < int(round) {
		return fmt.Errorf("invalid number of signatures - not matching round number %v num of signatures %v", round, len(publicKeys))
	}
	//todo: what if the signatures are of nodes that are not in the layer?
	if dsci.value != nil {
		if res := bytes.Compare(dsci.value, message.Msg.Data); res != 0 {
			dsci.abortProcessingInstance = true //with abort
			return fmt.Errorf("received two different messages from same sender, aborting this instance")
		}
		// no need to do anything
		return nil
	}*/
	dsci.value = message.Msg.Data
	message.Msg.Data[1] = ^message.Msg.Data[1]

	// add our signature on the message
	err = dsci.ds.signMessageAndAppend(message)
	if err != nil {
		return fmt.Errorf("cannot sign message %v error %v", message, err)
	}
	//add this node to the nodes already signed
	publicKeys[dsci.ds.publicKey.String()] = struct{}{}
	dsci.ds.sendToRemainingNodes(message, publicKeys) // to be sent in round +1
	return nil
}

type MaliciousHoldMessageReceiver struct {
	DolevStrongInstance
}

// ReceiveMessage is the main receiver handler for a message received from a dolev strong instance running in the network
func (dsci *MaliciousHoldMessageReceiver) ReceiveMessage(message *pb.ConsensusMessage) error {
	//calculates round according to local clock
	round := dsci.ds.getRound()
	log.Info("received message in round %v num of signatures: %v", round, len(message.Validators))

	if dsci.abortProcessingInstance {
		return fmt.Errorf("message received on aborted isntance, round: %v", round)
	}

	if round > dsci.ds.dsConfig.NumOfAdversaries+1 {
		return fmt.Errorf("round out of order: %v", round)
	}

	publicKeys, err := dsci.findAndValidateSignatures(message)
	if err != nil {
		return err
	}
	if dsci.value != nil {
		if res := bytes.Compare(dsci.value, message.Msg.Data); res != 0 {
			dsci.abortProcessingInstance = true //with abort
			return fmt.Errorf("received two different messages from same sender, aborting this instance")
		}
		// no need to do anything
		return nil
	}
	dsci.value = message.Msg.Data

	// add our signature on the message
	err = dsci.ds.signMessageAndAppend(message)
	if err != nil {
		return fmt.Errorf("cannot sign message %v error %v", message, err)
	}
	//add this node to the nodes already signed
	publicKeys[dsci.ds.publicKey.String()] = struct{}{}
	time.Sleep(5 * time.Second)
	dsci.ds.sendToRemainingNodes(message, publicKeys) // to be sent in round +1
	return nil
}

type MaliciousSendHoldMessageReceiver struct {
	DolevStrongInstance
}

// ReceiveMessage is the main receiver handler for a message received from a dolev strong instance running in the network
func (dsci *MaliciousSendHoldMessageReceiver) ReceiveMessage(message *pb.ConsensusMessage) error {
	//calculates round according to local clock
	round := dsci.ds.getRound()

	if dsci.abortProcessingInstance {
		return fmt.Errorf("message received on aborted isntance, round: %v", round)
	}

	if round > dsci.ds.dsConfig.NumOfAdversaries+1 {
		return fmt.Errorf("round out of order: %v", round)
	}

	publicKeys, err := dsci.findAndValidateSignatures(message)
	if err != nil {
		return err
	}
	if dsci.value != nil {
		if res := bytes.Compare(dsci.value, message.Msg.Data); res != 0 {
			dsci.abortProcessingInstance = true //with abort
			return fmt.Errorf("received two different messages from same sender, aborting this instance")
		}
		// no need to do anything
		return nil
	}
	dsci.value = message.Msg.Data

	// add our signature on the message
	err = dsci.ds.signMessageAndAppend(message)
	if err != nil {
		return fmt.Errorf("cannot sign message %v error %v", message, err)
	}
	//add this node to the nodes already signed
	log.Info("Sending first message")
	publicKeys[dsci.ds.publicKey.String()] = struct{}{}
	dsci.ds.sendToRemainingNodes(message, publicKeys) // to be sent in round +1

	time.Sleep(5 * time.Second)
	log.Info("Resending message")
	dsci.ds.sendToRemainingNodes(message, publicKeys) // to be sent in round +1
	return nil
}

//func (mal *)

func (msg messageData) Data() []byte {
	return msg
}

func NewMockNetwork(delayMilliseconds int32) *MockNetwork {
	val := &MockNetwork{
		connections:  make(map[string]chan OpaqueMessage),
		inputChannel: make(chan MockNetworkMessage, 0),
		mutex:        sync.Mutex{},
		delayMs:      delayMilliseconds,
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
	messageNum := 0
	rn := rand.New(rand.NewSource(time.Now().UnixNano()))
	go func() {
		for {
			select {
			case msg := <-net.inputChannel:
				log.Info("recevied message")
				if net.delayMs > 0 {

					time.Sleep(time.Duration(rn.Int31n(net.delayMs)) * time.Millisecond)
				}
				net.mutex.Lock()
				x, ok := net.connections[msg.sender]
				net.mutex.Unlock()
				if !ok {
					log.Error("Peer not found")
				} else {
					messageNum++
					go func() {
						x <- msg.data
					}()
				}
			case <-quit:
				log.Info("QUIT, messages passed: %v", messageNum)
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
	//log.Info("node %v sent data to %v", net.id, addr)
	data := messageData(message)
	networkMessage := MockNetworkMessage{data, addr}
	go func() {
		net.sendChannel <- networkMessage
		//log.Info("finished sending %v <-> %v", net.id, addr)
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
		log.Error("failed to create pub priv key", err)
	}
	return Node{pub, priv}
}

func createNodesAndIds(numOfNodes int) ([]string, []Node) {
	nodeIds := make([]string, numOfNodes)
	nodes := make([]Node, numOfNodes)

	log.Info("creating new nodes")
	for i := 0; i < numOfNodes; i++ {
		node := NewNode()
		nodes[i] = node
		nodeIds[i] = node.publicKey.String()
		log.Info("creating new node %v", node.publicKey.String())
	}

	return nodeIds, nodes
}

func TestSanity(t *testing.T) {
	t.Skip()
	numOfNodes := 20
	nodeIds, nodes := createNodesAndIds(numOfNodes)
	var dsInstances []HareAlgorithm

	cfg := config.DefaultConfig()
	cfg.NodesPerLayer = int32(numOfNodes)
	cfg.NumOfAdversaries = int32(numOfNodes / 2)
	timer := MyTimer{}

	mockNetwork := NewMockNetwork(0)
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

func TestReceiverHoldAndResendMessage(t *testing.T) {
	t.Skip()
	numOfNodes := 20
	nodeIds, nodes := createNodesAndIds(numOfNodes)
	var dsInstances []*DolevStrongMultiInstanceConsensus

	cfg := config.DefaultConfig()
	cfg.NodesPerLayer = int32(numOfNodes)
	cfg.NumOfAdversaries = int32(numOfNodes / 2)
	timer := MyTimer{}

	mockNetwork := NewMockNetwork(0)
	quit := mockNetwork.routeMessages()
	zeroBuf := make([]byte, 0)

	for i := 0; i < numOfNodes; i++ {
		ds := initDSC(mockNetwork, nodeIds, nodes[i], cfg, timer)
		dsInstances = append(dsInstances, ds)
	}

	log.Info("HoldBack %v", nodes[numOfNodes-2].publicKey.String())
	for _, node := range nodes {
		dsInstances[numOfNodes-2].activeInstances[node.publicKey.String()] = &MaliciousSendHoldMessageReceiver{DolevStrongInstance{value: nil, ds: dsInstances[numOfNodes-2]}}
	}
	log.Info("Actual: %v", nodeIds[numOfNodes-2])
	//dsInstances[numOfNodes-2] = ds

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
		//skip the malicious node
		validateDSC(t, dsc, arr, nodeIds[i], nodeIds[numOfNodes-1], true)
		i++
	}

}

func TestReceiverHoldMessage(t *testing.T) {
	t.Skip()
	numOfNodes := 20
	nodeIds, nodes := createNodesAndIds(numOfNodes)
	var dsInstances []HareAlgorithm

	cfg := config.DefaultConfig()
	cfg.NodesPerLayer = int32(numOfNodes)
	cfg.NumOfAdversaries = int32(numOfNodes / 2)
	timer := MyTimer{}

	mockNetwork := NewMockNetwork(0)
	quit := mockNetwork.routeMessages()
	zeroBuf := make([]byte, 0)

	for i := 0; i < numOfNodes; i++ {
		ds := initDSC(mockNetwork, nodeIds, nodes[i], cfg, timer)
		dsInstances = append(dsInstances, ds)
	}

	nodeMock := mockNetwork.createNewNodeNetMock(nodes[numOfNodes-2].publicKey.String())
	ds, _ := NewDSC(cfg, timer, nodeIds, nodeMock, nodes[numOfNodes-2].publicKey, nodes[numOfNodes-2].privateKey)

	for _, node := range nodes {
		ds.activeInstances[node.publicKey.String()] = &MaliciousHoldMessageReceiver{DolevStrongInstance{value: nil, ds: ds}}
	}

	dsInstances[numOfNodes-2] = ds

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
		//skip the malicious node
		validateDSC(t, dsc, arr, nodeIds[i], nodeIds[numOfNodes-1], true)
		i++
	}
}

func TestReceiverChangeMessage(t *testing.T) {
	t.Skip()
	numOfNodes := 20
	nodeIds, nodes := createNodesAndIds(numOfNodes)
	var dsInstances []HareAlgorithm

	cfg := config.DefaultConfig()
	cfg.NodesPerLayer = int32(numOfNodes)
	cfg.NumOfAdversaries = int32(numOfNodes / 2)
	timer := MyTimer{}

	mockNetwork := NewMockNetwork(0)
	quit := mockNetwork.routeMessages()
	zeroBuf := make([]byte, 0)

	for i := 0; i < numOfNodes; i++ {
		ds := initDSC(mockNetwork, nodeIds, nodes[i], cfg, timer)
		dsInstances = append(dsInstances, ds)
	}

	nodeMock := mockNetwork.createNewNodeNetMock(nodes[numOfNodes-2].publicKey.String())
	ds, _ := NewDSC(cfg, timer, nodeIds, nodeMock, nodes[numOfNodes-2].publicKey, nodes[numOfNodes-2].privateKey)

	for _, node := range nodes {
		ds.activeInstances[node.publicKey.String()] = &MaliciousChangeMessageReceiver{DolevStrongInstance{value: nil, ds: ds}}
	}

	dsInstances[numOfNodes-2] = ds

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
		//skip the malicious node
		if i == numOfNodes-2 {
			continue
		}
		validateDSC(t, dsc, arr, nodeIds[i], nodeIds[numOfNodes-1], true)
		i++
	}

}

func startInstance(wg *sync.WaitGroup, dsc HareAlgorithm, msg messageData) {
	wg.Add(1)
	go func() {
		dsc.StartInstance(msg)
		wg.Done()
	}()
}

func TestMultipleSenders(t *testing.T) {
	t.Skip()
	numOfNodes := 20
	nodeIds, nodes := createNodesAndIds(numOfNodes)
	var dsInstances []HareAlgorithm

	cfg := config.DefaultConfig()
	cfg.NodesPerLayer = int32(numOfNodes)
	cfg.NumOfAdversaries = int32(numOfNodes / 2)
	timer := MyTimer{}

	mockNetwork := NewMockNetwork(0)
	quit := mockNetwork.routeMessages()

	var wg sync.WaitGroup
	for i := 0; i < numOfNodes; i++ {
		ds := initDSC(mockNetwork, nodeIds, nodes[i], cfg, timer)
		dsInstances = append(dsInstances, ds)
		startInstance(&wg, dsInstances[i], messageData(nodes[i].publicKey.Bytes()[8:]))
	}
	wg.Wait()

	log.Info("closing network")
	quit <- struct{}{}
	//i := 0
	for instanceIndex, dsc := range dsInstances {
		for _, node := range nodes { //if this is the initiator node, output should be produces after send message...
			if nodes[instanceIndex] != node {
				assert.NotNil(t, dsc.GetOtherInstancesOutput()[node.publicKey.String()], "value not found for %v", node.publicKey.String())
				assert.Equal(t, dsc.GetOtherInstancesOutput()[node.publicKey.String()], node.publicKey.Bytes()[8:])
			}

		}
	}

}

func TestNodeNotInListJoins(t *testing.T) {
	t.Skip()
	numOfNodes := 10
	var dsInstances []HareAlgorithm

	cfg := config.DefaultConfig()
	cfg.NodesPerLayer = int32(numOfNodes)
	cfg.NumOfAdversaries = int32(numOfNodes / 2)
	timer := MyTimer{}

	mockNetwork := NewMockNetwork(0)
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

func TestSenderSendsTwoMessagesToDifferentParties(t *testing.T) {
	t.Skip()
	numOfNodes := 8

	var dsInstances []HareAlgorithm

	cfg := config.DefaultConfig()
	cfg.NodesPerLayer = int32(numOfNodes)
	cfg.NumOfAdversaries = int32(numOfNodes / 2)
	timer := MyTimer{}

	mockNetwork := NewMockNetwork(0)
	quit := mockNetwork.routeMessages()
	zeroBuf := make([]byte, 0)

	nodeIds, nodes := createNodesAndIds(numOfNodes)

	for i := 0; i < numOfNodes-1; i++ {
		ds := initDSC(mockNetwork, nodeIds, nodes[i], cfg, timer)
		dsInstances = append(dsInstances, ds)
	}

	for i := 0; i < numOfNodes-1; i++ {
		x := messageData(zeroBuf)
		//start instance blocks until protocol is finished
		go dsInstances[i].StartInstance(x)
	}

	node := nodes[numOfNodes-1]
	nodeMock := mockNetwork.createNewNodeNetMock(node.publicKey.String())
	dsc, _ := NewDSC(cfg, timer, nodeIds, nodeMock, node.publicKey, node.privateKey)
	mal := MaliciousSplitMessageSender{*dsc}

	//mal, _ := initMaliciousDSC(mockNetwork, nodeIds, nodes[numOfNodes -1], cfg, timer)

	arr := []byte{'l', 'o', 'l'}
	msg := messageData(arr)
	msg2 := messageData([]byte("lol2"))
	mal.StartInstance(msg, nodeIds[:numOfNodes/2], msg2, nodeIds[numOfNodes/2:numOfNodes-1])

	log.Info("closing network")
	quit <- struct{}{}
	i := 0
	for _, dsc := range dsInstances {
		validateDSC(t, dsc, arr, nodeIds[i], nodeIds[numOfNodes-1], false)
		i++
	}

}

func TestSenderSignsTwoTimes(t *testing.T) {
	t.Skip()
	numOfNodes := 8

	var dsInstances []HareAlgorithm

	cfg := config.DefaultConfig()
	cfg.NodesPerLayer = int32(numOfNodes)
	cfg.NumOfAdversaries = int32(numOfNodes / 2)
	timer := MyTimer{}

	mockNetwork := NewMockNetwork(0)
	quit := mockNetwork.routeMessages()
	zeroBuf := make([]byte, 0)

	nodeIds, nodes := createNodesAndIds(numOfNodes)

	for i := 0; i < numOfNodes-1; i++ {
		ds := initDSC(mockNetwork, nodeIds, nodes[i], cfg, timer)
		dsInstances = append(dsInstances, ds)
	}

	for i := 0; i < numOfNodes-1; i++ {
		x := messageData(zeroBuf)
		//start instance blocks until protocol is finished
		go dsInstances[i].StartInstance(x)
	}

	node := nodes[numOfNodes-1]
	nodeMock := mockNetwork.createNewNodeNetMock(node.publicKey.String())
	dsc, _ := NewDSC(cfg, timer, nodeIds, nodeMock, node.publicKey, node.privateKey)
	mal := MaliciousSignedTwoTimesSender{*dsc}

	arr := []byte{'l', 'o', 'l'}
	msg := messageData(arr)
	mal.StartInstance(msg)

	log.Info("closing network")
	quit <- struct{}{}
	i := 0
	for _, dsc := range dsInstances {
		validateDSC(t, dsc, arr, nodeIds[i], nodeIds[numOfNodes-1], true)
		i++
	}

}

func TestSenderSendsTwoMessages(t *testing.T) {
	t.Skip()
	numOfNodes := 8

	var dsInstances []HareAlgorithm

	cfg := config.DefaultConfig()
	cfg.NodesPerLayer = int32(numOfNodes)
	cfg.NumOfAdversaries = int32(numOfNodes / 2)
	timer := MyTimer{}

	mockNetwork := NewMockNetwork(0)
	quit := mockNetwork.routeMessages()
	zeroBuf := make([]byte, 0)

	nodeIds, nodes := createNodesAndIds(numOfNodes)

	for i := 0; i < numOfNodes-1; i++ {
		ds := initDSC(mockNetwork, nodeIds, nodes[i], cfg, timer)
		dsInstances = append(dsInstances, ds)
	}

	for i := 0; i < numOfNodes-1; i++ {
		x := messageData(zeroBuf)
		//start instance blocks until protocol is finished
		go dsInstances[i].StartInstance(x)
	}

	node := nodes[numOfNodes-1]

	mal := &MaliciousTwoValuesSender{
		*initDSC(mockNetwork, nodeIds, node, cfg, timer),
	}

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

func TestSenderSendsTwoMessagesWithDelay(t *testing.T) {
	t.Skip()
	numOfNodes := 20

	var dsInstances []HareAlgorithm

	cfg := config.DefaultConfig()
	cfg.NodesPerLayer = int32(numOfNodes)
	cfg.NumOfAdversaries = int32(numOfNodes / 2)
	timer := MyTimer{}

	mockNetwork := NewMockNetwork(0)
	quit := mockNetwork.routeMessages()
	zeroBuf := make([]byte, 0)

	nodeIds, nodes := createNodesAndIds(numOfNodes)

	for i := 0; i < numOfNodes-1; i++ {
		ds := initDSC(mockNetwork, nodeIds, nodes[i], cfg, timer)
		dsInstances = append(dsInstances, ds)
	}

	for i := 0; i < numOfNodes-1; i++ {
		x := messageData(zeroBuf)
		//start instance blocks until protocol is finished
		go dsInstances[i].StartInstance(x)
	}

	node := nodes[numOfNodes-1]
	nodeMock := mockNetwork.createNewNodeNetMock(node.publicKey.String())
	dsc, _ := NewDSC(cfg, timer, nodeIds, nodeMock, node.publicKey, node.privateKey)
	mal := MaliciousDelayedSecondMessageSender{*dsc}

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

func initDolevStrongConsensus(numOfNodes int) *DolevStrongMultiInstanceConsensus {
	cfg := config.DefaultConfig()
	cfg.NodesPerLayer = int32(numOfNodes)
	cfg.NumOfAdversaries = int32(numOfNodes / 2)
	timer := MyTimer{}

	mockNetwork := NewMockNetwork(0)
	//quit := mockNetwork.routeMessages()
	node := NewNode()

	nodeMock := mockNetwork.createNewNodeNetMock(node.publicKey.String())
	dolevStrong, _ := NewDSC(cfg, timer, nil, nodeMock, node.publicKey, node.privateKey)
	return dolevStrong
}

func TestInvalidPublicKeyOfInitiatorMessage(t *testing.T) {
	t.Skip()
	numOfNodes := 10
	dolevStrong := initDolevStrongConsensus(numOfNodes)

	arr := messageData([]byte("anton"))
	msg := createMessage(arr, dolevStrong)

	// flip a byte in the pub key to fail
	msg.Msg.AuthPubKey[0] = ^msg.Msg.AuthPubKey[0]

	bt, _ := proto.Marshal(&msg)
	ms := messageData(bt)
	err := dolevStrong.handleMessage(ms)

	assert.Error(t, err)
}

func TestInvalidSignature(t *testing.T) {
	t.Skip()
	numOfNodes := 10
	dolevStrong := initDolevStrongConsensus(numOfNodes)
	msg := createMessage(messageData([]byte("anton")), dolevStrong)
	dolevStrong.signMessageAndAppend(&msg)
	msg.Msg.Data[0] = ^msg.Msg.Data[0]
	instance := DolevStrongInstance{
		ds: dolevStrong,
	}
	_, err := instance.findAndValidateSignatures(&msg)
	assert.Error(t, err)
}

func validateDSC(t *testing.T, dsc HareAlgorithm, expected []byte, node string, senderInstance string, shouldExist bool) {
	outputs := dsc.GetOtherInstancesOutput()
	for key, out := range outputs {
		if key == senderInstance && node != senderInstance {
			if shouldExist {
				assert.True(t, bytes.Equal(out, expected), "buffer not found in any instance %v", node)
			} else {
				assert.False(t, bytes.Equal(out, expected), "buffer found in instance %v", node)
			}

		}
	}

}

func initDSC(mockNetwork *MockNetwork, activeNodes []string, node Node, cfg config.Config, timer MyTimer) *DolevStrongMultiInstanceConsensus {
	nodeMock := mockNetwork.createNewNodeNetMock(node.publicKey.String())
	ds, _ := NewDSC(cfg, timer, activeNodes, nodeMock, node.publicKey, node.privateKey)
	return ds
}

func createMessage(msg OpaqueMessage, impl *DolevStrongMultiInstanceConsensus) pb.ConsensusMessage {
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

}
