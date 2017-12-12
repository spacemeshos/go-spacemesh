package p2p2

import (
	"github.com/UnrulyOS/go-unruly/assert"
	"github.com/UnrulyOS/go-unruly/log"
	"testing"
)

// a simple interface
type IMessage interface {
	GetMessage() string
	SetMessage(msg string)
}

// a simple type implementing IMessage
type MessageImp struct {
	// internal state
	msg string
}

func (m *MessageImp) GetMessage() string {
	return m.msg
}

func (m *MessageImp) SetMessage(msg string) {
	log.Info("Updating obj id: %s", &m)
	m.msg = msg
}

func NewMessage(msg string) IMessage {
	return &MessageImp{msg: msg}
}

// a func or method that has an interface param accepts both pointers to structs or structs (types)
// as arguments - it is up to the caller to pass a pointer to a struct or a struct.
// When passing a struct it will be copied
func UpdateMessageState(msg IMessage, s string ) {
	msg.SetMessage(s)
}


func TestTypes(t *testing.T) {

	// msg is a pointer to an IMessage implementation
	msg := NewMessage("foo")
	log.Info("Obj id: %s", &msg)

	assert.Equal(t, msg.GetMessage(), "foo", "expected foo" )

	// UpdateMessageState expects IMessage but we can pass a pointer to a type implementing IMessage to it
	UpdateMessageState(msg, "bar")

	assert.Equal(t, msg.GetMessage(), "bar", "expected update to updated state" )

}
