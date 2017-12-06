package crypto

import (
	"crypto/rand"
	"errors"
	"github.com/UnrulyOS/go-unruly/log"
)

// Get n random bytes using go crypto.rand
// returns an error when the system pgn malfunctions
func GetRandomBytes(n int) ([]byte, error) {

	if n == 0 {
		return nil, errors.New("Invalid input param - n must be > 0")
	}

	b := make([]byte, n)
	_, err := rand.Read(b)

	if err != nil {
		log.Error("Failed to get entropy from system: %v", err)
		return nil, err
	}

	return b, nil
}
