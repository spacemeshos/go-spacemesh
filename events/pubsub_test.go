package events

import (
	"fmt"
	"github.com/spacemeshos/go-spacemesh/log"
	"github.com/stretchr/testify/assert"
	"nanomsg.org/go-mangos/transport/tcp"
	"os"
	"sync"
	"testing"
	"time"

	"nanomsg.org/go-mangos"
	"nanomsg.org/go-mangos/protocol/pub"
	"nanomsg.org/go-mangos/protocol/sub"
	"nanomsg.org/go-mangos/transport/ipc"
)

func die(format string, v ...interface{}) {
	fmt.Fprintln(os.Stderr, fmt.Sprintf(format, v...))
	os.Exit(1)
}

func date() string {
	return time.Now().Format(time.ANSIC)
}

func server(url string) {
	var sock mangos.Socket
	var err error
	if sock, err = pub.NewSocket(); err != nil {
		die("can't get NewEventPublisher pub socket: %s", err)
	}
	sock.AddTransport(ipc.NewTransport())
	sock.AddTransport(tcp.NewTransport())
	if err = sock.Listen(url); err != nil {
		die("can't listen on pub socket: %s", err.Error())
	}
	for {
		// Could also use sock.RecvMsg to get header
		d := date()
		fmt.Printf("SERVER: PUBLISHING DATE %s\n", d)
		if err = sock.Send([]byte("topic1anton")); err != nil {
			die("Failed publishing: %s", err.Error())
		}
		time.Sleep(time.Second)
	}
}

func client(url string, name string) {
	var sock mangos.Socket
	var err error
	var msg []byte

	if sock, err = sub.NewSocket(); err != nil {
		die("can't get NewEventPublisher sub socket: %s", err.Error())
	}
	sock.AddTransport(ipc.NewTransport())
	sock.AddTransport(tcp.NewTransport())
	if err = sock.Dial(url); err != nil {
		die("can't dial on sub socket: %s", err.Error())
	}
	// Empty byte array effectively subscribes to everything
	err = sock.SetOption(mangos.OptionSubscribe, []byte(""))
	if err != nil {
		die("cannot Subscribe: %s", err.Error())
	}
	for {
		if msg, err = sock.Recv(); err != nil {
			die("Cannot recv: %s", err.Error())
		}
		fmt.Printf("CLIENT(%s): RECEIVED %s\n", name, string(msg))
	}
}

func TestWhatever(t *testing.T) {
	t.Skip()
	url := "tcp://localhost:48844"

	go server(url)
	go client(url, "lala")

	time.Sleep(10 * time.Second)
}

func TestPubSub(t *testing.T) {
	topics := []ChannelId{'a', 'b'}
	url := "tcp://localhost:56565"
	var p *Publisher
	var err error
	p, err = newPublisher(url)
	assert.NoError(t, err)
	defer func() {
		assert.NoError(t, p.sock.Close())
	}()

	payload := []byte("anton2")
	s, err := NewSubscriber(url)
	assert.NoError(t, err)
	_, err = s.Subscribe(topics[0])
	assert.NoError(t, err)
	irrelevantTopic, err := s.Subscribe(topics[1])
	assert.NoError(t, err)
	s.StartListening()
	numOfMessages := 5
	time.Sleep(2 * time.Second)
	defer func() {
		assert.NoError(t, s.sock.Close())
	}()

	wg := sync.WaitGroup{}
	wg.Add(1)
	go func() {
		for i := 0; i < numOfMessages; i++ {
			err = p.publish(topics[0], payload)
			assert.NoError(t, err)
			if err != nil {
				log.Error("wtf : %v", err)
			}
		}
		wg.Done()
	}()

	msg := append([]byte{byte(topics[0])}, payload...)
	tm := time.NewTimer(3 * time.Second)
	counter := numOfMessages
	// check that we didnt get messages on this channel
	irrelvantCounter := 0
loop:
	for {
		select {
		case <-tm.C:
			assert.Fail(t, "didnt receive message")
			break loop
		case rec := <-s.output[topics[0]]:
			counter--
			log.Info("got msg: %v count %v", string(rec), counter)
			if counter == 0 {
				assert.Equal(t, rec, msg)
				log.Info(string(msg))
				break loop
			}
		case <-irrelevantTopic:
			irrelvantCounter++
		}
	}
	assert.Equal(t, 0, irrelvantCounter)
	wg.Wait()
}

func TestPubSub_subscribeAll(t *testing.T) {
	topics := []ChannelId{'a', 'b'}
	url := "tcp://localhost:56565"
	var p *Publisher
	var err error

	p, err = newPublisher(url)
	assert.NoError(t, err)
	defer func() {
		assert.NoError(t, p.sock.Close())
	}()

	payload := []byte("anton2")
	s, err := NewSubscriber(url)
	assert.NoError(t, err)
	_, err = s.Subscribe(topics[0])
	assert.NoError(t, err)
	out, err := s.SubscribeToAll()
	assert.NoError(t, err)
	s.StartListening()
	numOfMessages := 5
	time.Sleep(2 * time.Second)
	defer func() {
		assert.NoError(t, s.sock.Close())
	}()

	for i := 0; i < numOfMessages; i++ {
		err = p.publish(topics[1], payload)
		assert.NoError(t, err)
	}

	//msg := append([]byte{byte(topics[0])}, payload...)
	tm := time.NewTimer(3 * time.Second)
	counter := 0
	allCounter := 0
loop:
	for {
		select {
		case <-tm.C:
			assert.Fail(t, "didnt receive message")
			break loop
		case <-s.output[topics[0]]:
			counter++
		case <-out:
			allCounter++
			if allCounter == numOfMessages {
				break loop
			}
		}
	}
	assert.True(t, allCounter == numOfMessages)
	assert.True(t, counter == 0)
}
