package hare

import "bytes"

type Signing interface {
	Sign(m []byte) []byte
	Validate(m []byte, sig []byte) bool
}

type MockSigning struct {
}

func NewMockSigning() *MockSigning {
	return &MockSigning{}
}

func (mockSigning *MockSigning) Sign(m []byte) []byte {
	return m
}

func (mockSigning *MockSigning) Validate(m []byte, sig []byte) bool {
	return bytes.Equal(m, sig)
}
