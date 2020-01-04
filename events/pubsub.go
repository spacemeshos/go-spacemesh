package events

import (
	"fmt"
	"github.com/spacemeshos/go-spacemesh/log"
	"nanomsg.org/go-mangos"
	"nanomsg.org/go-mangos/protocol/pub"
	"nanomsg.org/go-mangos/protocol/sub"
	"nanomsg.org/go-mangos/transport/ipc"
	"nanomsg.org/go-mangos/transport/tcp"
	"sync"
	"time"
)

// channelBuffer defines the listening channel buffer size.
const channelBuffer = 100

// ChannelId is the Id on which subscribers must register in order to listen to messages on the channel.
type ChannelId byte

// Subscriber defines the struct of the receiving end of the pubsub messages.
type Subscriber struct {
	sock      mangos.Socket
	output    map[ChannelId]chan []byte
	allOutput chan []byte
	chanLock  sync.RWMutex
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
		socket,
		make(map[ChannelId]chan []byte, 100),
		nil,
		sync.RWMutex{},
	}, nil
}

// StartListening runs in a go routine and listens to all channels this subscriber is registered to. it then passes the
// messages received to the appropriate reader channel.
func (sub *Subscriber) StartListening() {
	go func() {
		for {
			data, err := sub.sock.Recv()
			log.Debug("got msg %v", string(data))
			if err != nil {
				if err.Error() == "connection closed" {
					log.Warning("connection closed on pubsub reader")
				} else {
					log.Error("error on recv: %v", err)
				}
				return
			}
			// cannot route empty message
			if len(data) < 1 {
				log.Error("got empty message")
			}
			sub.chanLock.RLock()
			if sub.allOutput != nil {
				sub.allOutput <- data
			}
			if c, ok := sub.output[ChannelId(data[0])]; ok {
				c <- data
			}
			sub.chanLock.RUnlock()
		}
	}()
}

// Close closes the socket which in turn will Close the listener func
func (sub *Subscriber) Close() error {
	return sub.sock.Close()
}

// Subscribe subscribes to the given topic, returns a channel on which data from the topic is received.
func (sub *Subscriber) Subscribe(topic ChannelId) (chan []byte, error) {
	sub.chanLock.Lock()
	defer sub.chanLock.Unlock()
	if _, ok := sub.output[topic]; !ok {
		sub.output[topic] = make(chan []byte, channelBuffer)
	}
	err := sub.sock.SetOption(mangos.OptionSubscribe, []byte{byte(topic)})
	if err != nil {
		return nil, err
	}
	return sub.output[topic], err
}

// SubscribeAll subscribes to all available topics.
func (sub *Subscriber) SubscribeToAll() (chan []byte, error) {
	err := sub.sock.SetOption(mangos.OptionSubscribe, []byte(""))
	if err != nil {
		return nil, err
	}
	sub.chanLock.Lock()
	sub.allOutput = make(chan []byte)
	sub.chanLock.Unlock()
	return sub.allOutput, err
}

type Publisher struct {
	sock mangos.Socket
}

func newPublisher(url string) (*Publisher, error) {
	var sock mangos.Socket
	var err error
	if sock, err = pub.NewSocket(); err != nil {
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

func (p *Publisher) publish(topic ChannelId, payload []byte) error {
	msg := append([]byte{byte(topic)}, payload...)
	log.Debug("sending message %v", string(msg))
	err := p.sock.Send(msg)
	return err
}
