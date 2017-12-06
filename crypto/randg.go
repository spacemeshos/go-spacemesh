package crypto

import (
	"crypto/rand"
	"errors"
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
		return nil, err
	}

	return b, nil
}
