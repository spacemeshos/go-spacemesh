package types

import (
	"errors"
	"math/big"
)

type RatVar big.Rat

func (r *RatVar) String() string {
	return (*big.Rat)(r).String()
}

func (r *RatVar) Set(s string) error {
	if _, ok := (*big.Rat)(r).SetString(s); !ok {
		return errors.New("malformed string provided")
	}

	return nil
}

func (r *RatVar) Type() string {
	return "*big.Rat"
}
