package crypto

import (
	"crypto/rand"
	"encoding/binary"
	"errors"
	"github.com/spacemeshos/go-spacemesh/log"
)

// Get n random bytes using go crypto.rand
// returns an error when the system pgn malfunctions
func GetRandomBytes(n int) ([]byte, error) {

	if n == 0 {
		return nil, errors.New("invalid input param - n must be positive")
	}

	b := make([]byte, n)
	_, err := rand.Read(b)

	if err != nil {
		log.Error("failed to get entropy from system: %v", err)
		return nil, err
	}

	return b, nil
}

// Return a uint64 in range [0 - max)
func GetRandomUInt64(max uint64) uint64 {

	b := make([]byte, 8)
	_, err := rand.Read(b)

	if err != nil {
		log.Error("failed to get entropy from system: %v", err)
		panic(err)
	}

	data := binary.BigEndian.Uint64(b)
	return data % max
}

// Return a uint32 in range [0 - max)
func GetRandomUInt32(max uint32) uint32 {

	b := make([]byte, 4)
	_, err := rand.Read(b)

	if err != nil {
		log.Error("failed to get entropy from system: %v", err)
		panic(err)
	}

	data := binary.BigEndian.Uint32(b)
	return data % max
}
