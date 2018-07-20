package crypto

import "github.com/google/uuid"

type UUID [16]byte

// UUIDString returns a new random type-4 UUID string.
func UUIDString() string {
	return uuid.New().String()
}

// NewUUID returns a new random type-4 UUID raw bytes.
func NewUUID() [16]byte {
	return uuid.New()
}
