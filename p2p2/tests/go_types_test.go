package tests

import (
	"github.com/UnrulyOS/go-unruly/assert"
	"testing"
)

// a simple public type
type Message interface {
	GetMessage() string
	SetMessage(msg string)
}

// A a simple internal type implementing Message
// notice lowercase name and Imp indicating implementation type
type messageImp struct {
	// internal state
	msg string
}

func (m *messageImp) GetMessage() string {
	return m.msg
}

func (m *messageImp) SetMessage(msg string) {
	m.msg = msg
}

func (m messageImp) SetMessageEx(msg string) {
	m.msg = msg
}

// public constructor should return the publc interface type - not pointer to imp class
func NewMessage(msg string) Message {
	return &messageImp{msg: msg}
}

// a func or method that has an interface param accepts both pointers to structs or structs (types)
// as arguments - it is up to the caller to pass a pointer to a struct or a struct.
// When passing a struct it will be copied
func UpdateMessageState(msg Message, s string) {
	msg.SetMessage(s)
}

func TestTypes(t *testing.T) {

	// msg is a pointer to a Message implementation
	msg := NewMessage("foo")

	assert.Equal(t, msg.GetMessage(), "foo", "expected foo")

	// UpdateMessageState expects IMessage but we can pass a pointer to a type implementing IMessage to it
	UpdateMessageState(msg, "bar")

	assert.Equal(t, msg.GetMessage(), "bar", "expected update to updated state")

	// Can't pass MsssageImp to a function - only a pointer to it
	msg1 := messageImp{"foo"}

	UpdateMessageState(&msg1, "bar")

}
