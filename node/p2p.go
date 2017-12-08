package node

import (
	"bufio"
	"github.com/UnrulyOS/go-unruly/log"
	"github.com/UnrulyOS/go-unruly/node/config"
	"time"

	"github.com/gogo/protobuf/proto"

	protobufCodec "github.com/multiformats/go-multicodec/protobuf"
	peer "gx/ipfs/QmXYjuNuxVzXKJCfWasQk1RqkhVLDM9jtUKhqc2WPQmFSB/go-libp2p-peer"
	inet "gx/ipfs/QmbD5yKbXahNvoMqzeuNyKQA9vAs9fUvJg2GXeWU1fVqY5/go-libp2p-net"

	libp2pcrypto "gx/ipfs/QmaPbCnUMBohSGo3KnxEa2bHqyJVVeEEcwtqJAYxerieBo/go-libp2p-crypto"

	"github.com/UnrulyOS/go-unruly/node/pb"
)

// Node p2p methods

func (n *Node) loadBootstrapNodes() {

	data := RemoteNodesFromMurls(config.BootstrapNodes)
	for _, node := range data {
		log.Info("Loaded bootstrap node: %s, %s", node.Id, node.Addresses[0].String())
	}
}

// Authenticate incoming p2p message
// message: a protobufs go data object
// data: common p2p message data
func (n *Node) AuthenticateMessage(message proto.Message, data *pb.MessageData) bool {

	// store a temp ref to signature and remove it from message data
	// sign is a string to allow easy reset to zero-value (empty string)
	sign := data.Sign
	data.Sign = ""

	// marshall data without the signature to protobufs3 binary format
	bin, err := proto.Marshal(message)

	if err != nil {
		log.Error("failed to marshal pb message. %s", err)
		return false
	}

	// restore sig in message data (for possible future use)
	data.Sign = sign

	// restore peer id binary format from base58 encoded node id data
	peerId, err := peer.IDB58Decode(data.NodeId)
	if err != nil {
		log.Error("failed to decode node id from base58. %s", err)
		return false
	}

	// verify the data was authored by the signing peer identified by the public key
	// and signature included in the message
	return n.verifyData(bin, []byte(sign), peerId, data.NodePubKey)
}

// sign an outgoing p2p message payload
func (n *Node) SignProtoMessage(message proto.Message) ([]byte, error) {
	data, err := proto.Marshal(message)
	if err != nil {
		return nil, err
	}
	return n.signData(data)
}

// sign binary data using the local node's private key
func (n *Node) signData(data []byte) ([]byte, error) {
	res, err := n.privateKey.PrivatePeerKey().Sign(data)
	return res, err
}

// Verify incoming p2p message data integrity
// data: data to verify
// signature: author signature provided in the message payload
// peerId: author peer id from the message payload
// pubKeyData: author public key from the message payload (protobufs encoded)
func (n *Node) verifyData(data []byte, signature []byte, peerId peer.ID, pubKeyData []byte) bool {

	key, err := libp2pcrypto.UnmarshalPublicKey(pubKeyData)
	if err != nil {

		log.Error("failed to extract key from message key data", err)
		return false
	}

	// verify that message author node id matches the provided node public key
	if !peerId.MatchesPublicKey(key) {
		log.Info("node id and provided public key mismatch. %s", err)
		return false
	}

	// this is implemented by veryfing signature of a sha256 of the data
	res, err := key.Verify(data, signature)
	if err != nil {
		log.Info("error authenticating data. %s", err)
		return false
	}

	return res
}

// helper method - generate message data shared between all node's p2p protocols
// messageId: unique for requests, copied from request for responses
func (n *Node) NewMessageData(messageId string, gossip bool) *pb.MessageData {

	nodePubKey, err := n.publicKey.Bytes()

	if err != nil {
		panic("failed to get public key for sender from local peer store.")
	}

	return &pb.MessageData{ClientVersion: config.ClientVersion,
		NodeId:     peer.IDB58Encode(n.ID()),
		NodePubKey: nodePubKey,
		Timestamp:  time.Now().Unix(),
		Id:         messageId,
		Gossip:     gossip}
}

// helper method - writes a protobuf go data object to a network stream
// data: reference of protobuf go data object to send (not the object itself)
// s: network stream to write the data to
func (n *Node) sendProtoMessage(data proto.Message, s inet.Stream) bool {

	writer := bufio.NewWriter(s)
	enc := protobufCodec.Multicodec(nil).Encoder(writer)
	err := enc.Encode(data)
	if err != nil {
		log.Error("Failed to send proto message. %s", err)
		return false
	}
	writer.Flush()
	return true
}
