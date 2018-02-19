package p2p

import (
	"bytes"
	"encoding/hex"
	"errors"
	"fmt"
	"github.com/gogo/protobuf/proto"
	"github.com/spacemeshos/go-spacemesh/assert"
	"github.com/spacemeshos/go-spacemesh/crypto"
	"github.com/spacemeshos/go-spacemesh/log"
	"github.com/spacemeshos/go-spacemesh/p2p/nodeconfig"
	"github.com/spacemeshos/go-spacemesh/p2p/pb"
	"testing"
)

// Basic handshake protocol data test
func TestHandshakeCoreData(t *testing.T) {

	// node 1
	port := crypto.GetRandomUserPort()
	address := fmt.Sprintf("0.0.0.0:%d", port)

	node1Local, err := NewNodeIdentity(address, nodeconfig.ConfigValues, false)

	if err != nil {
		t.Error("failed to create local node1", err)
	}

	// this will be node 2 view of node 1
	node1Remote, _ := NewRemoteNode(node1Local.String(), address)

	// node 2
	port1 := crypto.GetRandomUserPort()
	address1 := fmt.Sprintf("0.0.0.0:%d", port1)

	node2Local, err := NewNodeIdentity(address1, nodeconfig.ConfigValues, false)

	if err != nil {
		t.Error("failed to create local node2", err)
	}

	// this will be node1 view of node 2
	node2Remote, _ := NewRemoteNode(node2Local.String(), address1)

	// STEP 1: Node1 generates handshake data and sends it to node2 ....
	data, session, err := generateHandshakeRequestData(node1Local, node2Remote)

	assert.NoErr(t, err, "expected no error")
	assert.NotNil(t, session, "expected session")
	assert.NotNil(t, data, "expected session")

	log.Info("Node 1 session data: Id:%s, AES-KEY:%s", hex.EncodeToString(session.ID()), hex.EncodeToString(session.KeyE()))

	assert.False(t, session.IsAuthenticated(), "Expected session to be not authenticated yet")

	// STEP 2: Node2 gets handshake data from node 1 and processes it to establish a session with a shared AES key

	resp, session1, err := processHandshakeRequest(node2Local, node1Remote, data)

	assert.NoErr(t, err, "expected no error")
	assert.NotNil(t, session1, "expected session")
	assert.NotNil(t, resp, "expected resp data")

	log.Info("Node 2 session data: Id:%s, AES-KEY:%s", hex.EncodeToString(session1.ID()), hex.EncodeToString(session1.KeyE()))

	assert.Equal(t, string(session.ID()), string(session1.ID()), "expected agreed Id")
	assert.Equal(t, string(session.KeyE()), string(session1.KeyE()), "expected same shared AES enc key")
	assert.Equal(t, string(session.KeyM()), string(session1.KeyM()), "expected same shared AES mac key")
	assert.Equal(t, string(session.PubKey()), string(session1.PubKey()), "expected same shared secret")

	assert.True(t, session1.IsAuthenticated(), "expected session1 to be authenticated")

	// STEP 3: Node2 sends data1 back to node1.... Node 1 validates the data and sets its network session to authenticated
	err = processHandshakeResponse(node1Local, node2Remote, session, resp)

	assert.True(t, session.IsAuthenticated(), "expected session to be authenticated")
	assert.NoErr(t, err, "failed to authenticate or process response")

	// test session sym enc / dec

	const msg = "hello spacemesh - hello spacemesh - hello spacemesh :-)"
	cipherText, err := session.Encrypt([]byte(msg))
	assert.NoErr(t, err, "expected no error")

	clearText, err := session1.Decrypt(cipherText)
	assert.NoErr(t, err, "expected no error")
	assert.True(t, bytes.Equal(clearText, []byte(msg)), "Expected enc/dec to work")

	node1Local.Shutdown()
	node2Local.Shutdown()
}

func TestHandshakeProtocol(t *testing.T) {

	// node 1
	port := crypto.GetRandomUserPort()
	address := fmt.Sprintf("0.0.0.0:%d", port)

	node1Local, _ := NewNodeIdentity(address, nodeconfig.ConfigValues, false)
	node1Remote, _ := NewRemoteNode(node1Local.String(), address)

	// node 2
	port1 := crypto.GetRandomUserPort()
	address1 := fmt.Sprintf("0.0.0.0:%d", port1)

	node2Local, _ := NewNodeIdentity(address1, nodeconfig.ConfigValues, false)
	node2Remote, _ := NewRemoteNode(node2Local.String(), address1)

	// STEP 1: Node 1 generates handshake data and sends it to node2 ....

	data, session, err := generateHandshakeRequestData(node1Local, node2Remote)

	assert.NoErr(t, err, "expected no error")
	assert.NotNil(t, session, "expected session")
	assert.NotNil(t, data, "expected session")

	log.Info("Node 1 session data: Id:%s, AES-KEY:%s", hex.EncodeToString(session.ID()), hex.EncodeToString(session.KeyE()))

	assert.False(t, session.IsAuthenticated(), "expected session to be not authenticated yet")

	// encode message data
	wireFormat, err := proto.Marshal(data)

	// STEP 2: node 2 figures out if incoming message is a handshake request and handle it

	m := &pb.CommonMessageData{}
	err = proto.Unmarshal(wireFormat, m)
	assert.NoErr(t, err, "failed to unmarshal wire formatted data")

	assert.Equal(t, len(m.Payload), 0, "expected an empty payload for handshake request")

	// since payload is nil this is a handshake req or response and we can deserialize it

	data1 := &pb.HandshakeData{}
	err = proto.Unmarshal(wireFormat, data1)

	log.Info("Handshake request from %s", data1.TcpAddress)

	assert.NoErr(t, err, "failed to unmarshal wire formatted data to handshake data")
	assert.Equal(t, HandshakeReq, data1.Protocol, "expected this message to be a handshake req")

	// STEP 3: local node 2 handles req data and generates response

	resp, session1, err := processHandshakeRequest(node2Local, node1Remote, data1)

	assert.NoErr(t, err, "expected no error")
	assert.NotNil(t, session1, "expected session")
	assert.NotNil(t, resp, "expected resp data")

	log.Info("Node 2 session data: Id:%s, AES-KEY:%s", hex.EncodeToString(session1.ID()), hex.EncodeToString(session1.KeyE()))

	assert.Equal(t, string(session.ID()), string(session1.ID()), "expected agreed Id")
	assert.Equal(t, string(session.KeyE()), string(session1.KeyE()), "expected same shared AES enc key")
	assert.Equal(t, string(session.KeyM()), string(session1.KeyM()), "expected same shared AES mac key")
	assert.Equal(t, string(session.PubKey()), string(session1.PubKey()), "expected same shared secret")

	assert.True(t, session1.IsAuthenticated(), "expected session1 to be authenticated")

	// encode message data to wire format
	wireFormat, err = proto.Marshal(resp)

	// wireFormat data sent from node 2 to node 1....

	// STEP 4: node 1 - figure out if incoming message is a handshake response

	m = &pb.CommonMessageData{}
	err = proto.Unmarshal(wireFormat, m)
	assert.NoErr(t, err, "failed to unmarshal wire formatted data")

	assert.Equal(t, len(m.Payload), 0, "expected an empty payload for handshake response")

	// since payload is nil this is a handshake req or response and we can deserialize it

	data2 := &pb.HandshakeData{}
	err = proto.Unmarshal(wireFormat, data2)
	assert.NoErr(t, err, "failed to unmarshal wire formatted data to handshake data")
	assert.Equal(t, HandshakeResp, data2.Protocol, "expected this message to be a handshake req")

	// STEP 5: Node 1 validates the data and sets its network session to authenticated

	log.Info("Handshake response from from %s", data2.TcpAddress)

	err = processHandshakeResponse(node1Local, node2Remote, session, data2)

	assert.True(t, session.IsAuthenticated(), "expected session to be authenticated")
	assert.NoErr(t, err, "failed to authenticate or process response")

	node1Local.Shutdown()
	node2Local.Shutdown()
}

func TestBadHandshakes(t *testing.T) {

	testProtocol := func(cfg nodeconfig.Config, cfg2 nodeconfig.Config) error {
		// node 1
		port := crypto.GetRandomUserPort()
		address := fmt.Sprintf("0.0.0.0:%d", port)

		node1Local, _ := NewNodeIdentity(address, cfg, false)
		node1Remote, _ := NewRemoteNode(node1Local.String(), address)

		// node 2
		port1 := crypto.GetRandomUserPort()
		address1 := fmt.Sprintf("0.0.0.0:%d", port1)

		node2Local, _ := NewNodeIdentity(address1, cfg2, false)
		node2Remote, _ := NewRemoteNode(node2Local.String(), address1)

		// STEP 1: Node 1 generates handshake data and sends it to node2 ....

		data, session, err := generateHandshakeRequestData(node1Local, node2Remote)

		if err != nil {
			return fmt.Errorf("expected no error, %v", err)
		}
		if session == nil || data == nil {
			return errors.New("excpected session to not be nil")
		}

		log.Info("Node 1 session data: Id:%s, AES-KEY:%s", hex.EncodeToString(session.ID()), hex.EncodeToString(session.KeyE()))

		if session.IsAuthenticated() {
			return errors.New("expected session to be not authenticated yet")
		}

		// encode message data
		wireFormat, err := proto.Marshal(data)

		// STEP 2: node 2 figures out if incoming message is a handshake request and handle it

		m := &pb.CommonMessageData{}
		err = proto.Unmarshal(wireFormat, m)
		if err != nil {
			return errors.New("failed to unmarshal wire formatted data")
		}
		if len(m.Payload) != 0 {
			return errors.New("expected an empty payload for handshake request")
		}

		// since payload is nil this is a handshake req or response and we can deserialize it

		data1 := &pb.HandshakeData{}
		err = proto.Unmarshal(wireFormat, data1)

		log.Info("Handshake request from %s", data1.TcpAddress)

		if err != nil {
			return fmt.Errorf("failed to unmarshal wire formatted data to handshake data: %v", err)
		}

		if HandshakeReq != data1.Protocol {
			return errors.New("expected this message to be a handshake req")
		}

		// STEP 3: local node 2 handles req data and generates response

		resp, session1, err := processHandshakeRequest(node2Local, node1Remote, data1)

		if err != nil {
			return fmt.Errorf("error processing handshake %v", err)
		}

		if session1 == nil || resp == nil {
			return errors.New("session is nil")
		}

		log.Info("Node 2 session data: Id:%s, AES-KEY:%s", hex.EncodeToString(session1.ID()), hex.EncodeToString(session1.KeyE()))

		compareSlice1 := map[string][]byte{"session-id": session.ID(), "keyE": session.KeyE(), "keyM": session.KeyM(), "pubKey": session.PubKey()}
		compareSlice2 := map[string][]byte{"session-id": session1.ID(), "keyE": session1.KeyE(), "keyM": session1.KeyM(), "pubKey": session1.PubKey()}

		for i := range compareSlice1 {
			if string(compareSlice1[i]) != string(compareSlice2[i]) {
				return fmt.Errorf("excpected shared session data %v", i)
			}
		}

		if !session1.IsAuthenticated() {
			return errors.New("expected session1 to be authenticated")
		}

		// encode message data to wire format
		wireFormat, err = proto.Marshal(resp)

		// wireFormat data sent from node 2 to node 1....

		// STEP 4: node 1 - figure out if incoming message is a handshake response

		m = &pb.CommonMessageData{}
		err = proto.Unmarshal(wireFormat, m)

		if err != nil {
			return fmt.Errorf("failed to unmarshal wire formattted data %v", err)
		}

		if len(m.Payload) != 0 {
			return errors.New("expected an empty payload for handshake response")
		}

		// since payload is nil this is a handshake req or response and we can deserialize it

		data2 := &pb.HandshakeData{}
		err = proto.Unmarshal(wireFormat, data2)

		if err != nil {
			return fmt.Errorf("failed to unmarshal wire formattted data to handshake data %v", err)
		}

		if HandshakeResp != data2.Protocol {
			return errors.New("expected this message to be a handshake resp")
		}

		// STEP 5: Node 1 validates the data and sets its network session to authenticated

		log.Info("Handshake response from from %s", data2.TcpAddress)

		err = processHandshakeResponse(node1Local, node2Remote, session, data2)

		if err != nil {
			return fmt.Errorf("failed to authenticate or process response %v", err)
		}

		if !session.IsAuthenticated() {
			return errors.New("expected session to be authenticated")
		}

		node1Local.Shutdown()
		node2Local.Shutdown()
		return nil
	}

	defaultConfig := nodeconfig.ConfigValues
	if err := testProtocol(defaultConfig, defaultConfig); err != nil {
		t.Errorf("Default test failed %v", err)
	}
	networkIDCfg := nodeconfig.ConfigValues
	networkIDCfg.NetworkID = nodeconfig.ConfigValues.NetworkID + 1
	if err := testProtocol(defaultConfig, networkIDCfg); err == nil {
		t.Error("Excpected diffrent network id to not work")
	}

}
