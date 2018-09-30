package net

import (
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"crypto/sha512"
	"encoding/hex"
	"io"
	"time"

	"bytes"
	"errors"
	"fmt"
	"github.com/btcsuite/btcd/btcec"
	"github.com/gogo/protobuf/proto"
	"github.com/spacemeshos/go-spacemesh/crypto"
	"github.com/spacemeshos/go-spacemesh/p2p/config"
	"github.com/spacemeshos/go-spacemesh/p2p/pb"
	"github.com/spacemeshos/go-spacemesh/p2p/version"
	"strings"
)

// HandshakeReq specifies the handshake protocol request message identifier. pattern is [protocol][version][method-name].
const HandshakeReq = "/handshake/1.0/handshake-req/"

// HandshakeResp specifies the handshake protocol response message identifier.
const HandshakeResp = "/handshake/1.0/handshake-resp/"

// GenerateHandshakeRequestData and session data between node and remoteNode
// Returns handshake data to send to removeNode and a network session data object that includes the session enc/dec sym key and iv
// Node that NetworkSession is not yet authenticated - this happens only when the handshake response is processed and authenticated
// This is called by node1 (initiator)
func GenerateHandshakeRequestData(localPublicKey crypto.PublicKey, localPrivateKey crypto.PrivateKey, remotePublicKey crypto.PublicKey,
	networkID int8) (*pb.HandshakeData, NetworkSession, error) {

	// we use the Elliptic Curve Encryption Scheme
	// https://en.wikipedia.org/wiki/Integrated_Encryption_Scheme

	data := &pb.HandshakeData{
		Protocol: HandshakeReq,
	}

	data.ClientVersion = config.ClientVersion
	data.NetworkID = int32(networkID)
	data.Timestamp = time.Now().Unix()

	iv := make([]byte, 16)
	if _, err := io.ReadFull(rand.Reader, iv); err != nil {
		return nil, nil, err
	}

	data.SessionId = iv
	data.Iv = iv

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

// ProcessHandshakeRequest Process a session handshake request data from remoteNode r
// Returns Handshake data to send to r and a network session data object that includes the session sym  enc/dec key
// This is called by responder in the handshake protocol (node2)
func ProcessHandshakeRequest(networkID int8, lPub crypto.PublicKey, lPri crypto.PrivateKey, rPub crypto.PublicKey, req *pb.HandshakeData) (*pb.HandshakeData, NetworkSession, error) {
	// check that received clientversion is valid client string
	reqVersion := strings.Split(req.ClientVersion, "/")
	if len(reqVersion) != 2 {
		//node.Warning("Dropping incoming message - invalid client version")
		return nil, nil, errors.New("invalid client version")
	}

	// compare that version to the min client version in config
	ok, err := version.CheckNodeVersion(reqVersion[1], config.MinClientVersion)
	if err == nil && !ok {
		return nil, nil, errors.New("unsupported client version")
	}
	if err != nil {
		return nil, nil, fmt.Errorf("invalid client version, err: %v", err)
	}

	// make sure we're on the same network
	if req.NetworkID != int32(networkID) {
		return nil, nil, fmt.Errorf("request net id (%d) is different than local net id (%d)", req.NetworkID, networkID)
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
	req.Sign = sig
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

	// generate ack resp data
	iv := make([]byte, 16)
	if _, err := io.ReadFull(rand.Reader, iv); err != nil {
		return nil, nil, err
	}

	hm1 := hmac.New(sha256.New, keyM)
	hm1.Write(iv)
	hmac1 := hm1.Sum(nil)

	resp := &pb.HandshakeData{
		ClientVersion: config.ClientVersion,
		NetworkID:     int32(networkID),
		SessionId:     req.SessionId,
		Timestamp:     time.Now().Unix(),
		NodePubKey:    lPub.InternalKey().SerializeUncompressed(),
		PubKey:        req.PubKey,
		Iv:            iv,
		Hmac:          hmac1,
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

// ProcessHandshakeResponse is called by initiator (node1) to handle response from node2
// and to establish the session
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
	resp.Sign = sig
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
