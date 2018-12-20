package hare

import "bytes"

type Signing interface {
	Sign(m []byte) []byte
	Validate(m []byte, sig []byte) bool
}

type MockSigning struct {
	sig []byte
}

func NewMockSigning() *MockSigning {
	return &MockSigning{[]byte{1, 2, 3}}
}

func (mockSigning *MockSigning) Sign(m []byte) []byte {
	return mockSigning.sig
}

func (mockSigning *MockSigning) Validate(m []byte, sig []byte) bool {
	return bytes.Equal(mockSigning.sig, sig)
}
