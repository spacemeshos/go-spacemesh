package ping

import (
	"errors"
	"github.com/gogo/protobuf/proto"
	"github.com/spacemeshos/go-spacemesh/crypto"
	"github.com/spacemeshos/go-spacemesh/log"
	"github.com/spacemeshos/go-spacemesh/p2p"
	"github.com/spacemeshos/go-spacemesh/p2p/p2pcrypto"
	"github.com/spacemeshos/go-spacemesh/p2p/service"
	"github.com/spacemeshos/go-spacemesh/ping/pb"
	"sync"
	"time"
)

// Pinger is an identity that does ping.
type Pinger interface {
	PublicKey() p2pcrypto.PublicKey
}

const protocol = "/ping/1.0/"

// PingTimeout is a timeout for ping reply
const PingTimeout = time.Second * 10 // TODO: Parametrize

var responses = map[string]string{"hello": "world"}
var responseMutex sync.RWMutex

// AddResponse adds response according to originating request
func AddResponse(req, res string) {
	responseMutex.Lock()
	responses[req] = res
	responseMutex.Unlock()
}

var errPingTimedOut = errors.New("Ping took too long to response")

// Ping manages ping requests and responses
type Ping struct {
	p2p p2p.Service

	pending    map[crypto.UUID]chan *pb.Ping
	pendMuxtex sync.RWMutex

	ingressChannel chan service.Message
}

// New creates new ping instance, receives p2p as network infra
func New(p2p p2p.Service) *Ping {
	p := &Ping{pending: make(map[crypto.UUID]chan *pb.Ping)}
	p.p2p = p2p
	p.ingressChannel = p2p.RegisterProtocol(protocol)
	go p.readLoop()
	return p
}

func (p *Ping) readLoop() {
	for {
		msg, ok := <-p.ingressChannel
		if !ok {
			// Channel is closed.
			break
		}

		go func(msg service.Message) {
			ping := &pb.Ping{}
			err := proto.Unmarshal(msg.Bytes(), ping)
			if err != nil {
				log.Error("failed to read incoming ping message err:", err)
				// TODO : handle errors in readloop
			}

			if ping.Req {
				log.Info("Ping: Request from (%v) - Message : %v", msg.Sender().Pretty(), ping.Message)
				err := p.handleRequest(msg.Sender(), ping)
				if err != nil {
					log.Error("Error handling ping request", err)
				}
				return
			}

			p.handleResponse(ping)

		}(msg)
	}
}

// Ping sends actual pings to target
func (p *Ping) Ping(target p2pcrypto.PublicKey, msg string) (string, error) {
	var response string
	reqid := crypto.NewUUID()
	ping := &pb.Ping{
		ReqID:   reqid[:],
		Req:     true,
		Message: msg,
	}
	pchan, err := p.sendRequest(target, reqid, ping)
	if err != nil {
		return response, err
	}

	timer := time.NewTimer(PingTimeout)
	select {
	case res := <-pchan:
		response = res.Message
		p.pendMuxtex.Lock()
		delete(p.pending, reqid)
		p.pendMuxtex.Unlock()
	case <-timer.C:
		return response, errPingTimedOut
	}

	return response, nil
}

func (p *Ping) sendRequest(target p2pcrypto.PublicKey, reqid crypto.UUID, ping *pb.Ping) (chan *pb.Ping, error) {
	pchan := make(chan *pb.Ping)
	p.pendMuxtex.Lock()
	p.pending[reqid] = pchan
	p.pendMuxtex.Unlock()

	remove := func() {
		p.pendMuxtex.Lock()
		delete(p.pending, reqid)
		p.pendMuxtex.Unlock()
	}

	payload, err := proto.Marshal(ping)
	if err != nil {
		remove()
		return nil, err
	}

	err = p.p2p.SendMessage(target, protocol, payload)
	if err != nil {
		remove()
		return nil, err
	}

	return pchan, nil
}

func (p *Ping) handleRequest(sender crypto.PublicKey, ping *pb.Ping) error {
	responseMutex.RLock()
	resp, ok := responses[ping.Message]
	responseMutex.RUnlock()

	if !ok {
		resp = ping.Message
	}

	pingResp := &pb.Ping{
		ReqID:   ping.ReqID,
		Req:     false,
		Message: resp,
	}

	bin, err := proto.Marshal(pingResp)
	if err != nil {
		return err
	}
	log.Debug("Ping: Responding with %v", resp)
	return p.p2p.SendMessage(sender, protocol, bin)
}

func (p *Ping) handleResponse(ping *pb.Ping) {
	reqid := ping.ReqID
	var creqid crypto.UUID
	copy(creqid[:], reqid)
	p.pendMuxtex.RLock()
	c, ok := p.pending[creqid]
	p.pendMuxtex.RUnlock()
	if ok {
		c <- ping
		p.pendMuxtex.Lock()
		delete(p.pending, creqid)
		p.pendMuxtex.Unlock()
	}
}
