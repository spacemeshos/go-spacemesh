package crypto

import (
	"crypto/rand"
	"encoding/binary"
	"errors"
	"github.com/spacemeshos/go-spacemesh/log"
)

// GetRandomBytesToBuffer puts n random bytes using go crypto.rand into provided buff slice.
// buff: a slice allocated by called to hold n bytes.
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

// GetRandomBytes returns n random bytes. It returns an error if the system's pgn fails.
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

// GetRandomUserPort returns a random port number in the range of [1024-49151).
func GetRandomUserPort() uint32 {
	return GetRandomUInt32(48127) + 1024
}

// GetRandomUInt32 returns a uint32 in the range [0 - max).
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
