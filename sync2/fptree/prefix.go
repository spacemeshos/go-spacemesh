package fptree

import (
	"fmt"
	"math/bits"
	"strings"

	"github.com/spacemeshos/go-spacemesh/sync2/rangesync"
)

const (
	// prefixBytes is the number of bytes in a prefix.
	prefixBytes = rangesync.FingerprintSize
	// maxPrefixLen is the maximum length of a prefix in bits.
	maxPrefixLen = prefixBytes * 8
)

// prefix is a prefix of a key, represented as a bit string.
type prefix struct {
	// the bytes of the prefix, starting from the highest byte.
	b [prefixBytes]byte
	// length of the prefix in bits.
	l uint16
}

// emptyPrefix is the empty prefix (length 0).
var emptyPrefix = prefix{}

// prefixFromKeyBytes returns a prefix made from a key by using the maximum possible
// number of its bytes.
func prefixFromKeyBytes(k rangesync.KeyBytes) (p prefix) {
	p.l = uint16(copy(p.b[:], k) * 8)
	return p
}

// len returns the length of the prefix.
func (p prefix) len() int {
	return int(p.l)
}

// left returns the prefix with one more 0 bit.
func (p prefix) left() prefix {
	if p.l == maxPrefixLen {
		panic("BUG: max prefix len reached")
	}
	p.b[p.l/8] &^= 1 << (7 - p.l%8)
	p.l++
	return p
}

// right returns the prefix with one more 1 bit.
func (p prefix) right() prefix {
	if p.l == maxPrefixLen {
		panic("BUG: max prefix len reached")
	}
	p.b[p.l/8] |= 1 << (7 - p.l%8)
	p.l++
	return p
}

// String implements fmt.Stringer.
func (p prefix) String() string {
	if p.len() == 0 {
		return "<0>"
	}
	var sb strings.Builder
	for _, b := range p.b[:(p.l+7)/8] {
		sb.WriteString(fmt.Sprintf("%08b", b))
	}
	return fmt.Sprintf("<%d:%s>", p.l, sb.String()[:p.l])
}

// highBit returns the highest bit of the prefix as bool (false=0, true=1).
// If the prefix is empty, it returns false.
func (p prefix) highBit() bool {
	return p.l != 0 && p.b[0]&0x80 != 0
}

// minID sets the key to the smallest key with the prefix.
func (p prefix) minID(k rangesync.KeyBytes) {
	nb := (p.l + 7) / 8
	if len(k) < int(nb) {
		panic("BUG: id slice too small")
	}
	copy(k[:nb], p.b[:nb])
	clear(k[nb:])
}

// idAfter sets the key to the key immediately after the largest key with the prefix.
// idAfter returns true if the resulting id is zero, meaning wraparound.
func (p prefix) idAfter(k rangesync.KeyBytes) bool {
	nb := (p.l + 7) / 8
	if len(k) < int(nb) {
		panic("BUG: id slice too small")
	}
	// Copy prefix bits to the key, set all the bits after the prefix to 1, then
	// increment the key.
	copy(k[:nb], p.b[:nb])
	if p.l%8 != 0 {
		k[nb-1] |= (1<<(8-p.l%8) - 1)
	}
	for i := int(nb); i < len(k); i++ {
		k[i] = 0xff
	}
	return k.Inc()
}

// shift removes the highest bit from the prefix.
func (p prefix) shift() prefix {
	switch l := p.len(); l {
	case 0:
		panic("BUG: can't shift zero prefix")
	case 1:
		return emptyPrefix
	default:
		var c byte
		for nb := int((p.l+7)/8) - 1; nb >= 0; nb-- {
			c, p.b[nb] = (p.b[nb]&0x80)>>7, (p.b[nb]<<1)|c
		}
		p.l--
		return p
	}
}

// match returns true if the prefix matches the key, that is,
// all the prefix bits are equal to the corresponding bits of the key.
func (p prefix) match(b rangesync.KeyBytes) bool {
	if int(p.l) > len(b)*8 {
		panic("BUG: id slice too small")
	}
	if p.l == 0 {
		return true
	}
	bi := p.l / 8
	for i, v := range p.b[:bi] {
		if b[i] != v {
			return false
		}
	}
	s := p.l % 8
	return s == 0 || p.b[bi]>>(8-s) == b[bi]>>(8-s)
}

// preFirst0 returns the longest prefix of the key that consists entirely of binary 1s.
func preFirst0(k rangesync.KeyBytes) prefix {
	var p prefix
	nb := min(prefixBytes, len(k))
	for n, b := range k[:nb] {
		if b != 0xff {
			nOnes := bits.LeadingZeros8(^b)
			if nOnes != 0 {
				p.b[n] = 0xff << (8 - nOnes)
				p.l += uint16(nOnes)
			}
			break
		}
		p.b[n] = 0xff
		p.l += 8
	}
	return p
}

// preFirst1 returns the longest prefix of the key that consists entirely of binary 0s.
func preFirst1(k rangesync.KeyBytes) prefix {
	var p prefix
	nb := min(prefixBytes, len(k))
	for _, b := range k[:nb] {
		if b != 0 {
			p.l += uint16(bits.LeadingZeros8(b))
			break
		}
		p.l += 8
	}
	return p
}

// commonPrefix returns common prefix between two keys.
func commonPrefix(a, b rangesync.KeyBytes) prefix {
	var p prefix
	nb := min(prefixBytes, len(a), len(b))
	for n, v1 := range a[:nb] {
		v2 := b[n]
		p.b[n] = v1
		if v1 != v2 {
			nEqBits := bits.LeadingZeros8(v1 ^ v2)
			if nEqBits != 0 {
				// Clear unused bits in the last used prefix byte
				p.b[n] &^= 1<<(8-nEqBits) - 1
				p.l += uint16(nEqBits)
			} else {
				p.b[n] = 0
			}
			break
		}
		p.l += 8
	}
	return p
}
