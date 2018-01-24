package crypto

import "github.com/google/uuid"

// UUIDString returns a new random type-4 UUID string.
func UUIDString() string {
	return uuid.New().String()
}

// UUID returns a new random type-4 UUID raw bytes.
func UUID() []byte {
	return []byte(uuid.New().String())
}
