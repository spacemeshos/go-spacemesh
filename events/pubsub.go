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

// channelId is the Id on which subscribers must register in order to listen to messages on the channel.
type channelId byte

// Subscriber defines the struct of the receiving end of the pubsub messages.
type Subscriber struct {
	sock      mangos.Socket
	output    map[channelId]chan []byte
	allOutput chan []byte
	getAll    bool
	chanLock  sync.RWMutex
}

// newSubscriber received url string as input on which it will register to receive messages passed by server.
func newSubscriber(url string) (*Subscriber, error) {
	socket, err := sub.NewSocket()
	if err != nil {
		return nil, err
	}
	socket.AddTransport(ipc.NewTransport())
	socket.AddTransport(tcp.NewTransport())
	err = socket.Dial(url)
	if err != nil {
		return nil, err
	}

	return &Subscriber{
		socket,
		make(map[channelId]chan []byte),
		nil,
		false,
		sync.RWMutex{},
	}, nil
}

// startListening runs in a go routine and listens to all channels this subscriber is registered to. it then passes the
// messages received to the appropriate reader channel.
func (sub *Subscriber) startListening() {
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
			if c, ok := sub.output[channelId(data[0])]; ok {
				c <- data
			}
			sub.chanLock.RUnlock()
		}
	}()
}

// close closes the socket which in turn will close the listener func
func (sub *Subscriber) close() error {
	return sub.sock.Close()
}

// subscribe subscribes to the given topic, returns a channel on which data from the topic is received.
func (sub *Subscriber) subscribe(topic channelId) (chan []byte, error) {
	if _, ok := sub.output[topic]; !ok {
		sub.chanLock.Lock()
		sub.output[topic] = make(chan []byte, channelBuffer)
		sub.chanLock.Unlock()
	}
	err := sub.sock.SetOption(mangos.OptionSubscribe, []byte{byte(topic)})
	if err != nil {
		return nil, err
	}
	/*if err == nil {
		err = sub.SetOption(mangos.OptionRecvDeadline, 10*time.Second)
	}*/
	return sub.output[topic], err
}

// subscribe subscribes to the given topic, returns a channel on which data from the topic is received.
func (sub *Subscriber) subscribeToAll() (chan []byte, error) {

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
		return nil, fmt.Errorf("can't get newEventPublisher pub socket: %s", err)
	}
	sock.AddTransport(ipc.NewTransport())
	sock.AddTransport(tcp.NewTransport())
	if err = sock.Listen(url); err != nil {
		return nil, fmt.Errorf("can't listen on pub socket: %s", err.Error())
	}
	time.Sleep(time.Second)
	p := &Publisher{sock: sock}
	return p, nil
}

func (p *Publisher) publish(topic channelId, payload []byte) error {
	msg := append([]byte{byte(topic)}, payload...)
	log.Info("sending message %v", string(msg))
	err := p.sock.Send(msg)
	return err
}

func subscribe(socket mangos.Socket, topic string) error {
	err := socket.SetOption(mangos.OptionSubscribe, []byte(topic))
	if err == nil {
		err = socket.SetOption(mangos.OptionRecvDeadline, 10*time.Second)
	}
	return err
}

func publish(socket mangos.Socket, topic, message string) error {
	err := socket.Send([]byte(fmt.Sprintf("%s|%s", topic, message)))
	return err
}

func receive(socket mangos.Socket) (string, error) {
	message, err := socket.Recv()
	return string(message), err
}
