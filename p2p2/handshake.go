package p2p2

import (
	"bytes"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"crypto/sha512"
	"encoding/hex"
	"errors"
	"github.com/UnrulyOS/go-unruly/p2p2/pb"
	"github.com/btcsuite/btcd/btcec"
	"github.com/golang/protobuf/proto"
	"io"
)

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

	data := &pb.HandshakeData{}

	iv := make([]byte, 16)
	if _, err := io.ReadFull(rand.Reader, iv); err != nil {
		return nil, nil, err
	}
	data.Iv = iv

	ephemeral, err := btcec.NewPrivateKey(btcec.S256())
	if err != nil {
		return nil, nil, err
	}

	// new ephemeral private key
	data.NodePubKey = node.PublicKey().InternalKey().SerializeUncompressed()

	// start shared key generation
	ecdhKey := btcec.GenerateSharedSecret(ephemeral, remoteNode.PublicKey().InternalKey())
	derivedKey := sha512.Sum512(ecdhKey)
	keyE := derivedKey[:32] // used for aes enc/dec
	keyM := derivedKey[32:] // used for hmac

	data.SharedSecret = ephemeral.PubKey().SerializeUncompressed()

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
	session := NewNetworkSession(iv, keyE, keyM, data.SharedSecret)

	return data, session, nil
}

// Process a session handshake request data from remoteNode r
// Returns Handshake data to send to r and a network session data object that includes the session sym  enc/dec key
// This is called by responder in the handshake protocol (node2)
func ProcessHandshakeRequest(node LocalNode, r RemoteNode, req *pb.HandshakeData) (*pb.HandshakeData, NetworkSession, error) {

	// ephemeral public key
	pubkey, err := btcec.ParsePubKey(req.SharedSecret, btcec.S256())
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

	// set session data
	s := NewNetworkSession(req.Iv, keyE, keyM, req.SharedSecret)
	s.SetAuthenticated(true)

	// start generation of resp data

	iv := make([]byte, 16)
	if _, err := io.ReadFull(rand.Reader, iv); err != nil {
		return nil, nil, err
	}

	// auth resp
	hm1 := hmac.New(sha256.New, keyM)
	hm1.Write(iv)
	hmac1 := hm1.Sum(nil)

	resp := &pb.HandshakeData{
		NodePubKey: node.PublicKey().InternalKey().SerializeUncompressed(),
		SharedSecret:  req.SharedSecret,
		Iv:         iv,
		Hmac:       hmac1,
		Sign:       "",
	}

	// sign corpus - marshall data without the signature to protobufs3 binary format
	bin, err = proto.Marshal(resp)
	if err != nil {
		return nil, nil, err
	}

	sign, err := node.PrivateKey().Sign(bin)
	if err != nil {
		return nil, nil, err
	}

	// place signature in response - hex encoded string
	resp.Sign = hex.EncodeToString(sign)

	return resp, s, nil
}

// Process handshake protocol response. This is called by initiator (node1) to handle response from node2
// and to establish the session
// Side-effect - passed network session is set to authenticated
func ProcessHandshakeResponse(node LocalNode, r RemoteNode, s NetworkSession, resp *pb.HandshakeData) error {

	// verified shared public secret
	if !bytes.Equal(resp.SharedSecret, s.SharedSecret()) {
		return errors.New("Shared secret mismatch")
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

	return nil
}

