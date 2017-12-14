package swarm

import (
	"bytes"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"crypto/sha512"
	"encoding/hex"
	"errors"
	"github.com/UnrulyOS/go-unruly/log"
	"github.com/UnrulyOS/go-unruly/p2p2/keys"
	"github.com/UnrulyOS/go-unruly/p2p2/swarm/pb"
	"github.com/btcsuite/btcd/btcec"
	"github.com/golang/protobuf/proto"
	"io"
)

// pattern: [protocol][version][method-name]
const HandshakeReq = "/handshake/1.0/handshake-req/"
const HandshakeResp = "/handshake/1.0/handshake-resp/"

type HandshakeData interface {
	LocalNode() LocalNode
	RemoteNode() RemoteNode
	Session() NetworkSession
	GetError() error
	SetError(err error)
}

func NewHandshakeData(localNode LocalNode, remoteNode RemoteNode, session NetworkSession, err error) HandshakeData {
	return &handshakeDataImp{
		localNode:  localNode,
		remoteNode: remoteNode,
		session:    session,
		err:        err,
	}
}

type handshakeDataImp struct {
	localNode  LocalNode
	remoteNode RemoteNode
	session    NetworkSession
	err        error
}

func (n *handshakeDataImp) LocalNode() LocalNode {
	return n.localNode
}

func (n *handshakeDataImp) RemoteNode() RemoteNode {
	return n.remoteNode
}

func (n *handshakeDataImp) Session() NetworkSession {
	return n.session
}

func (n *handshakeDataImp) GetError() error {
	return n.err
}

func (n *handshakeDataImp) SetError(err error) {
	n.err = err
}

// Handshake protocol
type HandshakeProtocol interface {
	CreateSession(remoteNode RemoteNode)
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
	deletePendingSessionById  chan string
	sessionStateChanged       chan HandshakeData
}

func NewHandshakeProtocol(s Swarm) HandshakeProtocol {

	h := &handshakeProtocolImpl{
		swarm:                     s,
		pendingSessions:           make(map[string]HandshakeData),
		incomingHandshakeRequests: make(MessagesChan, 20),
		incomingHandsakeResponses: make(chan IncomingMessage, 20),
		registerSessionCallback:   make(chan chan HandshakeData, 2),
		newSessionCallbacks:       make([]chan HandshakeData, 1),
		deletePendingSessionById:  make(chan string, 5),
		sessionStateChanged:       make(chan HandshakeData, 3),
	}

	go h.processEvents()

	// protocol demuxer registration

	s.GetDemuxer().RegisterProtocolHandler(
		ProtocolRegistration{protocol: HandshakeReq, handler: h.incomingHandshakeRequests})

	s.GetDemuxer().RegisterProtocolHandler(
		ProtocolRegistration{protocol: HandshakeResp, handler: h.incomingHandsakeResponses})

	return h
}

func (h *handshakeProtocolImpl) RegisterNewSessionCallback(callback chan HandshakeData) {
	h.registerSessionCallback <- callback
}

func (h *handshakeProtocolImpl) CreateSession(remoteNode RemoteNode) {

	data, session, err := GenereateHandshakeRequestData(h.swarm.LocalNode(), remoteNode)

	handshakeData := NewHandshakeData(h.swarm.LocalNode(), remoteNode, session, err)

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

	h.swarm.SendMessage(SendMessageReq{
		reqId:        session.String(),
		remoteNodeId: remoteNode.String(),
		payload:      payload,
	})

	h.sessionStateChanged <- handshakeData

}

func (h *handshakeProtocolImpl) processEvents() {
	for {
		select {
		case m := <-h.incomingHandshakeRequests:
			h.onHandleIncomingHandshakeRequest(m)

		case m := <-h.incomingHandsakeResponses:
			h.onHandleIncomingHandshakeResponse(m)

		case r := <-h.registerSessionCallback:
			h.newSessionCallbacks = append(h.newSessionCallbacks, r)

		case s := <-h.addPendingSession:
			h.pendingSessions[s.Session().String()] = s

		case k := <-h.deletePendingSessionById:
			delete(h.pendingSessions, k)

		case s := <-h.sessionStateChanged:
			for _, c := range h.newSessionCallbacks {
				c <- s
			}
		}
	}
}

func (h *handshakeProtocolImpl) onHandleIncomingHandshakeRequest(msg IncomingMessage) {

	data := &pb.HandshakeData{}
	err := proto.Unmarshal(msg.Payload(), data)
	if err != nil {
		log.Warning("Invalid incoming handshake request bin data: %v", err)
		return
	}

	if msg.Sender() == nil {
		// we don't know about this remote node - create a new one for it using the info it sent
		// and add it to the swarm
	}

	respData, session, err := ProcessHandshakeRequest(h.swarm.LocalNode(), msg.Sender(), data)

	// we have a new session started by a remote node
	handshakeData := NewHandshakeData(h.swarm.LocalNode(), msg.Sender(), session, err)

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

	// send response back to sender
	h.swarm.SendMessage(SendMessageReq{
		reqId:        session.String(),
		remoteNodeId: msg.Sender().String(),
		payload:      payload,
	})

	// we have an active session initiated by a remote node
	h.sessionStateChanged <- handshakeData
}

func (h *handshakeProtocolImpl) onHandleIncomingHandshakeResponse(msg IncomingMessage) {
	respData := &pb.HandshakeData{}
	err := proto.Unmarshal(msg.Payload(), respData)
	if err != nil {
		log.Warning("invalid incoming handshake resp bin data: %v", err)
		return
	}

	sessionId := hex.EncodeToString(respData.Iv)

	// this is the session data we sent to the node
	sessionRequestData := h.pendingSessions[sessionId]

	if sessionRequestData == nil {
		log.Warning("can't match this response with a handshake request - aborting")
		return
	}

	err = ProcessHandshakeResponse(sessionRequestData.LocalNode(), sessionRequestData.RemoteNode(),
		sessionRequestData.Session(), respData)
	if err != nil {
		// can't establish session - set error
		sessionRequestData.SetError(err)
	}

	// no longer pending if error or if sesssion created
	h.deletePendingSessionById <- sessionId

	h.sessionStateChanged <- sessionRequestData
	log.Info("Session established")
}

// Handshake protocol:
// Node1 -> Node 2: Req(HandshakeData)
// Node2 -> Node 1: Resp(HandshakeData)
// After response is processed by node1 both sides have an auth session with a secret ephemeral aes sym key

// Generate handshake and session data between node and remoteNode
// Returns handshake data to send to removeNode and a network session data object that includes the session enc/dec sym key and iv
// Node that NetworkSession is not yet authenticated - this happens only when the handshake response is processed and authenticated
// This is called by node1 (initiator)
func GenereateHandshakeRequestData(node LocalNode, remoteNode RemoteNode) (*pb.HandshakeData, NetworkSession, error) {

	// we use the Elliptic Curve Encryption Scheme
	// https://en.wikipedia.org/wiki/Integrated_Encryption_Scheme

	data := &pb.HandshakeData{
		Protocol: HandshakeReq,
	}

	iv := make([]byte, 16)
	if _, err := io.ReadFull(rand.Reader, iv); err != nil {
		return nil, nil, err
	}

	data.SessionId = iv
	data.Iv = iv
	data.TcpAddress = node.TcpAddress()

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

	// sign corupus - marshall data without the signature to protobufs3 binary format
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

// Authenticate the sender node generated the signed data
func AuthenticateSenderNode(req *pb.HandshakeData) error {

	snderPubKey, err := keys.NewPublicKey(req.NodePubKey)
	if err != nil {
		return err
	}

	// verify signature
	sigData, err := hex.DecodeString(req.Sign)
	if err != nil {
		return err
	}

	req.Sign = ""
	bin, err := proto.Marshal(req)
	if err != nil {
		return err
	}

	// restore back signature
	req.Sign = hex.EncodeToString(sigData)

	// we verify against the remote node public key
	v, err := snderPubKey.Verify(bin, sigData)
	if err != nil {
		return err
	}

	if !v {
		return errors.New("invalid data")
	}

	return nil

}

// Process a session handshake request data from remoteNode r
// Returns Handshake data to send to r and a network session data object that includes the session sym  enc/dec key
// This is called by responder in the handshake protocol (node2)
func ProcessHandshakeRequest(node LocalNode, r RemoteNode, req *pb.HandshakeData) (*pb.HandshakeData, NetworkSession, error) {

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
	sigData, err := hex.DecodeString(req.Sign)

	if err != nil {
		return nil, nil, err
	}

	req.Sign = ""
	bin, err := proto.Marshal(req)
	if err != nil {
		return nil, nil, err
	}

	// we verify against the remote node public key
	v, err := r.PublicKey().Verify(bin, sigData)
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

	resp := &pb.HandshakeData{
		SessionId:  req.SessionId,
		NodePubKey: node.PublicKey().InternalKey().SerializeUncompressed(),
		PubKey:     req.PubKey,
		Iv:         iv,
		Hmac:       hmac1,
		TcpAddress: node.TcpAddress(),
		Protocol:   HandshakeResp,
		Sign:       "",
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
// Side-effect - passed network session is set to authenticated
func ProcessHandshakeResponse(node LocalNode, r RemoteNode, s NetworkSession, resp *pb.HandshakeData) error {

	// verified shared public secret
	if !bytes.Equal(resp.PubKey, s.PubKey()) {
		return errors.New("shared secret mismatch")
	}

	// verify response is for the expected session id
	if !bytes.Equal(s.Id(), resp.SessionId) {
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
	sigData, err := hex.DecodeString(resp.Sign)

	if err != nil {
		return err
	}

	resp.Sign = ""
	bin, err := proto.Marshal(resp)
	if err != nil {
		return err
	}

	// we verify against the remote node public key
	v, err := r.PublicKey().Verify(bin, sigData)
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
