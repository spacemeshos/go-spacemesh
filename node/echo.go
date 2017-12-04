package node

import (
	"bufio"
	"context"
	"fmt"
	"github.com/UnrulyOS/go-unruly/assert"
	"github.com/UnrulyOS/go-unruly/log"
	"github.com/UnrulyOS/go-unruly/node/pb"

	uuid "github.com/google/uuid"
	inet "gx/ipfs/QmbD5yKbXahNvoMqzeuNyKQA9vAs9fUvJg2GXeWU1fVqY5/go-libp2p-net"

	protobufCodec "github.com/multiformats/go-multicodec/protobuf"
	"gx/ipfs/QmRS46AyqtpJBsf1zmQdeizSDEzo1qkWR7rdEuPFAv8237/go-libp2p-host"
)

// pattern: /protocol-name/request-or-response-message/version
const echoRequest = "/echo/echoreq/0.0.1"
const echoResponse = "/echo/echoresp/0.0.1"

type EchoProtocol struct {
	node     *Node                      // local host
	requests map[string]*pb.EchoRequest // used to access request data from response handlers
	done     chan bool                  // only for demo purposes to hold main from terminating
}

func NewEchoProtocol(node *Node, done chan bool) *EchoProtocol {
	e := EchoProtocol{node: node, requests: make(map[string]*pb.EchoRequest), done: done}
	node.SetStreamHandler(echoRequest, e.onEchoRequest)
	node.SetStreamHandler(echoResponse, e.onEchoResponse)

	// design note: to implement fire-and-forget style messages you may just skip specifying a response callback.
	// a fire-and-forget message will just include a request and not specify a response object
	return &e
}

// remote peer requests handler
func (e EchoProtocol) onEchoRequest(s inet.Stream) {
	// get request data
	data := &pb.EchoRequest{}
	decoder := protobufCodec.Multicodec(nil).Decoder(bufio.NewReader(s))
	err := decoder.Decode(data)
	if err != nil {
		log.Error("failed to send request. %s", err)
		return
	}

	log.Info("%s: received echo request from %s. Message: %s", s.Conn().LocalPeer(), s.Conn().RemotePeer(), data.Message)
	valid := e.node.AuthenticateMessage(data, data.MessageData)
	if !valid {
		log.Error("failed to authenticate message")
		return
	}

	log.Info("%s: sending echo response to %s. Message id: %s...", s.Conn().LocalPeer(), s.Conn().RemotePeer(), data.MessageData.Id)

	// send response to the request using the message string he provided
	resp := &pb.EchoResponse{
		MessageData: e.node.NewMessageData(data.MessageData.Id, false),
		Message:     data.Message}

	// sign the data
	signature, err := e.node.SignProtoMessage(resp)
	if err != nil {
		log.Error("failed to sign response. %err", err)
		return
	}

	// add the signature to the message
	resp.MessageData.Sign = string(signature)
	s, respErr := e.node.NewStream(context.Background(), s.Conn().RemotePeer(), echoResponse)
	if respErr != nil {
		log.Error("failed to create strem to node. %s", respErr)
		return
	}

	ok := e.node.SendProtoMessage(resp, s)
	if ok {
		log.Info("%s: echo response to %s sent.", s.Conn().LocalPeer().String(), s.Conn().RemotePeer().String())
	}
}

// remote echo response handler
func (e EchoProtocol) onEchoResponse(s inet.Stream) {
	data := &pb.EchoResponse{}
	decoder := protobufCodec.Multicodec(nil).Decoder(bufio.NewReader(s))
	err := decoder.Decode(data)
	if err != nil {
		return
	}

	// authenticate message content
	valid := e.node.AuthenticateMessage(data, data.MessageData)

	if !valid {
		log.Error("failed to authenticate message")
		return
	}

	// locate request data and remove it if found
	req, ok := e.requests[data.MessageData.Id]
	if ok {
		// remove request from map as we have processed it here
		delete(e.requests, data.MessageData.Id)
	} else {
		log.Error("failed to locate request data boject for response")
		return
	}

	assert.True(req.Message == data.Message, nil, "Expected echo to respond with request message")
	log.Info("%s: received echo response from %s. Message id:%s. Message: %s.", s.Conn().LocalPeer(), s.Conn().RemotePeer(), data.MessageData.Id, data.Message)
	e.done <- true
}

func (e EchoProtocol) Echo(host host.Host) bool {
	log.Info("%s: sending echo to: %s....", e.node.ID(), host.ID())

	// create message data
	req := &pb.EchoRequest{
		MessageData: e.node.NewMessageData(uuid.New().String(), false),
		Message:     fmt.Sprintf("Echo from %s", e.node.ID())}

	signature, err := e.node.SignProtoMessage(req)
	if err != nil {
		log.Error("failed to sign message. %s", err)
		return false
	}

	// add the signature to the message
	req.MessageData.Sign = string(signature)

	s, err := e.node.NewStream(context.Background(), host.ID(), echoRequest)
	if err != nil {
		log.Error("failed to create stream. %s", err)
		return false
	}

	ok := e.node.SendProtoMessage(req, s)
	if !ok {
		return false
	}

	// store request so response handler has access to it
	e.requests[req.MessageData.Id] = req
	log.Info("%s: echo to: %s was sent. Message Id: %s, Message: %s", e.node.ID(), host.ID(), req.MessageData.Id, req.Message)
	return true
}
