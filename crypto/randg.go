package crypto

import (
	"crypto/rand"
	"encoding/binary"
	"errors"
	"github.com/spacemeshos/go-spacemesh/log"
)

// Put n random bytes using go crypto.rand into provided buff slice
// Buff must be allocated to hold n bytes by caller
func GetRandomBytesToBuffer(n int, buff []byte) error {

	if n == 0 {
		return errors.New("invalid input param - n must be positive")
	}

	if len(buff) < n {
		return errors.New("invalid input param - buff must be allocated to hold n items")
	}

	_, err := rand.Read(buff)

	if err != nil {
		return err
	}

	return nil
}

// Get n random bytes using go crypto.rand
// returns an error when the system pgn malfunctions
func GetRandomBytes(n int) ([]byte, error) {

	if n == 0 {
		return nil, errors.New("invalid input param - n must be positive")
	}

	b := make([]byte, n)
	_, err := rand.Read(b)

	if err != nil {
		return nil, err
	}

	return b, nil
}

// Return a uint64 in range [0 - max)
func GetRandomUInt64(max uint64) uint64 {

	b := make([]byte, 8)
	_, err := rand.Read(b)

	if err != nil {
		log.Error("Failed to get entropy from system", err)
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
		log.Error("Failed to get entropy from system", err)
		panic(err)
	}

	data := binary.BigEndian.Uint32(b)
	return data % max
}
