package util

import (
	"fmt"
	"math"
	"math/big"

	"github.com/spacemeshos/fixed"
	"github.com/spacemeshos/go-scale"
)

var ZeroWeight = Weight{}

// WeightFromInt64 converts an int64 to Weight.
func WeightFromInt64(value int64) Weight {
	return Weight{Fixed: fixed.New64(value)}
}

// WeightFromUint64 converts an uint64 to Weight.
func WeightFromUint64(value uint64) Weight {
	if value > math.MaxInt64 {
		panic(fmt.Sprintf("%d overflows max int64 value", value))
	}
	return Weight{Fixed: fixed.New64(int64(value))}
}

// WeightFromFloat64 converts a float64 to Weight.
func WeightFromFloat64(value float64) Weight {
	return Weight{Fixed: fixed.From(value)}
}

// WeightFromNumDenom converts an uint64 tuple (numerator, denominator) to Weight.
func WeightFromNumDenom(num, denom uint64) Weight {
	return Weight{Fixed: fixed.DivUint64(num, denom)}
}

// Weight represents weight for any ATX/ballot.
type Weight struct {
	fixed.Fixed
}

// Add adds `other` to weight.
func (w Weight) Add(other Weight) Weight {
	return Weight{Fixed: w.Fixed.Add(other.Fixed)}
}

// Sub subtracts `other` from weight.
func (w Weight) Sub(other Weight) Weight {
	return Weight{Fixed: w.Fixed.Sub(other.Fixed)}
}

// Div divides weight by `other` and return the quotient.
func (w Weight) Div(other Weight) Weight {
	return Weight{Fixed: w.Fixed.Div(other.Fixed)}
}

// Mul multiplies weight by `other`.
func (w Weight) Mul(other Weight) Weight {
	return Weight{Fixed: w.Fixed.Mul(other.Fixed)}
}

// Fraction multiples weight by `frac`.
func (w Weight) Fraction(frac *big.Rat) Weight {
	value := w.Fixed.
		Mul(fixed.From(float64(frac.Num().Uint64()))).
		Div(fixed.From(float64(frac.Denom().Uint64())))
	return Weight{Fixed: value}
}

// Cmp returns 1 if weight is positive and larger than other,
// -1 if weight is negative and absolute value is larger than other.
// otherwise it returns 0.
func (w Weight) Cmp(other Weight) int {
	if w.Fixed.Float() > 0 && w.Fixed.GreaterThan(other.Fixed) {
		return 1
	}
	if w.Fixed.Float() < 0 && w.Fixed.Abs().GreaterThan(other.Fixed) {
		return -1
	}
	return 0
}

// Sign of the weight value.
func (w Weight) Sign() int {
	if w.Fixed.Float() > 0 {
		return 1
	}
	if w.Fixed.Float() < 0 {
		return -1
	}
	return 0
}

// String representation of the weight.
func (w Weight) String() string {
	return w.Fixed.String()
}

// EncodeScale implements scale-codec interface.
func (w Weight) EncodeScale(enc *scale.Encoder) (int, error) {
	return scale.EncodeByteArray(enc, w.Fixed.Bytes())
}

// DecodeScale implements scale-codec interface.
func (w *Weight) DecodeScale(dec *scale.Decoder) (int, error) {
	buf := [16]byte{}
	n, err := scale.DecodeByteArray(dec, buf[:])
	if err != nil {
		return n, err
	}
	w.Fixed = fixed.FracFromBytes(buf[:])
	return n, nil
}
