package p2p2

import (
	"time"
)

// Session info with a remote node - wraps connection
type NetworkSession interface {
	Iv() []byte // session iv
	Key() []byte // session shared sym key
	Created() time.Time // time when session was established

	// TODO: add expiration support

	IsAuthenticated() bool
	SetAuthenticated(val bool)

	// todo: this might include an AES cypher instance for fast enc/dec of data to/from a remote node
	// when we have an active session
}

type NetworkSessionImpl struct {
	iv []byte
	key []byte
	created time.Time
	authenticated bool
}

func (n* NetworkSessionImpl) Iv() []byte {
	return n.iv
}

func (n* NetworkSessionImpl) Key() []byte {
	return n.Key()
}

func (n* NetworkSessionImpl) IsAuthenticated() bool {
	return n.authenticated
}

func (n* NetworkSessionImpl) SetAuthenticated(val bool) {
	n.authenticated = val
}

func (n* NetworkSessionImpl) Created() time.Time {
	return n.created
}

func NewNetworkSession(iv []byte, key []byte) NetworkSession {
	return &NetworkSessionImpl{
		iv: iv,
		key: key,
		created: time.Now(),
		authenticated: false,
	}
}


// Remote node data
// Node connections are maintained by swarm
type RemoteNode interface {
	Id() []byte     // node id is public key bytes
	String() string // node public key string
	Pretty() string
	TcpAddress() string // tcp address advertised by node e.g. 127.0.0.1:3058 - todo consider multiaddress here

	PublicKey() PublicKey

	// session support
	GetSession(callback func(session NetworkSession))
	SetSession(s NetworkSession)
	HasSession() bool

	Kill()
}

type RemoteNodeImpl struct {

	publicKey  PublicKey
	tcpAddress string

	session NetworkSession

	attachSessoinChannel chan NetworkSession
	expireSessionChannel chan bool

	getSessionChannel chan func(s NetworkSession)

	kill chan bool
}

// Create a new remote node using provided id and tcp address
func NewRemoteNode(id string, tcpAddress string) (RemoteNode, error) {

	key, err := NewPublicKeyFromString(id)
	if err != nil {
		return nil, err
	}

	node := &RemoteNodeImpl{
		publicKey:  key,
		tcpAddress: tcpAddress,
		attachSessoinChannel: make(chan NetworkSession, 1),
		expireSessionChannel: make(chan bool),
		getSessionChannel:    make(chan func(s NetworkSession), 1),
		kill : make(chan bool),

	}

	go node.processEvents()

	return node, nil
}

func (n *RemoteNodeImpl) Kill() {
	// stop processing events
	n.kill <- true
}

func (n *RemoteNodeImpl) processEvents() {

Loop:
	for {
		select {

		case <- n.kill:
			break Loop

		case s := <-n.attachSessoinChannel:
			n.session = s

		case <-n.expireSessionChannel:
			n.session = nil

		case getSession := <-n.getSessionChannel:
			getSession(n.session)

		}
	}
}

// go safe - attach a session to the connection
func (n *RemoteNodeImpl) SetSession(s NetworkSession) {
	n.attachSessoinChannel <- s
}

func (n *RemoteNodeImpl) HasSession() bool {
	return n.session != nil
}

// go safe - use a channel of func to implement concurent safe callbacks
func (n *RemoteNodeImpl) GetSession(callback func(n NetworkSession)) {
	n.getSessionChannel <- callback
}

// go safe
func (n *RemoteNodeImpl) ExpireSession() {
	n.expireSessionChannel <- true
}

func (n *RemoteNodeImpl) Id() []byte {
	return n.publicKey.Bytes()
}

func (n *RemoteNodeImpl) String() string {
	return n.publicKey.String()
}

func (n *RemoteNodeImpl) Pretty() string {
	return n.publicKey.Pretty()
}

func (n *RemoteNodeImpl) PublicKey() PublicKey {
	return n.publicKey
}

func (n *RemoteNodeImpl) TcpAddress() string {
	return n.tcpAddress
}
