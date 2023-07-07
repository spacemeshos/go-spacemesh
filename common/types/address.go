package types

import (
	"errors"
	"fmt"

	"github.com/cosmos/btcutil/bech32"
	"github.com/spacemeshos/go-scale"

	"github.com/spacemeshos/go-spacemesh/log"
)

const (
	// AddressLength is the expected length of the address.
	AddressLength = 24
	// AddressReservedSpace define how much bytes from top is reserved in address for future.
	AddressReservedSpace = 4
)

var (
	// ErrWrongAddressLength is returned when the length of the address is not correct.
	ErrWrongAddressLength = errors.New("wrong address length")
	// ErrUnsupportedNetwork is returned when a network is not supported.
	ErrUnsupportedNetwork = errors.New("unsupported network")
	// ErrDecodeBech32 is returned when an error occurs during decoding bech32.
	ErrDecodeBech32 = errors.New("error decoding bech32")
	// ErrMissingReservedSpace is returned if top bytes of address is not 0.
	ErrMissingReservedSpace = errors.New("missing reserved space")
)

// Config is the configuration of the address package.
type Config struct {
	NetworkHRP string `mapstructure:"network-hrp"`
}

var networkHrp = "sm"

func SetAddressHRP(update string) {
	networkHrp = update
}

// DefaultAddressConfig returns the default configuration of the address package.
func DefaultAddressConfig() *Config {
	return &Config{
		NetworkHRP: "sm",
	}
}

// DefaultTestAddressConfig returns the default test configuration of the address package.
func DefaultTestAddressConfig() *Config {
	return &Config{
		NetworkHRP: "stest",
	}
}

// Address represents the address of a spacemesh account with AddressLength length.
type Address [AddressLength]byte

// StringToAddress returns a new Address from a given string like `sm1abc...`.
func StringToAddress(src string) (Address, error) {
	var addr Address
	hrp, data, err := bech32.DecodeNoLimit(src)
	if err != nil {
		return addr, fmt.Errorf("%s: %w", ErrDecodeBech32, err)
	}

	// for encoding bech32 uses slice of 5-bit unsigned integers. convert it back it 8-bit uints.
	dataConverted, err := bech32.ConvertBits(data, 5, 8, true)
	if err != nil {
		return addr, fmt.Errorf("error converting bech32 bits: %w", err)
	}

	// AddressLength+1 cause ConvertBits append empty byte to the end of the slice.
	if len(dataConverted) != AddressLength+1 {
		return addr, fmt.Errorf("expected %d bytes, got %d: %w", AddressLength, len(data), ErrWrongAddressLength)
	}
	if networkHrp != hrp {
		return addr, fmt.Errorf("wrong network id: expected `%s`, got `%s`: %w", networkHrp, hrp, ErrUnsupportedNetwork)
	}
	// check that first 4 bytes are 0.
	for i := 0; i < AddressReservedSpace; i++ {
		if dataConverted[i] != 0 {
			return addr, fmt.Errorf("expected first %d bytes to be 0, got %d: %w", AddressReservedSpace, dataConverted[i], ErrMissingReservedSpace)
		}
	}

	copy(addr[:], dataConverted[:])
	return addr, nil
}

// Bytes gets the string representation of the underlying address.
func (a Address) Bytes() []byte { return a[:] }

// IsEmpty checks if address is empty.
func (a Address) IsEmpty() bool {
	for i := AddressReservedSpace; i < AddressLength; i++ {
		if a[i] != 0 {
			return false
		}
	}
	return true
}

// String implements fmt.Stringer.
func (a Address) String() string {
	dataConverted, err := bech32.ConvertBits(a[:], 8, 5, true)
	if err != nil {
		log.Panic("error converting bech32 bits: ", err.Error())
	}

	result, err := bech32.Encode(networkHrp, dataConverted)
	if err != nil {
		log.Panic("error encoding to bech32: ", err.Error())
	}
	return result
}

// Field returns a log field. Implements the LoggableField interface.
func (a Address) Field() log.Field {
	return log.String("address", a.String())
}

// Format implements fmt.Formatter, forcing the byte slice to be formatted as is,
// without going through the stringer interface used for logging.
func (a Address) Format(s fmt.State, c rune) {
	_, _ = fmt.Fprintf(s, "%"+string(c), a[:])
}

// EncodeScale implements scale codec interface.
func (a *Address) EncodeScale(e *scale.Encoder) (int, error) {
	return scale.EncodeByteArray(e, a[:])
}

// DecodeScale implements scale codec interface.
func (a *Address) DecodeScale(d *scale.Decoder) (int, error) {
	return scale.DecodeByteArray(d, a[:])
}

// GenerateAddress generates an address from a public key.
func GenerateAddress(publicKey []byte) Address {
	var addr Address
	if len(publicKey) > len(addr)-AddressReservedSpace {
		publicKey = publicKey[len(publicKey)-AddressLength+AddressReservedSpace:]
	}
	copy(addr[AddressReservedSpace:], publicKey[:])
	return addr
}

// GetHRPNetwork returns the Human-Readable-Part of bech32 addresses for a networkID.
func (a Address) GetHRPNetwork() string {
	return networkHrp
}
