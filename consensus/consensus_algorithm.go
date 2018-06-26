package consensus

import (
	"github.com/spacemeshos/go-spacemesh/consensus/pb"
	"github.com/spacemeshos/go-spacemesh/crypto"
	"time"
)

type OpaqueMessage interface {
	Data() []byte
}

type ConsensusAlgorithm interface {
	StartInstance(msg OpaqueMessage) []byte

	Abort()

	GetOtherInstancesOutput() map[string][]byte

	//======= for ut =======
	// Starts listening for new attempts to reach consensus from other nodes
	StartListening() error

	// Sends this message in order to reach consensus
	SendMessage(msg OpaqueMessage) error
}

type RemoteNodeData interface {
	ID() string    // base58 encoded node key/id
	IP() string    // node tcp listener e.g. 127.0.0.1:3038
	Bytes() []byte // node raw id bytes

	//DhtID() dht.ID // dht id must be uniformly distributed for XOR distance to work, hence we hash the node key/id to create it

	Pretty() string
}

type CAInstance interface {
	ReceiveMessage(msg *pb.ConsensusMessage)

	GetOutput() []byte
}

type Signing interface {
	SignMessage(data []byte) []byte
}

type NetworkConnection interface {
	SendMessage(message []byte, addr string) (int, error)
	RegisterProtocol(protocolName string) chan OpaqueMessage
}

type Timer interface {
	GetTime() time.Time
}

//todo: make valiues to be interface for everybody to use
type MessageQueue struct {
	outputQueue chan interface{}
	first       *interface{}
}

func newMessageQueue(size int) MessageQueue {
	return MessageQueue{
		outputQueue: make(chan interface{}, size),
		first:       nil,
	}
}

func (mq *MessageQueue) Peek() *interface{} {
	if mq.first == nil {
		val := <-mq.outputQueue
		mq.first = &val
	}
	return mq.first
}

func (mq *MessageQueue) Pop() *interface{} {
	msg := mq.Peek()
	mq.first = nil
	return msg
}

type KeySet map[crypto.PublicKey]struct{}
