package p2p

import (
	"bytes"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"crypto/sha512"
	"encoding/hex"
	"errors"
	"io"
	"time"

	"github.com/btcsuite/btcd/btcec"
	"github.com/gogo/protobuf/proto"
	"github.com/spacemeshos/go-spacemesh/crypto"
	"github.com/spacemeshos/go-spacemesh/p2p/nodeconfig"
	"github.com/spacemeshos/go-spacemesh/p2p/pb"
	"strings"
)

// HandshakeReq specifies the handshake protocol request message identifier. pattern is [protocol][version][method-name].
const HandshakeReq = "/handshake/1.0/handshake-req/"

// HandshakeResp specifies the handshake protocol response message identifier.
const HandshakeResp = "/handshake/1.0/handshake-resp/"

// HandshakeData defines the handshake protocol capabilities.
type HandshakeData interface {
	LocalNode() LocalNode
	Peer() Peer
	Session() NetworkSession
	GetError() error
	SetError(err error)
}

// NewHandshakeData creates new HandshakeData with the provided data.
func NewHandshakeData(localNode LocalNode, peer Peer, session NetworkSession, err error) HandshakeData {
	return &handshakeDataImp{
		localNode: localNode,
		peer:      peer,
		session:   session,
		err:       err,
	}
}

type handshakeDataImp struct {
	localNode LocalNode
	peer      Peer
	session   NetworkSession
	err       error
}

// LocalNode returns the protocol's local node.
func (n *handshakeDataImp) LocalNode() LocalNode {
	return n.localNode
}

// Peer returns the protocol's remote node.
func (n *handshakeDataImp) Peer() Peer {
	return n.peer
}

// Session returns the network session between the local and the remote nodes.
func (n *handshakeDataImp) Session() NetworkSession {
	return n.session
}

// GetError returns the last protocol error or nil otherwise.
func (n *handshakeDataImp) GetError() error {
	return n.err
}

// SetError sets protocol error. The last error set is returned by GetError().
func (n *handshakeDataImp) SetError(err error) {
	n.err = err
}

// HandshakeProtocol specifies the handshake protocol.
// Node1 -> Node 2: Req(HandshakeData)
// Node2 -> Node 1: Resp(HandshakeData)
// After response is processed by node1 both sides have an auth session with a secret ephemeral aes sym key.
type HandshakeProtocol interface {
	CreateSession(peer Peer)
	RegisterNewSessionCallback(callback chan HandshakeData) // register a channel to receive session state changes
}

type handshakeProtocolImpl struct {

	// state
	swarm               Swarm
	newSessionCallbacks []chan HandshakeData     // a list of callback channels for new sessions
	pendingSessions     map[string]HandshakeData // sessions pending authentication

	// ops
	incomingHandshakeRequests MessagesChan
	incomingHandsakeResponses MessagesChan
	registerSessionCallback   chan chan HandshakeData
	addPendingSession         chan HandshakeData
	deletePendingSessionByID  chan string
	sessionStateChanged       chan HandshakeData
}

// NewHandshakeProtocol Creates a new Handshake protocol for the provided swarm.
func NewHandshakeProtocol(s Swarm) HandshakeProtocol {

	h := &handshakeProtocolImpl{
		swarm:           s,
		pendingSessions: make(map[string]HandshakeData),

		incomingHandshakeRequests: make(MessagesChan, 20),
		incomingHandsakeResponses: make(chan IncomingMessage, 20),
		registerSessionCallback:   make(chan chan HandshakeData, 2),
		newSessionCallbacks:       make([]chan HandshakeData, 0), // start with empty slice
		deletePendingSessionByID:  make(chan string, 5),
		sessionStateChanged:       make(chan HandshakeData, 3),
		addPendingSession:         make(chan HandshakeData, 3),
	}

	go h.processEvents()

	// protocol demuxer registration

	s.GetDemuxer().RegisterProtocolHandler(
		ProtocolRegistration{Protocol: HandshakeReq, Handler: h.incomingHandshakeRequests})

	s.GetDemuxer().RegisterProtocolHandler(
		ProtocolRegistration{Protocol: HandshakeResp, Handler: h.incomingHandsakeResponses})

	return h
}

// RegisterNewSessionCallback registers a callback to baclled when a new session is established by the protocol.
func (h *handshakeProtocolImpl) RegisterNewSessionCallback(callback chan HandshakeData) {
	h.swarm.GetLocalNode().Info("New session callback registered.")
	h.registerSessionCallback <- callback
}

// CreateSession is called to initiate the handshake protocol between the local node and a remote peer.
func (h *handshakeProtocolImpl) CreateSession(peer Peer) {

	data, session, err := generateHandshakeRequestData(h.swarm.GetLocalNode(), peer)

	handshakeData := NewHandshakeData(h.swarm.GetLocalNode(), peer, session, err)

	if err != nil {
		h.sessionStateChanged <- handshakeData
		return
	}

	payload, err := proto.Marshal(data)
	if err != nil {
		h.sessionStateChanged <- handshakeData
		return
	}

	// so we can match handshake responses with the session
	h.addPendingSession <- handshakeData

	h.swarm.GetLocalNode().Info("Creating session handshake request session id: %s", session.String())

	h.swarm.sendHandshakeMessage(SendMessageReq{
		ReqID:    session.ID(),
		PeerID:   peer.String(),
		Payload:  payload,
		Callback: nil,
	})

	h.sessionStateChanged <- handshakeData
}

// internal event processing handler
func (h *handshakeProtocolImpl) processEvents() {
	for {
		select {
		case m := <-h.incomingHandshakeRequests:
			h.onHandleIncomingHandshakeRequest(m)

		case m := <-h.incomingHandsakeResponses:
			h.onHandleIncomingHandshakeResponse(m)

		case c := <-h.registerSessionCallback:
			h.newSessionCallbacks = append(h.newSessionCallbacks, c)

		case d := <-h.addPendingSession:
			sessionKey := d.Session().String()
			h.swarm.GetLocalNode().Info("Storing pending session w key: %s", sessionKey)
			h.pendingSessions[sessionKey] = d

		case k := <-h.deletePendingSessionByID:
			delete(h.pendingSessions, k)

		case s := <-h.sessionStateChanged:
			for _, c := range h.newSessionCallbacks {
				go func(c chan HandshakeData) { c <- s }(c)
			}
		}
	}
}

// Handles a remote handshake request.
func (h *handshakeProtocolImpl) onHandleIncomingHandshakeRequest(msg IncomingMessage) {

	data := &pb.HandshakeData{}
	err := proto.Unmarshal(msg.Payload(), data)
	if err != nil {
		h.swarm.GetLocalNode().Warning("invalid incoming handshake request bin data", err)
		return
	}

	if msg.Sender() == nil {
		// we don't know about this remote node - create a new one for it using the info it sent
		// and add it to the swarm
	}

	respData, session, err := processHandshakeRequest(h.swarm.GetLocalNode(), msg.Sender(), data)

	// we have a new session started by a remote node
	handshakeData := NewHandshakeData(h.swarm.GetLocalNode(), msg.Sender(), session, err)

	if err != nil {
		// failed to process request
		handshakeData.SetError(err)
		h.sessionStateChanged <- handshakeData
		return
	}

	payload, err := proto.Marshal(respData)
	if err != nil {
		handshakeData.SetError(err)
		h.sessionStateChanged <- handshakeData
		return
	}

	// todo: support callback errors

	// send response back to sender
	h.swarm.sendHandshakeMessage(SendMessageReq{
		ReqID:    session.ID(),
		PeerID:   msg.Sender().String(),
		Payload:  payload,
		Callback: nil,
	})

	// we have an active session initiated by a remote node
	h.sessionStateChanged <- handshakeData

	h.swarm.GetLocalNode().Info("Remotely initiated session established. Session id: %s :-)", session.String())

}

// Handles an incoming handshake response from a remote peer.
func (h *handshakeProtocolImpl) onHandleIncomingHandshakeResponse(msg IncomingMessage) {
	respData := &pb.HandshakeData{}
	err := proto.Unmarshal(msg.Payload(), respData)
	if err != nil {
		h.swarm.GetLocalNode().Warning("invalid incoming handshake resp bin data", err)
		return
	}

	sessionID := hex.EncodeToString(respData.SessionId)

	h.swarm.GetLocalNode().Info("Incoming handshake response for session id %s", sessionID)

	// this is the session data we sent to the node
	sessionRequestData := h.pendingSessions[sessionID]

	if sessionRequestData == nil {
		h.swarm.GetLocalNode().Error("Expected to have data about this session - aborting")
		return
	}

	err = processHandshakeResponse(sessionRequestData.LocalNode(), sessionRequestData.Peer(),
		sessionRequestData.Session(), respData)
	if err != nil {
		// can't establish session - set error
		sessionRequestData.SetError(err)
	}

	// no longer pending if error or if sesssion created
	h.deletePendingSessionByID <- sessionID

	h.sessionStateChanged <- sessionRequestData
	h.swarm.GetLocalNode().Info("Locally initiated session established! Session id: %s :-)", sessionID)
}

/////////////////////////// functions below - they don't provide Handshake protocol state

// Generate handshake and session data between node and remoteNode
// Returns handshake data to send to removeNode and a network session data object that includes the session enc/dec sym key and iv
// Node that NetworkSession is not yet authenticated - this happens only when the handshake response is processed and authenticated
// This is called by node1 (initiator)
func generateHandshakeRequestData(node LocalNode, remoteNode Peer) (*pb.HandshakeData, NetworkSession, error) {

	// we use the Elliptic Curve Encryption Scheme
	// https://en.wikipedia.org/wiki/Integrated_Encryption_Scheme

	data := &pb.HandshakeData{
		Protocol: HandshakeReq,
	}

	data.ClientVersion = nodeconfig.ClientVersion
	data.NetworkID = int32(node.Config().NetworkID)
	data.Timestamp = time.Now().Unix()

	iv := make([]byte, 16)
	if _, err := io.ReadFull(rand.Reader, iv); err != nil {
		return nil, nil, err
	}

	data.SessionId = iv
	data.Iv = iv

	// attempt to refresh the node's public ip address
	node.RefreshPubTCPAddress()

	// announce the pub ip address of the node - not the private one
	data.TcpAddress = node.PubTCPAddress()

	ephemeral, err := btcec.NewPrivateKey(btcec.S256())
	if err != nil {
		return nil, nil, err
	}

	data.NodePubKey = node.PublicKey().InternalKey().SerializeUncompressed()

	// start shared key generation
	ecdhKey := btcec.GenerateSharedSecret(ephemeral, remoteNode.PublicKey().InternalKey())
	derivedKey := sha512.Sum512(ecdhKey)
	keyE := derivedKey[:32] // used for aes enc/dec
	keyM := derivedKey[32:] // used for hmac

	data.PubKey = ephemeral.PubKey().SerializeUncompressed()

	// start HMAC-SHA-256
	hm := hmac.New(sha256.New, keyM)
	hm.Write(iv) // iv is hashed
	data.Hmac = hm.Sum(nil)
	data.Sign = ""

	// sign corpus - marshall data without the signature to protobufs3 binary format
	bin, err := proto.Marshal(data)
	if err != nil {
		return nil, nil, err
	}

	sign, err := node.PrivateKey().Sign(bin)
	if err != nil {
		return nil, nil, err
	}

	// place signature - hex encoded string
	data.Sign = hex.EncodeToString(sign)

	// create local session data - iv and key
	session, err := NewNetworkSession(iv, keyE, keyM, data.PubKey, node.String(), remoteNode.String())

	if err != nil {
		return nil, nil, err
	}

	return data, session, nil
}

// Authenticate that the sender node generated the signed data
func authenticateSenderNode(req *pb.HandshakeData) error {

	// get public key from data
	snderPubKey, err := crypto.NewPublicKey(req.NodePubKey)
	if err != nil {
		return err
	}

	// copy the signature
	sig := req.Sign

	// recreate the signed binary corpus - all data without the signature
	req.Sign = ""
	bin, err := proto.Marshal(req)
	if err != nil {
		return err
	}

	// Verify that the signed data was signed by the public key
	v, err := snderPubKey.VerifyString(bin, sig)
	if err != nil {
		return err
	}

	if !v {
		return errors.New("failed to verify data is from claimed sender node")
	}

	return nil

}

// Process a session handshake request data from remoteNode r
// Returns Handshake data to send to r and a network session data object that includes the session sym  enc/dec key
// This is called by responder in the handshake protocol (node2)
func processHandshakeRequest(node LocalNode, r Peer, req *pb.HandshakeData) (*pb.HandshakeData, NetworkSession, error) {

	// check that received clientversion is valid client string
	reqVersion := strings.Split(req.ClientVersion, "/")
	if len(reqVersion) != 2 {
		//node.Warning("Dropping incoming message - invalid client version")
		return nil, nil, errors.New("dropping incoming handshake - invalid client version")
	}

	// compare that version to the min client version in config
	ok, err := CheckNodeVersion(reqVersion[1], nodeconfig.MinClientVersion)
	if err != nil || !ok {
		return nil, nil, errors.New("dropping incoming handshake - invalid client version")
	}

	// make sure we're on the same network
	if int(req.NetworkID) != node.Config().NetworkID {
		return nil, nil, errors.New("dropping incoming handshake - different networkID version")
		//TODO : drop and blacklist this sender
	}

	// ephemeral public key
	pubkey, err := btcec.ParsePubKey(req.PubKey, btcec.S256())
	if err != nil {
		return nil, nil, err
	}

	// generate shared secret
	ecdhKey := btcec.GenerateSharedSecret(node.PrivateKey().InternalKey(), pubkey)
	derivedKey := sha512.Sum512(ecdhKey)
	keyE := derivedKey[:32] // this is the encryption key
	keyM := derivedKey[32:]

	// verify mac
	hm := hmac.New(sha256.New, keyM)
	hm.Write(req.Iv)
	expectedMAC := hm.Sum(nil)
	if !hmac.Equal(req.Hmac, expectedMAC) {
		return nil, nil, errors.New("invalid hmac")
	}

	// verify signature
	sig := req.Sign
	req.Sign = ""
	bin, err := proto.Marshal(req)
	if err != nil {
		return nil, nil, err
	}

	// verify against remote node public key
	v, err := r.PublicKey().VerifyString(bin, sig)
	if err != nil {
		return nil, nil, err
	}

	if !v {
		return nil, nil, errors.New("invalid signature")
	}

	// set session data - it is authenticated as far as local node is concerned
	// we might consider waiting with auth until node1 responded to the ack message but it might be an overkill
	s, err := NewNetworkSession(req.Iv, keyE, keyM, req.PubKey, node.String(), r.String())
	if err != nil {
		return nil, nil, err

	}
	s.SetAuthenticated(true)

	// update remote node session here
	r.GetSessions()[s.String()] = s

	// generate ack resp data
	iv := make([]byte, 16)
	if _, err := io.ReadFull(rand.Reader, iv); err != nil {
		return nil, nil, err
	}

	hm1 := hmac.New(sha256.New, keyM)
	hm1.Write(iv)
	hmac1 := hm1.Sum(nil)

	// attempt to refresh the node's public ip address
	node.RefreshPubTCPAddress()

	resp := &pb.HandshakeData{
		ClientVersion: nodeconfig.ClientVersion,
		NetworkID:     int32(node.Config().NetworkID),
		SessionId:     req.SessionId,
		Timestamp:     time.Now().Unix(),
		NodePubKey:    node.PublicKey().InternalKey().SerializeUncompressed(),
		PubKey:        req.PubKey,
		Iv:            iv,
		Hmac:          hmac1,
		TcpAddress:    node.PubTCPAddress(),
		Protocol:      HandshakeResp,
		Sign:          "",
	}

	// sign corpus - marshall data without the signature to protobufs3 binary format and sign it
	bin, err = proto.Marshal(resp)
	if err != nil {
		return nil, nil, err
	}

	sign, err := node.PrivateKey().Sign(bin)
	if err != nil {
		return nil, nil, err
	}

	// place signature in response
	resp.Sign = hex.EncodeToString(sign)

	return resp, s, nil
}

// Process handshake protocol response. This is called by initiator (node1) to handle response from node2
// and to establish the session
// Side effect - passed network session is set to authenticated
func processHandshakeResponse(node LocalNode, r Peer, s NetworkSession, resp *pb.HandshakeData) error {

	// verified shared public secret
	if !bytes.Equal(resp.PubKey, s.PubKey()) {
		return errors.New("shared secret mismatch")
	}

	// verify response is for the expected session id
	if !bytes.Equal(s.ID(), resp.SessionId) {
		return errors.New("expected same session id")
	}

	// verify mac
	hm := hmac.New(sha256.New, s.KeyM())
	hm.Write(resp.Iv)
	expectedMAC := hm.Sum(nil)

	if !hmac.Equal(resp.Hmac, expectedMAC) {
		return errors.New("invalid hmac")
	}

	// verify signature
	sig := resp.Sign
	resp.Sign = ""
	bin, err := proto.Marshal(resp)
	if err != nil {
		return err
	}

	// we verify against remote node public key
	v, err := r.PublicKey().VerifyString(bin, sig)
	if err != nil {
		return err
	}

	if !v {
		return errors.New("invalid signature")
	}

	// Session is now authenticated
	s.SetAuthenticated(true)

	// update remote node session here
	r.GetSessions()[s.String()] = s

	return nil
}
