package merkle

import "errors"

// fromHexChar converts a hex character into its value and a success flag.
// Adapted from https://golang.org/src/encoding/hex/hex.go - too bad it is private
func fromHexChar(c byte) (byte, bool) {
	switch {
	case '0' <= c && c <= '9':
		return c - '0', true
	case 'a' <= c && c <= 'f':
		return c - 'a' + 10, true
	case 'A' <= c && c <= 'F':
		return c - 'A' + 10, true
	}

	return 0, false
}
