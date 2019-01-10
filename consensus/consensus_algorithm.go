package consensus

import (
	"github.com/spacemeshos/go-spacemesh/consensus/pb"
	"time"
)

// OpaqueMessage is a basic blob of bytes type interface
type OpaqueMessage interface {
	// Data returns the bytes contained by this message
	Data() []byte
}

// Algorithm is the main API to run a consensus algorithm that coordinates messages between nodes
type HareAlgorithm interface {

	//StartInstance starts an instance of the byzantine agreement, trying to reach agreement for the given message while
	//receiving other agreement attempts from other nodes. this method blocks until protocol has finished
	StartInstance(msg OpaqueMessage) []byte

	//Aborts the operation of the consensus protocol
	Abort()

	// after algorithm has finished, returns consensus messages from other initiator parties
	GetOtherInstancesOutput() map[string][]byte

	//======= for ut =======
	// Starts listening for new attempts to reach consensus from other nodes
	StartListening()

	// Sends this message in order to reach consensus
	SendMessage(msg OpaqueMessage) error
}

// RemoteNodeData describes Node identity
type RemoteNodeData interface {
	ID() string    // base58 encoded node key/id
	IP() string    // node tcp listener e.g. 127.0.0.1:3038
	Bytes() []byte // node raw id bytes

	//DhtID() dht.ID // dht id must be uniformly distributed for XOR distance to work, hence we hash the node key/id to create it

	Pretty() string
}

// CAInstance is an instance of the consensus algorithm used to receive and participate in an consensus protocol attempt of one instance
type CAInstance interface {
	ReceiveMessage(msg *pb.ConsensusMessage) error

	GetOutput() []byte
}

// Signing  is a simple interface for signing messages with private key
type Signing interface {
	SignMessage(data []byte) []byte
}

// NetworkConnection is a network tap interface
type NetworkConnection interface {
	SendMessage(message []byte, addr string) (int, error)
	RegisterProtocol(protocolName string) chan OpaqueMessage
}

// Timer is an interface to receive current time
type Timer interface {
	GetTime() time.Time

	Since(t time.Time) time.Duration
}
