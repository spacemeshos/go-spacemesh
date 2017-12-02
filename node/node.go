package node

import (
	"bufio"
	"github.com/UnrulyOS/go-unruly/log"
	"time"

	"github.com/gogo/protobuf/proto"

	protobufCodec "github.com/multiformats/go-multicodec/protobuf"
	host "gx/ipfs/QmRS46AyqtpJBsf1zmQdeizSDEzo1qkWR7rdEuPFAv8237/go-libp2p-host"
	peer "gx/ipfs/QmXYjuNuxVzXKJCfWasQk1RqkhVLDM9jtUKhqc2WPQmFSB/go-libp2p-peer"
	crypto "gx/ipfs/QmaPbCnUMBohSGo3KnxEa2bHqyJVVeEEcwtqJAYxerieBo/go-libp2p-crypto"
	inet "gx/ipfs/QmbD5yKbXahNvoMqzeuNyKQA9vAs9fUvJg2GXeWU1fVqY5/go-libp2p-net"

	"github.com/UnrulyOS/go-unruly/node/pb"
)

// node client version
const clientVersion = "go-p2p-node/0.0.1"

// Node type - a p2p host implementing one or more p2p protocols
type Node struct {
	host.Host     // lib-p2p host
	*PingProtocol // ping protocol impl
	*EchoProtocol // echo protocol impl
	// add other protocols here...
}

// Create a new node with its implemented protocols
func NewNode(host host.Host, done chan bool) *Node {
	n := &Node{Host: host}
	n.PingProtocol = NewPingProtocol(n, done)
	n.EchoProtocol = NewEchoProtocol(n, done)
	return n
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
		log.Error( "Failed to decode node id from base58. %s", err)
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
	key := n.Peerstore().PrivKey(n.ID())

	// this is implemented by signing a sha256 of the data
	res, err := key.Sign(data)
	return res, err
}

// Verify incoming p2p message data integrity
// data: data to verify
// signature: author signature provided in the message payload
// peerId: author peer id from the message payload
// pubKeyData: author public key from the message payload (protobufs encoded)
func (n *Node) verifyData(data []byte, signature []byte, peerId peer.ID, pubKeyData []byte) bool {

	key, err := crypto.UnmarshalPublicKey(pubKeyData)
	if err != nil {

		log.Error("Failed to extract key from message key data", err)
		return false
	}

	// verify that message author node id matches the provided node public key
	if !peerId.MatchesPublicKey(key) {
		log.Info("Node id and provided public key mismatch. %s", err)
		return false
	}

	// this is implemented by veryfing signature of a sha256 of the data
	res, err := key.Verify(data, signature)
	if err != nil {
		log.Info("Error authenticating data. %s", err)
		return false
	}

	return res
}

// helper method - generate message data shared between all node's p2p protocols
// messageId: unique for requests, copied from request for responses
func (n *Node) NewMessageData(messageId string, gossip bool) *pb.MessageData {
	// Add protobufs bin data for message author public key
	// this is useful for authenticating  messages forwarded by a node authored by another node
	nodePubKey, err := n.Peerstore().PubKey(n.ID()).Bytes()

	if err != nil {
		panic("Failed to get public key for sender from local peer store.")
	}

	return &pb.MessageData{ClientVersion: clientVersion,
		NodeId:     peer.IDB58Encode(n.ID()),
		NodePubKey: nodePubKey,
		Timestamp:  time.Now().Unix(),
		Id:         messageId,
		Gossip:     gossip}
}

// helper method - writes a protobuf go data object to a network stream
// data: reference of protobuf go data object to send (not the object itself)
// s: network stream to write the data to
func (n *Node) SendProtoMessage(data proto.Message, s inet.Stream) bool {
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
