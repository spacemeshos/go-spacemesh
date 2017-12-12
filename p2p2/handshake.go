package p2p2

import (
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
// Node that NetworkSession is not yet authneticated - this happens only when the handshake response is processed and authenticated
func GenereateHandshakeRequestData(node LocalNode, remoteNode RemoteNode) (*pb.HandshakeData, NetworkSession, error) {

	// we use the Elliptic Curve Encryption Scheme
	// https://en.wikipedia.org/wiki/Integrated_Encryption_Scheme

	data := &pb.HandshakeData{}

	iv := make([]byte,16)
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
	keyE := derivedKey[:32]	// used for aes enc/dec
	keyM := derivedKey[32:] // used for hmac

	data.SharedKey = ephemeral.PubKey().SerializeUncompressed()

	// start HMAC-SHA-256
	hm := hmac.New(sha256.New, keyM)
	hm.Write(iv)      	    			// iv is hashed
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
	session := NewNetworkSession(iv, keyE)

	return data, session, nil
}

// Process a session handshake request data from remoteNode r
// Returns Handshake data to send to r and a network session data object that includes the session sym  enc/dec key

func ProcessHandshakeRequest(node LocalNode, r RemoteNode, req *pb.HandshakeData) (*pb.HandshakeData, NetworkSession, error) {

	// ephemeral public key
	pubkey, err := btcec.ParsePubKey(req.SharedKey, btcec.S256())
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
	l := len(req.Sign)
	if l > 32 {
		return nil, nil, errors.New("invalid sig")
	}

	sig, err := hex.DecodeString(req.Sign)
	if err != nil {
		return nil, nil, err
	}

	req.Sign = ""
	bin, err := proto.Marshal(req)
	if err != nil {
		return nil, nil, err
	}

	v, err := node.PublicKey().Verify(bin, sig)
	if err != nil {
		return nil, nil, err
	}

	if !v {
		return nil, nil, errors.New("invalid signature")
	}

	// generate resp data
	iv := make([]byte,16)
	if _, err := io.ReadFull(rand.Reader, iv); err != nil {
		return nil, nil, err
	}

	// auth resp
	hm.Reset()
	hm.Write(iv)      	    			// iv is hashed
	hmac1 := hm.Sum(nil)

	s := NewNetworkSession(req.Iv, keyE)
	s.SetAuthenticated(true)

	resp := &pb.HandshakeData{
		 NodePubKey:node.PublicKey().InternalKey().SerializeUncompressed(),
		 SharedKey: req.SharedKey,
		 Iv: iv,
		 Hmac: hmac1,
		 Sign: "",
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

