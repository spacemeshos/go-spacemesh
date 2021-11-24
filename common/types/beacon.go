package types

import (
	"github.com/spacemeshos/go-spacemesh/common/util"
	"github.com/spacemeshos/go-spacemesh/log"
)

// Beacon defines the beacon value. A beacon is generated once per epoch and is used to
// - verify smesher's VRF signature for proposal/ballot eligibility
// - determine good ballots in verifying tortoise.
type Beacon Hash32

// EmptyBeacon is a canonical empty Beacon.
var EmptyBeacon = Beacon{}

// String implements the stringer interface and is used also by the logger when
// doing full logging into a file.
func (b Beacon) String() string {
	return Hash32(b).String()
}

// ShortString returns the first 5 characters of the Beacon, usually for logging purposes.
func (b Beacon) ShortString() string {
	return Hash32(b).ShortString()
}

// Bytes gets the byte representation of the underlying hash.
func (b Beacon) Bytes() []byte {
	return b[:]
}

// Field returns a log field. Implements the LoggableField interface.
func (b Beacon) Field() log.Field {
	return log.String("beacon", b.ShortString())
}

// BytesToBeacon sets b to the Beacon's data.
// If b is larger than len(h), b will be cropped from the left.
func BytesToBeacon(b []byte) Beacon {
	var h Hash32
	h.SetBytes(b)
	return Beacon(h)
}

// HexToBeacon sets byte representation of s to a Beacon.
func HexToBeacon(s string) Beacon {
	return BytesToBeacon(util.FromHex(s))
}
