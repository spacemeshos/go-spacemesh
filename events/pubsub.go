package events

import (
	"fmt"
	"sync"
	"time"

	"github.com/spacemeshos/go-spacemesh/log"
	"nanomsg.org/go-mangos"
	"nanomsg.org/go-mangos/protocol/pub"
	"nanomsg.org/go-mangos/protocol/sub"
	"nanomsg.org/go-mangos/transport/ipc"
	"nanomsg.org/go-mangos/transport/tcp"
)

// channelBuffer defines the listening channel buffer size.
const channelBuffer = 100

// ChannelID is the ID on which subscribers must register in order to listen to messages on the channel.
type ChannelID byte

// Subscriber defines the struct of the receiving end of the pubsub messages.
type Subscriber struct {
	sock   mangos.Socket
	closer chan struct{}

	chanLock  sync.RWMutex
	output    map[ChannelID]chan []byte
	allOutput chan []byte
}

// NewSubscriber received url string as input on which it will register to receive messages passed by server.
func NewSubscriber(url string) (*Subscriber, error) {
	socket, err := sub.NewSocket()
	if err != nil {
		return nil, err
	}
	socket.AddTransport(ipc.NewTransport())
	socket.AddTransport(tcp.NewTransport())
	socket.SetOption(mangos.OptionBestEffort, false)
	socket.SetOption(mangos.OptionReadQLen, 10000)

	err = socket.Dial(url)
	if err != nil {
		return nil, err
	}

	return &Subscriber{
		sock:   socket,
		output: make(map[ChannelID]chan []byte, 100),
		closer: make(chan struct{}),
	}, nil
}

// StartListening runs in a go routine and listens to all channels this subscriber is registered to. it then passes the
// messages received to the appropriate reader channel.
func (sub *Subscriber) StartListening() {
	go func() {
		for {
			payload, err := sub.sock.Recv()
			log.With().Debug("got message", log.Binary("payload", payload))
			if err != nil {
				if err.Error() == "connection closed" {
					log.Warning("connection closed on pubsub reader")
				} else {
					log.Error("error on recv: %v", err)
				}
				return
			}
			sub.route(payload)
		}
	}()
}

func (sub *Subscriber) route(payload []byte) {
	// cannot route empty message
	if len(payload) < 1 {
		log.Error("got empty message")
		return
	}

	sub.chanLock.RLock()
	defer sub.chanLock.RUnlock()

	var receiver chan []byte
	if sub.allOutput != nil {
		receiver = sub.allOutput
	} else {
		if c, ok := sub.output[ChannelID(payload[0])]; ok {
			receiver = c
		}
	}
	if receiver == nil {
		return
	}
	select {
	case receiver <- payload:
	case <-sub.closer:
	}
}

// Close closes the socket which in turn will Close the listener func
func (sub *Subscriber) Close() error {
	select {
	case <-sub.closer:
	default:
		close(sub.closer)
		return sub.sock.Close()
	}
	return nil
}

// Subscribe subscribes to the given topic, returns a channel on which data from the topic is received.
func (sub *Subscriber) Subscribe(topic ChannelID) (chan []byte, error) {
	sub.chanLock.Lock()
	defer sub.chanLock.Unlock()

	if _, ok := sub.output[topic]; !ok {
		sub.output[topic] = make(chan []byte, channelBuffer)
	}
	err := sub.sock.SetOption(mangos.OptionSubscribe, []byte{byte(topic)})
	if err != nil {
		return nil, err
	}
	return sub.output[topic], nil
}

// SubscribeToAll subscribes to all available topics.
func (sub *Subscriber) SubscribeToAll() (chan []byte, error) {
	sub.chanLock.Lock()
	defer sub.chanLock.Unlock()
	if sub.allOutput == nil {
		allOutput := make(chan []byte, channelBuffer)
		err := sub.sock.SetOption(mangos.OptionSubscribe, []byte(""))
		if err != nil {
			return nil, err
		}
		sub.allOutput = allOutput
	}
	return sub.allOutput, nil
}

// Publisher is a wrapper for mangos pubsub publisher socket
type Publisher struct {
	sock mangos.Socket
}

func newPublisher(url string) (*Publisher, error) {
	sock, err := pub.NewSocket()
	if err != nil {
		return nil, fmt.Errorf("can't get NewEventPublisher pub socket: %s", err)
	}
	sock.AddTransport(ipc.NewTransport())
	sock.AddTransport(tcp.NewTransport())
	sock.SetOption(mangos.OptionBestEffort, false)
	sock.SetOption(mangos.OptionWriteQLen, 10000)
	if err = sock.Listen(url); err != nil {
		return nil, fmt.Errorf("can't listen on pub socket: %s", err.Error())
	}
	time.Sleep(time.Second)
	p := &Publisher{sock: sock}
	return p, nil
}

func (p *Publisher) publish(topic ChannelID, payload []byte) error {
	msg := append([]byte{byte(topic)}, payload...)
	log.With().Debug("sending msg", log.Binary("payload", payload))
	err := p.sock.Send(msg)
	return err
}

// Close closes the publishers output socket
func (p *Publisher) Close() {
	p.sock.Close()
}
