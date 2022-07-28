package address

import (
	"encoding/hex"
	"errors"
	"fmt"
	"math/big"

	"github.com/cosmos/btcutil/bech32"
	"github.com/spacemeshos/go-scale"

	"github.com/spacemeshos/go-spacemesh/common/util"
	"github.com/spacemeshos/go-spacemesh/hash"
	"github.com/spacemeshos/go-spacemesh/log"
)

const (
	// AddressLength is the expected length of the address.
	AddressLength = 24
)

// ErrWrongAddressLength is returned when the length of the address is not correct.
var ErrWrongAddressLength = errors.New("wrong address length")

// Address represents the address of a spacemesh account with AddressLength length.
type Address [AddressLength]byte // contains slice 8 bytes unsigned integers

// NewAddress returns a new Address from a byte slice.
func NewAddress(src string) (Address, error) {
	var addr Address
	hrp, data, err := bech32.DecodeNoLimit(src)
	if err != nil {
		return addr, fmt.Errorf("error decode to bech32: %w", err)
	}

	// for encoding bech32 uses slice of 5-bit unsigned integers. convert it back it 8-bit uints.
	dataConverted, err := bech32.ConvertBits(data, 5, 8, true)
	if err != nil {
		return addr, fmt.Errorf("error convert bits to 8 bits: %w", err)
	}

	// AddressLength+1 cause ConvertBits append empty byte to the end of the slice.
	if len(dataConverted) != AddressLength+1 {
		return addr, fmt.Errorf("expected %d bytes, got %d: %w", AddressLength, len(data), ErrWrongAddressLength)
	}

	if conf.NetworkHRP != hrp {
		return addr, fmt.Errorf("wrong network id: expected `%s`, got `%s`: %w", conf.NetworkHRP, hrp, ErrUnsupportedNetwork)
	}

	copy(addr[:], dataConverted[:])
	return addr, nil // todo 3315
}

// BytesToAddress returns Address with value b.
// If b is larger than len(h), b will be cropped from the left.
func BytesToAddress(b []byte) (Address, error) {
	var a Address
	if len(b) != len(a) {
		return a, ErrWrongAddressLength
	}
	copy(a[:], b)
	return a, nil
}

// Bytes gets the string representation of the underlying address.
func (a Address) Bytes() []byte { return a[:] }

// Big converts an address to a big integer.
func (a Address) Big() *big.Int { return new(big.Int).SetBytes(a[:]) }

// Hex returns an EIP55-compliant hex string representation of the address.
func (a Address) Hex() string {
	unchecksummed := hex.EncodeToString(a[:])
	sha := hash.New()
	sha.Write([]byte(unchecksummed))
	hh := sha.Sum(nil)

	result := []byte(unchecksummed)
	for i := 0; i < len(result); i++ {
		hashByte := hh[i/2]
		if i%2 == 0 {
			hashByte = hashByte >> 4
		} else {
			hashByte &= 0xf
		}
		if result[i] > '9' && hashByte > 7 {
			result[i] -= 32
		}
	}
	return "0x" + string(result)
}

// String implements fmt.Stringer.
func (a Address) String() string {
	dataConverted, err := bech32.ConvertBits(a[:], 8, 5, true)
	if err != nil {
		log.Panic("error convert bits to 8 bits: ", err.Error())
	}

	result, err := bech32.Encode(conf.NetworkHRP, dataConverted)
	if err != nil {
		log.Panic("error encode to bech32: ", err.Error())
	}
	return result
}

// Field returns a log field. Implements the LoggableField interface.
func (a Address) Field() log.Field {
	return log.String("address", a.String())
}

// Short returns the first 7 characters of the address hex representation (incl. "0x"), for logging purposes.
func (a Address) Short() string {
	hx := a.String()
	return hx[:util.Min(7, len(hx))]
}

// Format implements fmt.Formatter, forcing the byte slice to be formatted as is,
// without going through the stringer interface used for logging.
func (a Address) Format(s fmt.State, c rune) {
	_, _ = fmt.Fprintf(s, "%"+string(c), a[:])
}

// setBytes sets the address to the value of b.
// If b is larger than len(a) it will panic.
func (a *Address) setBytes(b []byte) {
	if len(b) > len(a) {
		b = b[len(b)-AddressLength:]
	}
	copy(a[AddressLength-len(b):], b)
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
	addr.setBytes(publicKey)
	return addr
}

// ByteToAddress converts a byte array to an address.
func ByteToAddress(b byte) Address {
	var data [AddressLength]byte
	data[0] = b
	return GenerateAddress(data[:])
}
