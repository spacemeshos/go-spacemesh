package consensus

import (
	"github.com/golang/protobuf/proto"
	"github.com/spacemeshos/go-spacemesh/consensus/pb"
	"github.com/spacemeshos/go-spacemesh/log"
	//"reflect"
	"bytes"
	dsCfg "github.com/spacemeshos/go-spacemesh/consensus/config"
	"github.com/spacemeshos/go-spacemesh/crypto"
	"time"
)

// DolevStrongConsensus is an implementation of a byzanteen agreement protocol
type DolevStrongConsensus struct {
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
	value              []byte
	initiator          RemoteNodeData
	ds                 *DolevStrongConsensus
	finishedProcessing bool
}

// StartInstance starts an agreement protocol instance
func (impl *DolevStrongConsensus) StartInstance(msg OpaqueMessage) []byte {
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
	privKey crypto.PrivateKey) (Algorithm, error) {

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

func (impl *DolevStrongConsensus) handleMessage(message OpaqueMessage) {
	msg := &pb.ConsensusMessage{}
	proto.Unmarshal(message.Data(), msg)
	pk, ok := crypto.NewPublicKey(msg.Msg.AuthPubKey)
	if ok != nil {
		log.Error("cannot parse public key from message: ", msg.Msg.AuthPubKey)
	}
	nodeID := pk.String()
	var ci CAInstance
	if val, ok := impl.activeInstances[nodeID]; !ok {
		ci = NewCAInstance(impl)
		impl.activeInstances[nodeID] = ci
	} else {
		ci = val
	}
	ci.ReceiveMessage(msg)
}

// StartListening starts listening for agreement protocol messages from other instances running in the network
func (impl *DolevStrongConsensus) StartListening() error {
	impl.ingressChannel = impl.net.RegisterProtocol("DolevStrong")
	log.Info("started listening")
	for {
		select {
		case message := <-impl.ingressChannel:
			{
				impl.handleMessage(message)
			}
		case <-impl.abortListening:
			log.Info("listening ABORTED!")
			return nil
		}

	}
}

func (impl *DolevStrongConsensus) waitForConsensus() {
	numOfRounds := impl.dsConfig.NumOfAdverseries + 1
	ticker := time.NewTicker(time.Duration(time.Duration(numOfRounds) * impl.dsConfig.PhaseTime))

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
func (impl *DolevStrongConsensus) SendMessage(msg OpaqueMessage) error {
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
	except := make(map[string]struct{})
	except[impl.publicKey.String()] = struct{}{}
	impl.sendToRemainingNodes(&protocolMsg, except)
	return nil
}

func (dsci *DolevStrongInstance) authenticateSignature(validator crypto.PublicKey, signature []byte, data *pb.MessageData) bool {
	dt, err := proto.Marshal(data)
	if err != nil {
		panic("error marshaling validator struct")

	}
	out, err := validator.Verify(dt, signature)
	if err != nil {
		log.Error("error validating signature", signature, " errno:", err)
		return false
	}
	return out
}

func (impl *DolevStrongConsensus) getRound() int32 {
	return int32(time.Since(impl.startTime).Nanoseconds() / impl.dsConfig.PhaseTime.Nanoseconds())
}

//GetOutput Returns the message agreed upon in this instance of dolev string, returns nil if the message agreement failed
func (dsci *DolevStrongInstance) GetOutput() []byte {
	if dsci.finishedProcessing {
		return nil
	}
	return dsci.value
}

func (impl *DolevStrongConsensus) signMessage(message []byte) []byte {
	out, ok := impl.privateKey.Sign(message)
	if ok != nil {
		log.Error("Failed to sign message", message, " error ", ok)
	}
	return out
}

// ReceiveMessage is the main receiver handler for a message received from a dolev strong instance running in the network
func (dsci *DolevStrongInstance) ReceiveMessage(message *pb.ConsensusMessage) {
	validSig := true
	//calculates round according to local clock
	round := dsci.ds.getRound()
	log.Info("received message in round %v num of signatures: %v", round, len(message.Validators))

	if dsci.finishedProcessing {
		log.Info("message received on aborted isntance, round:", round)
	}

	//todo: should the protocol send round number for validation?
	if round > dsci.ds.dsConfig.NumOfAdverseries+1 {
		log.Error("round out of order:", round)
		return
	}

	publicKeys := make(map[string]struct{})
	log.Debug("received message from round %v num of signatures %v", round, len(message.Validators))

	initiatorPubKey := message.Msg.AuthPubKey
	foundInitiatorSig := false
	// validate signatures
	for _, val := range message.Validators {
		key, ok := crypto.NewPublicKey(val.AuthPubKey)
		if ok != nil {
			log.Error("cannot read validator public key")
			return
		}

		//check if initiator signed message
		if bytes.Compare(val.AuthPubKey, initiatorPubKey) == 0 {
			foundInitiatorSig = true
		}

		//todo: check if has its own signature
		validSig = dsci.authenticateSignature(key, val.AuthorSign, message.Msg)
		if !validSig {
			log.Error("invalid signature of ", val.AuthorSign)
			//break //TODO: what should be done here?
			return
		}
		publicKeys[key.String()] = struct{}{}
	}

	if !foundInitiatorSig {
		log.Error("initiator did not sign the message - aborting")
		return
	}

	//check if there are enough signature to match round number
	if len(publicKeys) < int(round) {
		log.Error("invalid number of signatures - not matching round number %v num of signatures %v", round, len(publicKeys))
		return
	}

	if dsci.value != nil {
		if res := bytes.Compare(dsci.value, message.Msg.Data); res != 0 {
			log.Error("received two different messages from same sender, aborting this instance")
			dsci.finishedProcessing = true //with abort
			if res > 0 {                   //or just != 0
				dsci.value = message.Msg.Data
				//todo: what happens when there is a message flood?
			}
		} else { //same value received, no need to resend value
			return
		}
	} else {
		//set the message as the correct one
		dsci.value = message.Msg.Data
	}
	// add our signature on the message
	err := dsci.ds.signMessageAndAppend(message)
	if err != nil {
		log.Error("cannot sign message %v", message, " error ", err)
	}
	//add this node to the nodes already signed
	publicKeys[dsci.ds.publicKey.String()] = struct{}{}
	dsci.ds.sendToRemainingNodes(message, publicKeys) // to be sent in round +1
}

// Abort aborts the run of the current agreement protocol
func (impl *DolevStrongConsensus) Abort() {
	impl.abortInstance <- struct{}{}
	impl.abortListening <- struct{}{}
}

func (impl *DolevStrongConsensus) sendToRemainingNodes(message *pb.ConsensusMessage, foundValidators map[string]struct{}) error {
	// Sends this message in order to reach consensus
	data, ok := proto.Marshal(message)
	if ok != nil {
		log.Error("could not marshal output message", message)
		panic("Hi there")
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
func (impl *DolevStrongConsensus) GetOtherInstancesOutput() map[string][]byte {
	output := make(map[string][]byte)
	for key, ds := range impl.activeInstances {
		output[key] = ds.GetOutput()
	}
	return output
}

func (impl *DolevStrongConsensus) signMessageAndAppend(message *pb.ConsensusMessage) error {
	bytes, err := proto.Marshal(message.Msg)
	if err != nil {
		log.Error("cannot marshal message %v", message.Msg)
		return err
	}
	mySignature := impl.signMessage(bytes)
	validator := pb.Validator{
		AuthPubKey: impl.publicKey.Bytes(),
		AuthorSign: mySignature,
	}

	message.Validators = append(message.Validators, &validator)
	return nil
}

//NewCAInstance return a new instance of a dolev strong receiver
func NewCAInstance(ca *DolevStrongConsensus) CAInstance {
	ds := &DolevStrongInstance{
		value: nil,
		ds:    ca,
	}

	return ds
}
