package util

import (
	"math/big"
)

// WeightFromInt64 converts an int64 to Weight.
func WeightFromInt64(value int64) Weight {
	return Weight{Rat: new(big.Rat).SetInt64(value)}
}

// WeightFromUint64 converts an uint64 to Weight.
func WeightFromUint64(value uint64) Weight {
	return Weight{Rat: new(big.Rat).SetUint64(value)}
}

// WeightFromFloat64 converts a float64 to Weight.
func WeightFromFloat64(value float64) Weight {
	return Weight{Rat: new(big.Rat).SetFloat64(value)}
}

// WeightFromNumDenom converts an uint64 tuple (numerator, denominator) to Weight.
func WeightFromNumDenom(num, denom uint64) Weight {
	return Weight{Rat: new(big.Rat).SetFrac(new(big.Int).SetUint64(num), new(big.Int).SetUint64(denom))}
}

// Weight represents weight for any ATX/ballot.
// note: this is golang specific and is used to do math on weight.
// for representing weight over the wire or data persistence, use types.RatNum.
type Weight struct {
	*big.Rat
}

// Add adds `other` to weight.
func (w Weight) Add(other Weight) Weight {
	if w.Rat == nil {
		w.Rat = new(big.Rat)
	}
	if other.Rat == nil {
		return w
	}
	w.Rat.Add(w.Rat, other.Rat)
	return w
}

// Sub subtracts `other` from weight.
func (w Weight) Sub(other Weight) Weight {
	if other.Rat == nil {
		return w
	}
	if w.Rat == nil {
		w.Rat = new(big.Rat)
	}
	w.Rat.Sub(w.Rat, other.Rat)
	return w
}

// Div divides weight by `other` and return the quotient.
func (w Weight) Div(other Weight) Weight {
	if w.Rat == nil {
		w.Rat = new(big.Rat)
	}
	w.Rat.Quo(w.Rat, other.Rat)
	return w
}

// Mul multiplies weight by `other`.
func (w Weight) Mul(other Weight) Weight {
	if w.Rat == nil {
		w.Rat = new(big.Rat)
	}
	w.Rat.Mul(w.Rat, other.Rat)
	return w
}

// Neg multiples weight by -1.
func (w Weight) Neg() Weight {
	if w.Rat == nil {
		return w
	}
	w.Rat.Neg(w.Rat)
	return w
}

// Copy copies weight.
func (w Weight) Copy() Weight {
	if w.Rat == nil {
		return w
	}
	other := Weight{Rat: new(big.Rat)}
	other.Rat = other.Rat.Set(w.Rat)
	return other
}

// Fraction multiples weight by `frac`.
func (w Weight) Fraction(frac *big.Rat) Weight {
	if w.Rat == nil {
		return w
	}
	w.Rat = w.Rat.Mul(w.Rat, frac)
	return w
}

// Cmp returns 1 if weight > `other`, -1 if weight < `other`, and 0 if weight == `other`.
func (w Weight) Cmp(other Weight) int {
	if w.Rat.Sign() == 1 && w.Rat.Cmp(other.Rat) == 1 {
		return 1
	}
	if w.Rat.Sign() == -1 && new(big.Rat).Abs(w.Rat).Cmp(other.Rat) == 1 {
		return -1
	}
	return 0
}

func (w Weight) String() string {
	if w.IsNil() {
		return "0"
	}
	if w.Rat.IsInt() {
		return w.Rat.Num().String()
	}
	return w.Rat.FloatString(2)
}

// IsNil returns true if weight is nil.
func (w Weight) IsNil() bool {
	return w.Rat == nil
}
