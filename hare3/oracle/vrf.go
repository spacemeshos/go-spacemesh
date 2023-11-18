package oracle

import (
	"math"

	"github.com/spacemeshos/fixed"
	"github.com/spacemeshos/go-spacemesh/codec"
	"github.com/spacemeshos/go-spacemesh/common/types"
	"github.com/spacemeshos/go-spacemesh/signing"
)

// higher values result in an overflow when calculating CDF
const maxSupportedWeight = (math.MaxInt32 / 2) + 1

func compute(
	committee uint16,
	weight, totalWeight uint64,
	count uint16,
	sig types.VrfSignature,
) uint16 {
	if uint64(committee) > totalWeight {
		weight *= uint64(committee)
		totalWeight *= uint64(committee)
	}
	frac := fixed.FracFromBytes(sig[:8])
	p := fixed.DivUint64(uint64(committee), totalWeight)
	n := int(weight)
	for i := 0; i < n; i++ {
		if fixed.BinCDF(int(weight), p, i).GreaterThan(frac) {
			// even with large N and large P, x will be << 2^16, so this cast is safe
			return uint16(i)
		}
	}
	// since BinCDF(n, p, n) is 1 for any p, this code can only be reached if n is much smaller
	// than 2^16 (so that BinCDF(n, p, n-1) is still lower than vrfFrac)
	return uint16(n)
}

func validate(
	committee uint16,
	weight, totalWeight uint64,
	count uint16,
	sig types.VrfSignature,
) bool {
	if uint64(committee) > totalWeight {
		weight *= uint64(committee)
		totalWeight *= uint64(committee)
	}
	frac := fixed.FracFromBytes(sig[:8])
	p := fixed.DivUint64(uint64(committee), totalWeight)
	if !fixed.BinCDF(int(weight), p, int(count-1)).GreaterThan(frac) &&
		frac.LessThan(fixed.BinCDF(int(weight), p, int(count))) {
		return true
	}
	return false
}

func genVRF(
	signer *signing.VRFSigner,
	beacon types.Beacon,
	layer types.LayerID,
	round uint32,
) types.VrfSignature {
	return signer.Sign(
		codec.MustEncode(&VRFInput{Type: types.EligibilityHare, Beacon: beacon, Round: round, Layer: layer}),
	)
}

//go:generate scalegen -types VRFInput

type VRFInput struct {
	Type   types.EligibilityType
	Beacon types.Beacon
	Round  uint32
	Layer  types.LayerID
}
