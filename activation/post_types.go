package activation

import (
	"encoding/hex"
	"fmt"
	"strconv"

	"github.com/spacemeshos/post/config"
)

type PowDifficulty [32]byte

func (d *PowDifficulty) String() string {
	return hex.EncodeToString(d[:])
}

// Set implements pflag.Value.Set.
func (d *PowDifficulty) Set(value string) error {
	return d.UnmarshalText([]byte(value))
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

type PostProviderID struct {
	value *uint32
}

// String implements pflag.Value.String.
func (id PostProviderID) String() string {
	if id.value == nil {
		return ""
	}
	return strconv.FormatUint(uint64(*id.value), 10)
}

// Type implements pflag.Value.Type.
func (PostProviderID) Type() string {
	return "PostProviderID"
}

// Set implements pflag.Value.Set.
func (id *PostProviderID) Set(value string) error {
	if len(value) == 0 {
		id.value = nil
		return nil
	}

	i, err := strconv.ParseUint(value, 10, 32)
	if err != nil {
		return fmt.Errorf("failed to parse PoST Provider ID (\"%s\"): %w", value, err)
	}

	id.value = new(uint32)
	*id.value = uint32(i)
	return nil
}

// SetInt64 sets the value of the PostProviderID to the given int64.
func (id *PostProviderID) SetUint32(value uint32) {
	id.value = &value
}

// Value returns the value of the PostProviderID as a pointer to uint32.
func (id *PostProviderID) Value() *uint32 {
	return id.value
}

type PostPowFlags config.PowFlags

// String implements pflag.Value.String.
func (f PostPowFlags) String() string {
	return fmt.Sprintf("%d", f)
}

// Type implements pflag.Value.Type.
func (PostPowFlags) Type() string {
	return "PostPowFlags"
}

// Set implements pflag.Value.Set.
func (f *PostPowFlags) Set(value string) error {
	if len(value) == 0 {
		*f = 0
		return nil
	}

	i, err := strconv.ParseUint(value, 10, 32)
	if err != nil {
		return fmt.Errorf("failed to parse PoST PoW flags (\"%s\"): %w", value, err)
	}

	*f = PostPowFlags(i)
	return nil
}

func (f *PostPowFlags) Value() config.PowFlags {
	return config.PowFlags(*f)
}

type PostRandomXMode string

const (
	PostRandomXModeFast  PostRandomXMode = "fast"
	PostRandomXModeLight PostRandomXMode = "light"
)

// String implements pflag.Value.String.
func (m PostRandomXMode) String() string {
	return string(m)
}

// Type implements pflag.Value.Type.
func (PostRandomXMode) Type() string {
	return "PostRandomXMode"
}

// Set implements pflag.Value.Set.
func (m *PostRandomXMode) Set(value string) error {
	switch value {
	case string(PostRandomXModeFast):
		*m = PostRandomXModeFast
	case string(PostRandomXModeLight):
		*m = PostRandomXModeLight
	default:
		return fmt.Errorf("invalid PoST RandomX mode (\"%s\")", value)
	}
	return nil
}
