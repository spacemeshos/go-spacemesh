package p2p

import (
	"bytes"
	"github.com/UnrulyOS/go-unruly/log"
	"github.com/UnrulyOS/go-unruly/p2p/pb"
	"github.com/google/uuid"
	"testing"
	"time"
)

func TestPingProtocol(t *testing.T) {

	node1Local, _ := GenerateTestNode(t)
	_, node2Remote := GenerateTestNode(t)

	// let node 1 know about node 2
	node1Local.GetSwarm().RegisterNode(RemoteNodeData{node2Remote.String(), node2Remote.TcpAddress()})

	// 4 lines of code and a callback on a channel !
	pingReqId := []byte(uuid.New().String())
	callback := make(chan *pb.PingRespData)
	node1Local.GetPing().Register(callback)

	ping1ReqId := []byte(uuid.New().String())

	t0 := time.Now()
	node1Local.GetPing().Send("hello unruly", pingReqId, node2Remote.String())

	// internally, node 1 creates an encrypted authenticated session with node 2 and sends the ping request
	// over that session once it is established. Node 1 registers an app-level callback to get the ping response from node 2.
	// The response includes the request id so it can match it with one or more tracked requests it sent.

Loop:
	for {
		select {
		case c := <-callback:
			if bytes.Equal(c.GetMetadata().ReqId, ping1ReqId) {
				log.Info("Got 2nd pong: `%s`. Total RTT: %s", c.GetPong(), time.Now().Sub(t0).String())
				break Loop;
			} else if bytes.Equal(c.GetMetadata().ReqId, pingReqId) {
				log.Info("Got pong: `%s`. Total RTT: %s", c.GetPong(), time.Now().Sub(t0))
				t0 = time.Now()
				go node1Local.GetPing().Send("hello unruly", ping1ReqId, node2Remote.String())
			}
		}
	}
}
