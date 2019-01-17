package hare

type Signing interface {
	Sign(m []byte) []byte
}

type MockSigning struct {
}

func NewMockSigning() *MockSigning {
	return &MockSigning{}
}

func (mockSigning *MockSigning) Sign(m []byte) []byte {
	return m
}