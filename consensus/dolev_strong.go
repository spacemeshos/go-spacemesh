package consensus

import (
	"github.com/golang/protobuf/proto"
	"github.com/spacemeshos/go-spacemesh/consensus/pb"
	"github.com/spacemeshos/go-spacemesh/log"
	//"reflect"
	"bytes"
	"errors"
	"fmt"
	dsCfg "github.com/spacemeshos/go-spacemesh/consensus/config"
	"github.com/spacemeshos/go-spacemesh/crypto"
	"time"
)

// DolevStrongMultiInstanceConsensus is an implementation of a byzanteen agreement protocol
type DolevStrongMultiInstanceConsensus struct {
	activeInstances map[string]CAInstance
	ingressChannel  chan OpaqueMessage
	net             NetworkConnection
	dsConfig        dsCfg.Config
	privateKey      crypto.PrivateKey
	publicKey       crypto.PublicKey
	timer           Timer
	startTime       time.Time
	abortInstance   chan struct{}
	abortListening  chan struct{}
}

// DolevStrongInstance is a struct holding state for a single instance of a dolev strong agreement protocol as a receiver
type DolevStrongInstance struct {
	value                   []byte
	ds                      *DolevStrongMultiInstanceConsensus
	abortProcessingInstance bool
}

// StartInstance starts an agreement protocol instance
func (impl *DolevStrongMultiInstanceConsensus) StartInstance(msg OpaqueMessage) []byte {
	log.Info("Starting %v", impl.publicKey.String())
	impl.startTime = time.Now()
	go impl.SendMessage(msg)
	go impl.StartListening()
	impl.waitForConsensus()
	return msg.Data()
}

// NewDSC creates a new instance of dolev strong agreement protocol
func NewDSC(conf dsCfg.Config,
	timer Timer,
	nodes []string,
	network NetworkConnection,
	pubKey crypto.PublicKey,
	privKey crypto.PrivateKey) (*DolevStrongMultiInstanceConsensus, error) {

	dsc := &DolevStrongMultiInstanceConsensus{
		activeInstances: make(map[string]CAInstance),
		ingressChannel:  network.RegisterProtocol("DolevStrong"),
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
		dsc.activeInstances[node] = NewDolevStrongInstance(dsc)
	}

	return dsc, nil
}

func (impl *DolevStrongMultiInstanceConsensus) handleMessage(message OpaqueMessage) error {
	msg := &pb.ConsensusMessage{}
	proto.Unmarshal(message.Data(), msg)
	pk, err := crypto.NewPublicKey(msg.Msg.AuthPubKey)
	if err != nil {
		log.Error("cannot parse public key from message: %v, err :%v", msg.Msg.AuthPubKey, err)
		return err
	}
	nodeID := pk.String()
	var ci CAInstance
	if val, ok := impl.activeInstances[nodeID]; !ok {
		ci = NewDolevStrongInstance(impl)
		impl.activeInstances[nodeID] = ci
	} else {
		ci = val
	}
	return ci.ReceiveMessage(msg)
}

// StartListening starts listening for agreement protocol messages from other instances running in the network
func (impl *DolevStrongMultiInstanceConsensus) StartListening() {
	log.Info("started listening")
	for {
		select {
		case message := <-impl.ingressChannel:
			err := impl.handleMessage(message)
			if err != nil {
				log.Error("Protocol encountered error: %v", err)
			}
		case <-impl.abortListening:
			log.Info("listening ABORTED!")
			return
		}

	}
}

func (impl *DolevStrongMultiInstanceConsensus) waitForConsensus() {
	numOfRounds := impl.dsConfig.NumOfAdverseries + 1
	ticker := time.NewTicker(time.Duration(time.Duration(numOfRounds) * impl.dsConfig.RoundTime))

	for {
		select {
		case <-ticker.C:
			log.Info("finished all DS rounds")
			impl.Abort()
		case <-impl.abortInstance:
			ticker.Stop()
			return
		}
	}
}

// SendMessage sends this message in order to reach consensus
func (impl *DolevStrongMultiInstanceConsensus) SendMessage(msg OpaqueMessage) error {
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
		log.Error("cannot sign message %v err %v", protocolMsg, err)
		return err
	}
	except := make(map[string]struct{})
	except[impl.publicKey.String()] = struct{}{}
	impl.sendToRemainingNodes(&protocolMsg, except)
	return nil
}

func (dsci *DolevStrongInstance) authenticateSignature(validator crypto.PublicKey, signature []byte, data []byte) bool {

	out, err := validator.Verify(data, signature)
	if err != nil {
		log.Error("error validating signature", signature, " errno:", err)
		return false
	}
	return out
}

func (impl *DolevStrongMultiInstanceConsensus) getRound() int32 {
	return int32(impl.timer.Since(impl.startTime).Nanoseconds() / impl.dsConfig.RoundTime.Nanoseconds())
}

//GetOutput Returns the message agreed upon in this instance of dolev string, returns nil if the message agreement failed
func (dsci *DolevStrongInstance) GetOutput() []byte {
	if dsci.abortProcessingInstance {
		return nil
	}
	return dsci.value
}

// ReceiveMessage is the main receiver handler for a message received from a dolev strong instance running in the network
func (dsci *DolevStrongInstance) ReceiveMessage(message *pb.ConsensusMessage) error {
	//calculates round according to local clock
	round := dsci.ds.getRound()
	log.Info("%v received message in round %v num of signatures: %v", dsci.ds.publicKey.String(), round, len(message.Validators))

	if dsci.abortProcessingInstance {
		return fmt.Errorf("message received on aborted isntance, round: %v", round)
	}

	if round > dsci.ds.dsConfig.NumOfAdverseries+1 {
		return fmt.Errorf("round out of order: %v", round)
	}

	publicKeys, err := dsci.findAndValidateSignatures(message)
	if err != nil {
		return err
	}
	initiatorPubKey, err := crypto.NewPublicKey(message.Msg.AuthPubKey)
	if _, ok := publicKeys[initiatorPubKey.String()]; !ok || err != nil {
		return fmt.Errorf("initiator did not sign the message - aborting %v", initiatorPubKey.String())

	}
	//check if there are enough signature to match round number
	if len(publicKeys) < int(round) {
		return fmt.Errorf("invalid number of signatures - not matching round number %v num of signatures %v", round, len(publicKeys))
	}
	//todo: what if the signatures are of nodes that are not in the layer?
	if dsci.value != nil {
		if res := bytes.Compare(dsci.value, message.Msg.Data); res != 0 {
			dsci.abortProcessingInstance = true //with abort
			dsci.sendMessage(message, publicKeys)
			return fmt.Errorf("received two different messages from same sender, aborting this instance")
		}
		// no need to do anything
		return nil
	}
	//set the message as the correct one
	dsci.value = message.Msg.Data
	log.Info("Message received on node : %v", dsci.ds.publicKey.String())

	return dsci.sendMessage(message, publicKeys)
}

func (dsci *DolevStrongInstance) sendMessage(message *pb.ConsensusMessage, publicKeys map[string]struct{}) error {
	// add our signature on the message
	err := dsci.ds.signMessageAndAppend(message)
	if err != nil {
		return fmt.Errorf("cannot sign message %v error %v", message, err)
	}
	//add this node to the nodes already signed
	publicKeys[dsci.ds.publicKey.String()] = struct{}{}
	dsci.ds.sendToRemainingNodes(message, publicKeys) // to be sent in round +1
	return nil
}

func (dsci *DolevStrongInstance) findAndValidateSignatures(message *pb.ConsensusMessage) (map[string]struct{}, error) {
	publicKeys := make(map[string]struct{})
	data, err := proto.Marshal(message.Msg)
	if err != nil {
		return nil, fmt.Errorf("error marshaling validator struct")
	}
	// validate signatures
	for _, val := range message.Validators {
		key, ok := crypto.NewPublicKey(val.AuthPubKey)
		if ok != nil {
			return nil, errors.New("cannot read validator public key")
		}

		validSig := dsci.authenticateSignature(key, val.AuthorSign, data)
		if !validSig {
			return nil, fmt.Errorf("invalid signature of %v", val.AuthorSign)
		}
		publicKeys[key.String()] = struct{}{}
	}
	return publicKeys, nil
}

// Abort aborts the run of the current agreement protocol
func (impl *DolevStrongMultiInstanceConsensus) Abort() {
	impl.abortInstance <- struct{}{}
	impl.abortListening <- struct{}{}
}

func (impl *DolevStrongMultiInstanceConsensus) sendToRemainingNodes(message *pb.ConsensusMessage, foundValidators map[string]struct{}) error {
	// Sends this message in order to reach consensus
	data, ok := proto.Marshal(message)
	if ok != nil {
		log.Error("could not marshal output message", message)
	}
	for key := range impl.activeInstances {
		//don't send to signed validators
		if _, ok := foundValidators[key]; ok {
			continue
		}
		_, err := impl.net.SendMessage(data, key)
		if err != nil {
			//todo: should we break the loop now?
			//return err
		}
	}
	return nil
}

// GetOtherInstancesOutput returns all the outputs agreed in this layer from other instances that ran in the same time as this initiator
func (impl *DolevStrongMultiInstanceConsensus) GetOtherInstancesOutput() map[string][]byte {
	output := make(map[string][]byte)
	for key, ds := range impl.activeInstances {
		output[key] = ds.GetOutput()
	}
	return output
}

func (impl *DolevStrongMultiInstanceConsensus) signMessageAndAppend(message *pb.ConsensusMessage) error {
	//marshall the message so that we could sign the entire byte stream as a whole
	bytes, err := proto.Marshal(message.Msg)
	if err != nil {
		log.Error("cannot marshal message %v err %v", message.Msg, err)
		return err
	}
	mySignature, err := impl.privateKey.Sign(bytes)
	if err != nil {
		return err
	}
	validator := pb.Validator{
		AuthPubKey: impl.publicKey.Bytes(),
		AuthorSign: mySignature,
	}

	message.Validators = append(message.Validators, &validator)
	return nil
}

//NewDolevStrongInstance return a new instance of a dolev strong receiver
func NewDolevStrongInstance(ca *DolevStrongMultiInstanceConsensus) *DolevStrongInstance {
	ds := &DolevStrongInstance{
		value: nil,
		ds:    ca,
	}

	return ds
}
