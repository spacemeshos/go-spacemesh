package net

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

	"github.com/gogo/protobuf/proto"
	"github.com/spacemeshos/go-spacemesh/crypto"
	"github.com/spacemeshos/go-spacemesh/p2p/nodeconfig"
	"github.com/spacemeshos/go-spacemesh/p2p/pb"
	"github.com/btcsuite/btcd/btcec"
	"strings"
	"fmt"
	"github.com/spacemeshos/go-spacemesh/p2p/version"
)

// HandshakeReq specifies the handshake protocol request message identifier. pattern is [protocol][version][method-name].
const HandshakeReq = "/handshake/1.0/handshake-req/"

// HandshakeResp specifies the handshake protocol response message identifier.
const HandshakeResp = "/handshake/1.0/handshake-resp/"

// HandshakeData defines the handshake protocol capabilities.
//type HandshakeData interface {
//	LocalNode() LocalNode
//	Peer() Peer
//	Session() NetworkSession
//	GetError() error
//	SetError(err error)
//}
//
//// NewHandshakeData creates new HandshakeData with the provided data.
//func NewHandshakeData(localNode LocalNode, peer Peer, session NetworkSession, err error) HandshakeData {
//	return &handshakeDataImp{
//		localNode: localNode,
//		peer:      peer,
//		session:   session,
//		err:       err,
//	}
//}
//
//type handshakeDataImp struct {
//	localNode LocalNode
//	peer      Peer
//	session   NetworkSession
//	err       error
//}
//
//// LocalNode returns the protocol's local node.
//func (n *handshakeDataImp) LocalNode() LocalNode {
//	return n.localNode
//}
//
//// Peer returns the protocol's remote node.
//func (n *handshakeDataImp) Peer() Peer {
//	return n.peer
//}
//
//// Session returns the network session between the local and the remote nodes.
//func (n *handshakeDataImp) Session() NetworkSession {
//	return n.session
//}
//
//// GetError returns the last protocol error or nil otherwise.
//func (n *handshakeDataImp) GetError() error {
//	return n.err
//}
//
//// SetError sets protocol error. The last error set is returned by GetError().
//func (n *handshakeDataImp) SetError(err error) {
//	n.err = err
//}
//
//type pendingSessionRequest struct {
//	SessionID string
//	Callback  chan bool
//}
//
//// HandshakeProtocol specifies the handshake protocol.
//// Node1 -> Node 2: Req(HandshakeData)
//// Node2 -> Node 1: Resp(HandshakeData)
//// After response is processed by node1 both sides have an auth session with a secret ephemeral aes sym key.
//type HandshakeProtocol interface {
//	CreateSession(peer Peer)
//	RegisterNewSessionCallback(callback chan HandshakeData) // register a channel to receive session state changes
//	SessionIsPending(sessionID string, callback chan bool)
//}
//
//type handshakeProtocolImpl struct {
//
//	// state
//	swarm               Swarm
//	newSessionCallbacks []chan HandshakeData     // a list of callback channels for new sessions
//	pendingSessions     map[string]HandshakeData // sessions pending authentication
//	pendingMutex        sync.RWMutex
//
//	// ops
//	incomingHandshakeRequests MessagesChan
//	incomingHandsakeResponses MessagesChan
//	registerSessionCallback   chan chan HandshakeData
//	sessionStateChanged       chan HandshakeData
//	pendingCheckRequest       chan pendingSessionRequest
//}
//
//// NewHandshakeProtocol Creates a new Handshake protocol for the provided swarm.
//func NewHandshakeProtocol(s Swarm) HandshakeProtocol {
//
//	h := &handshakeProtocolImpl{
//		swarm:           s,
//		pendingSessions: make(map[string]HandshakeData),
//
//		incomingHandshakeRequests: make(MessagesChan, 20),
//		incomingHandsakeResponses: make(chan IncomingMessage, 20),
//		registerSessionCallback:   make(chan chan HandshakeData, 2),
//		newSessionCallbacks:       make([]chan HandshakeData, 0), // start with empty slice
//		sessionStateChanged:       make(chan HandshakeData, 3),
//		pendingCheckRequest:       make(chan pendingSessionRequest, 3),
//	}
//
//	go h.processEvents()
//
//	// protocol demuxer registration
//
//	s.GetDemuxer().RegisterProtocolHandler(
//		ProtocolRegistration{Protocol: HandshakeReq, Handler: h.incomingHandshakeRequests})
//
//	s.GetDemuxer().RegisterProtocolHandler(
//		ProtocolRegistration{Protocol: HandshakeResp, Handler: h.incomingHandsakeResponses})
//
//	return h
//}
//
//// RegisterNewSessionCallback registers a callback to baclled when a new session is established by the protocol.
//func (h *handshakeProtocolImpl) RegisterNewSessionCallback(callback chan HandshakeData) {
//	h.swarm.GetLocalNode().Debug("New session callback registered.")
//	h.registerSessionCallback <- callback
//}
//
//func (h *handshakeProtocolImpl) stateChanged(hd HandshakeData) {
//	go func() { h.sessionStateChanged <- hd }()
//}
//
//// CreateSession is called to initiate the handshake protocol between the local node and a remote peer.
//func (h *handshakeProtocolImpl) CreateSession(peer Peer) {
//
//	data, session, err := generateHandshakeRequestData(h.swarm.GetLocalNode(), peer)
//
//	handshakeData := NewHandshakeData(h.swarm.GetLocalNode(), peer, session, err)
//
//	if err != nil {
//		h.stateChanged(handshakeData)
//		return
//	}
//
//	payload, err := proto.Marshal(data)
//	if err != nil {
//		h.stateChanged(handshakeData)
//		return
//	}
//
//	// so we can match handshake responses with the session
//	h.pendingMutex.Lock()
//	h.pendingSessions[handshakeData.Session().String()] = handshakeData
//	h.pendingMutex.Unlock()
//
//	h.swarm.GetLocalNode().Debug("Creating session handshake request session id: %s", session.String())
//
//	h.swarm.sendHandshakeMessage(SendMessageReq{
//		ReqID:    session.ID(),
//		PeerID:   peer.String(),
//		Payload:  payload,
//		Callback: nil,
//	})
//
//	//TODO: We don't really need to announce half state
//	h.stateChanged(handshakeData)
//}
//
//// internal event processing handler
//func (h *handshakeProtocolImpl) processEvents() {
//	for {
//		select {
//		case m := <-h.incomingHandshakeRequests:
//			h.onHandleIncomingHandshakeRequest(m)
//
//		case m := <-h.incomingHandsakeResponses:
//			h.onHandleIncomingHandshakeResponse(m)
//
//		case c := <-h.registerSessionCallback:
//			h.newSessionCallbacks = append(h.newSessionCallbacks, c)
//
//		case pcr := <-h.pendingCheckRequest:
//			p := h.sessionIsPending(pcr.SessionID)
//			go func(pending bool) { pcr.Callback <- pending }(p)
//
//		case s := <-h.sessionStateChanged:
//			for _, c := range h.newSessionCallbacks {
//				go func(c chan HandshakeData) { c <- s }(c)
//			}
//		}
//	}
//}
//
//func (h *handshakeProtocolImpl) sessionIsPending(sessionID string) bool {
//	_, ok := h.pendingSessions[sessionID]
//	return ok
//}
//
//// SessionIsPending checks if there's a pending session for this session ID
//func (h *handshakeProtocolImpl) SessionIsPending(sessionID string, callback chan bool) {
//	h.pendingMutex.RLock()
//	_, e := h.pendingSessions[sessionID]
//	h.pendingMutex.Unlock()
//	callback <- e
//}
//
//// Handles a remote handshake request.
//func (h *handshakeProtocolImpl) onHandleIncomingHandshakeRequest(msg IncomingMessage) {
//
//	data := &pb.HandshakeData{}
//	err := proto.Unmarshal(msg.Payload(), data)
//	if err != nil {
//		h.swarm.GetLocalNode().Warning("invalid incoming handshake request bin data", err)
//		return
//	}
//
//	if msg.Sender() == nil {
//		// we don't know about this remote node - create a new one for it using the info it sent
//		// and add it to the swarm
//	}
//
//	respData, session, err := ProcessHandshakeRequest(h.swarm.GetLocalNode(), msg.Sender(), data)
//
//	// we have a new session started by a remote node
//	handshakeData := NewHandshakeData(h.swarm.GetLocalNode(), msg.Sender(), session, err)
//
//	if err != nil {
//		// failed to process request
//		handshakeData.SetError(err)
//		h.stateChanged(handshakeData)
//		return
//	}
//
//	payload, err := proto.Marshal(respData)
//	if err != nil {
//		handshakeData.SetError(err)
//		h.stateChanged(handshakeData)
//		return
//	}
//
//	// todo: support callback errors
//
//	// send response back to sender
//	h.swarm.sendHandshakeMessage(SendMessageReq{
//		ReqID:    session.ID(),
//		PeerID:   msg.Sender().String(),
//		Payload:  payload,
//		Callback: nil,
//	})
//
//	// we have an active session initiated by a remote node
//	h.stateChanged(handshakeData)
//
//	h.swarm.GetLocalNode().Debug("Remotely initiated session established. Session id: %s :-)", session.String())
//
//}
//
//// Handles an incoming handshake response from a remote peer.
//func (h *handshakeProtocolImpl) onHandleIncomingHandshakeResponse(msg IncomingMessage) {
//	respData := &pb.HandshakeData{}
//	err := proto.Unmarshal(msg.Payload(), respData)
//	if err != nil {
//		h.swarm.GetLocalNode().Warning("invalid incoming handshake resp bin data", err)
//		return
//	}
//
//	sessionID := hex.EncodeToString(respData.SessionId)
//
//	h.swarm.GetLocalNode().Debug("Incoming handshake response for session id %s", sessionID)
//
//	// this is the session data we sent to the node
//	h.pendingMutex.RLock()
//	sessionRequestData := h.pendingSessions[sessionID]
//	h.pendingMutex.RUnlock()
//
//	if sessionRequestData == nil {
//		h.swarm.GetLocalNode().Error("Expected to have data about this session - aborting")
//		return
//	}
//
//	err = processHandshakeResponse(sessionRequestData.LocalNode(), sessionRequestData.Peer(),
//		sessionRequestData.Session(), respData)
//	if err != nil {
//		// can't establish session - set error
//		sessionRequestData.SetError(err)
//	}
//
//	// no longer pending if error or if sesssion created
//	h.pendingMutex.Lock()
//	delete(h.pendingSessions, sessionID)
//	h.pendingMutex.Unlock()
//
//	h.stateChanged(sessionRequestData)
//	h.swarm.GetLocalNode().Debug("Locally initiated session established! Session id: %s :-)", sessionID)
//}

/////////////////////////// functions below - they don't provide Handshake protocol state

// Generate handshake and session data between node and remoteNode
// Returns handshake data to send to removeNode and a network session data object that includes the session enc/dec sym key and iv
// Node that NetworkSession is not yet authenticated - this happens only when the handshake response is processed and authenticated
// This is called by node1 (initiator)
func GenerateHandshakeRequestData(localPublicKey crypto.PublicKey, localPrivateKey crypto.PrivateKey, remotePublicKey crypto.PublicKey,
	networkId int32) (*pb.HandshakeData, NetworkSession, error) {

	// we use the Elliptic Curve Encryption Scheme
	// https://en.wikipedia.org/wiki/Integrated_Encryption_Scheme

	data := &pb.HandshakeData{
		Protocol: HandshakeReq,
	}

	data.ClientVersion = nodeconfig.ClientVersion
	data.NetworkID = networkId
	data.Timestamp = time.Now().Unix()

	iv := make([]byte, 16)
	if _, err := io.ReadFull(rand.Reader, iv); err != nil {
		return nil, nil, err
	}

	data.SessionId = iv
	data.Iv = iv

	/*
	// attempt to refresh the node's public ip address
	node.RefreshPubTCPAddress()

	// announce the pub ip address of the node - not the private one
	data.TcpAddress = node.PubTCPAddress()
*/
	ephemeral, err := btcec.NewPrivateKey(btcec.S256())
	if err != nil {
		return nil, nil, err
	}

	data.NodePubKey = localPublicKey.InternalKey().SerializeUncompressed()

	// start shared key generation
	ecdhKey := btcec.GenerateSharedSecret(ephemeral, remotePublicKey.InternalKey())
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

	sign, err := localPrivateKey.Sign(bin)
	if err != nil {
		return nil, nil, err
	}

	// place signature - hex encoded string
	data.Sign = hex.EncodeToString(sign)

	// create local session data - iv and key
	session, err := NewNetworkSession(iv, keyE, keyM, data.PubKey, localPublicKey.String(), remotePublicKey.String())

	if err != nil {
		return nil, nil, err
	}

	return data, session, nil
}

// Handles an incoming handshake response from a remote peer.
//func HandleHandshakeResponse(msg []byte, logger *logging.Logger ) {
//	respData := &pb.HandshakeData{}
//	err := proto.Unmarshal(msg.Payload(), respData)
//	if err != nil {
//		h.swarm.GetLocalNode().Warning("invalid incoming handshake resp bin data", err)
//		return
//	}
//
//	sessionID := hex.EncodeToString(respData.SessionId)
//
//	h.swarm.GetLocalNode().Debug("Incoming handshake response for session id %s", sessionID)
//
//	err = processHandshakeResponse(sessionRequestData.LocalNode(), sessionRequestData.Peer(),
//		sessionRequestData.Session(), respData)
//	if err != nil {
//		// can't establish session - set error
//		sessionRequestData.SetError(err)
//	}
//
//	// no longer pending if error or if sesssion created
//	h.pendingMutex.Lock()
//	delete(h.pendingSessions, sessionID)
//	h.pendingMutex.Unlock()
//
//	h.stateChanged(sessionRequestData)
//	h.swarm.GetLocalNode().Debug("Locally initiated session established! Session id: %s :-)", sessionID)
//}

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
func ProcessHandshakeRequest(networkId int32, lPub crypto.PublicKey, lPri crypto.PrivateKey, rPub crypto.PublicKey, req *pb.HandshakeData) (*pb.HandshakeData, NetworkSession, error) {
	// check that received clientversion is valid client string
	reqVersion := strings.Split(req.ClientVersion, "/")
	if len(reqVersion) != 2 {
		//node.Warning("Dropping incoming message - invalid client version")
		return nil, nil, errors.New("invalid client version")
	}

	// compare that version to the min client version in config
	ok, err := version.CheckNodeVersion(reqVersion[1], nodeconfig.MinClientVersion)
	if err == nil && !ok {
		return nil, nil, errors.New("unsupported client version")
	}
	if err != nil {
		return nil, nil, fmt.Errorf("invalid client version, err: %v", err)
	}

	// make sure we're on the same network
	if req.NetworkID != networkId {
		return nil, nil, fmt.Errorf("request net id (%d) is different than local net id (%d)", req.NetworkID, networkId)
		//TODO : drop and blacklist this sender
	}

	// ephemeral public key
	pubkey, err := btcec.ParsePubKey(req.PubKey, btcec.S256())
	if err != nil {
		return nil, nil, err
	}

	// generate shared secret
	ecdhKey := btcec.GenerateSharedSecret(lPri.InternalKey(), pubkey)
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
	v, err := rPub.VerifyString(bin, sig)
	if err != nil {
		return nil, nil, err
	}

	if !v {
		return nil, nil, errors.New("invalid signature")
	}

	// set session data - it is authenticated as far as local node is concerned
	// we might consider waiting with auth until node1 responded to the ack message but it might be an overkill
	s, err := NewNetworkSession(req.Iv, keyE, keyM, req.PubKey, lPub.String(), rPub.String())
	if err != nil {
		return nil, nil, err

	}
	//s.SetAuthenticated(true) -- TODO remove

	// update remote node session here
	//r.UpdateSession(s.String(), s) -- TODO remove

	// generate ack resp data
	iv := make([]byte, 16)
	if _, err := io.ReadFull(rand.Reader, iv); err != nil {
		return nil, nil, err
	}

	hm1 := hmac.New(sha256.New, keyM)
	hm1.Write(iv)
	hmac1 := hm1.Sum(nil)

	// TODO consider how to handle IP addr changing
	// attempt to refresh the node's public ip address
	//node.RefreshPubTCPAddress()

	resp := &pb.HandshakeData{
		ClientVersion: nodeconfig.ClientVersion,
		NetworkID:     networkId,
		SessionId:     req.SessionId,
		Timestamp:     time.Now().Unix(),
		NodePubKey:    lPub.InternalKey().SerializeUncompressed(),
		PubKey:        req.PubKey,
		Iv:            iv,
		Hmac:          hmac1,
		TcpAddress:    "1.1.1.1", //node.PubTCPAddress(), TODO what is it used for?
		Protocol:      HandshakeResp,
		Sign:          "",
	}

	// sign corpus - marshall data without the signature to protobufs3 binary format and sign it
	bin, err = proto.Marshal(resp)
	if err != nil {
		return nil, nil, err
	}

	sign, err := lPri.Sign(bin)
	if err != nil {
		return nil, nil, err
	}

	// place signature in response
	resp.Sign = hex.EncodeToString(sign)

	return resp, s, nil
}

// Handles a remote handshake request.
//func HandleIncomingHandshakeRequest(lPub crypto.PublicKey, lPri crypto.PrivateKey, msg IncomingMessage) error {
//	errMsg := "failed to handle handshake request"
//	data := &pb.HandshakeData{}
//	err := proto.Unmarshal(msg.Message, data)
//	if err != nil {
//		return fmt.Errorf("%s. err: %v", errMsg, err)
//	}
//
//	err = authenticateSenderNode(data)
//	if err != nil {
//		return fmt.Errorf("%s. err: %v", errMsg, err)
//	}
//
//	respData, session, err := ProcessHandshakeRequest(msg.Connection.net.GetNetworkId(), lPub, lPri, msg.Connection.remotePub, data)
//
//	// we have a new session started by a remote node
//	//handshakeData := NewHandshakeData(h.swarm.GetLocalNode(), msg.Sender(), session, err)
//
//	//if err != nil {
//	//	// failed to process request
//	//	handshakeData.SetError(err)
//	//	h.stateChanged(handshakeData)
//	//	return
//	//}
//
//	payload, err := proto.Marshal(respData)
//	if err != nil {
//		//handshakeData.SetError(err)
//		//h.stateChanged(handshakeData)
//		return fmt.Errorf("%s. err: %v", errMsg, err)
//	}
//
//	// todo: support callback errors
//
//	msg.Connection.Send(payload, session.ID())
//	// send response back to sender
//	//h.swarm.sendHandshakeMessage(SendMessageReq{
//	//	ReqID:    session.ID(),
//	//	PeerID:   msg.Sender().String(),
//	//	Payload:  payload,
//	//	Callback: nil,
//	//})
//
//	// we have an active session initiated by a remote node
//	//h.stateChanged(handshakeData)
//
//	//h.swarm.GetLocalNode().Debug("Remotely initiated session established. Session id: %s :-)", session.String())
//	return nil
//}

// Process handshake protocol response. This is called by initiator (node1) to handle response from node2
// and to establish the session
// Side effect - passed network session is set to authenticated
func ProcessHandshakeResponse(remotePub crypto.PublicKey, s NetworkSession, resp *pb.HandshakeData) error {

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
	v, err := remotePub.VerifyString(bin, sig)
	if err != nil {
		return err
	}

	if !v {
		return errors.New("invalid signature")
	}

	// TODO does peer need to hold all sessions?
	// update remote node session here
	//r.UpdateSession(s.String(), s)

	return nil
}
