package types

import (
	"github.com/spacemeshos/go-spacemesh/common/util"
	"github.com/spacemeshos/go-spacemesh/log"
)

const (
	// BeaconSize in bytes.
	BeaconSize = 4
)

// Beacon defines the beacon value. A beacon is generated once per epoch and is used to
// - verify smesher's VRF signature for proposal/ballot eligibility
// - determine good ballots in verifying tortoise.
type Beacon [BeaconSize]byte

// EmptyBeacon is a canonical empty Beacon.
var EmptyBeacon = Beacon{}

// Hex converts a hash to a hex string.
func (b Beacon) Hex() string { return util.Encode(b[:]) }

// String implements the stringer interface and is used also by the logger when
// doing full logging into a file.
func (b Beacon) String() string { return b.Hex() }

// ShortString returns the first 5 characters of the Beacon, usually for logging purposes.
func (b Beacon) ShortString() string {
	str := b.Hex()
	l := len(str)
	return Shorten(str[util.Min(2, l):], 10)
}

// Bytes gets the byte representation of the underlying hash.
func (b Beacon) Bytes() []byte {
	return b[:]
}

// Field returns a log field. Implements the LoggableField interface.
func (b Beacon) Field() log.Field {
	return log.String("beacon", b.ShortString())
}

func (b *Beacon) MarshalText() ([]byte, error) {
	return util.Base64Encode(b[:]), nil
}

func (b *Beacon) UnmarshalText(buf []byte) error {
	return util.Base64Decode(b[:], buf)
}

// BytesToBeacon sets the first BeaconSize bytes of b to the Beacon's data.
func BytesToBeacon(b []byte) Beacon {
	var beacon Beacon
	copy(beacon[:], b)
	return beacon
}

// HexToBeacon sets byte representation of s to a Beacon.
func HexToBeacon(s string) Beacon {
	return BytesToBeacon(util.FromHex(s))
}
