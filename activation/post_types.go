package activation

import (
	"encoding/hex"
	"fmt"
)

type PowDifficulty [32]byte

func (d PowDifficulty) String() string {
	return fmt.Sprintf("%X", d[:])
}

// Set implements pflag.Value.Set.
func (f *PowDifficulty) Set(value string) error {
	return f.UnmarshalText([]byte(value))
}

// Type implements pflag.Value.Type.
func (PowDifficulty) Type() string {
	return "PowDifficulty"
}

func (d *PowDifficulty) UnmarshalText(text []byte) error {
	decodedLen := hex.DecodedLen(len(text))
	if decodedLen != 32 {
		return fmt.Errorf("expected 32 bytes, got %d", decodedLen)
	}
	var dst [32]byte
	if _, err := hex.Decode(dst[:], text); err != nil {
		return err
	}
	*d = PowDifficulty(dst)
	return nil
}
