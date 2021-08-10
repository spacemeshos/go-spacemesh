package types

import (
	"errors"
	"math/big"
)

// RatVar is a wrapper for big.Rat to use it with the pflag package.
type RatVar big.Rat

// String returns a string representation of big.Rat.
func (r *RatVar) String() string {
	return (*big.Rat)(r).String()
}

// Set sets the value of big.Rat to a string.
func (r *RatVar) Set(s string) error {
	if _, ok := (*big.Rat)(r).SetString(s); !ok {
		return errors.New("malformed string provided")
	}

	return nil
}

// Type returns *big.Rat type.
func (r *RatVar) Type() string {
	return "*big.Rat"
}
