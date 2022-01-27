package types

import (
	"bytes"

	"github.com/spacemeshos/go-spacemesh/common/util"
	"github.com/spacemeshos/go-spacemesh/log"
)

// Beacon defines the beacon value. A beacon is generated once per epoch and is used to
// - verify smesher's VRF signature for proposal/ballot eligibility
// - determine good ballots in verifying tortoise.
type Beacon []byte

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

// Equal returns true if the other beacon is equal to this one.
func (b Beacon) Equal(other Beacon) bool {
	return bytes.Equal(b, other)
}

// Field returns a log field. Implements the LoggableField interface.
func (b Beacon) Field() log.Field {
	return log.String("beacon", b.ShortString())
}

// HexToBeacon sets byte representation of s to a Beacon.
func HexToBeacon(s string) Beacon {
	return util.FromHex(s)
}
