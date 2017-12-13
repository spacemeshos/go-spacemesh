package p2p2

import "github.com/UnrulyOS/go-unruly/log"

// Remote node data
// Node connections are maintained by swarm
type RemoteNode interface {
	Id() []byte     // node id is public key bytes
	String() string // node public key string
	Pretty() string
	TcpAddress() string // tcp address advertised by node e.g. 127.0.0.1:3058 - todo consider multiaddress here

	PublicKey() PublicKey

	// todo: we might need to support multiple sessions per node to handle the case where 2 nodes try to establish a session
	// with each other over a short time span - we need to properly handle this case

	GetSession(callback func(session NetworkSession))
	SetSession(s NetworkSession)
	HasSession() bool

	Kill()
}

type remoteNodeImpl struct {
	publicKey  PublicKey
	tcpAddress string

	session NetworkSession

	attachSessionChannel chan NetworkSession
	expireSessionChannel chan bool

	getSessionChannel chan func(s NetworkSession)

	kill chan bool
}

// Create a new remote node using provided id and tcp address
func NewRemoteNode(id string, tcpAddress string) (RemoteNode, error) {

	key, err := NewPublicKeyFromString(id)
	if err != nil {
		log.Error("invalid node id format: %v", err)
		return nil, err
	}

	node := &remoteNodeImpl{
		publicKey:            key,
		tcpAddress:           tcpAddress,
		attachSessionChannel: make(chan NetworkSession, 1),
		expireSessionChannel: make(chan bool),
		getSessionChannel:    make(chan func(s NetworkSession), 1),
		kill:                 make(chan bool),
	}

	go node.processEvents()

	return node, nil
}

func (n *remoteNodeImpl) Kill() {
	// stop processing events
	n.kill <- true
}

func (n *remoteNodeImpl) processEvents() {

Loop:
	for {
		select {

		case <-n.kill:
			break Loop

		case s := <-n.attachSessionChannel:
			n.session = s

		case <-n.expireSessionChannel:
			n.session = nil

		case getSession := <-n.getSessionChannel:
			getSession(n.session)

		}
	}
}

// go safe - attach a session to the connection
func (n *remoteNodeImpl) SetSession(s NetworkSession) {
	n.attachSessionChannel <- s
}

func (n *remoteNodeImpl) HasSession() bool {
	return n.session != nil
}

// go safe - use a channel of func to implement concurent safe callbacks
func (n *remoteNodeImpl) GetSession(callback func(n NetworkSession)) {
	n.getSessionChannel <- callback
}

// go safe
func (n *remoteNodeImpl) ExpireSession() {
	n.expireSessionChannel <- true
}

func (n *remoteNodeImpl) Id() []byte {
	return n.publicKey.Bytes()
}

func (n *remoteNodeImpl) String() string {
	return n.publicKey.String()
}

func (n *remoteNodeImpl) Pretty() string {
	return n.publicKey.Pretty()
}

func (n *remoteNodeImpl) PublicKey() PublicKey {
	return n.publicKey
}

func (n *remoteNodeImpl) TcpAddress() string {
	return n.tcpAddress
}
