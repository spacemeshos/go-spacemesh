package core

import "errors"

var (
	// ErrInternal raised on any unexpected error due to internal conditions.
	// Most likely due to the disk failures.
	ErrInternal = errors.New("internal")
	// ErrMalformed raised if transaction cannot be decoded properly.
	ErrMalformed = errors.New("malformed tx")
	// ErrInvalidNonce raised due to the expected nonce mismatch.
	ErrInvalidNonce = errors.New("invalid nonce")
	// ErrNoBalance raised if transaction run out of balance during execution.
	ErrNoBalance = errors.New("no balance")
	// ErrMaxGas raised if tx consumed over MaxGas value.
	ErrMaxGas = errors.New("max gas")
	// ErrMaxSpend raised if tx transfered over MaxSpend value.
	ErrMaxSpend = errors.New("max spend")
	// ErrSpawn raised to block regular spawn.
	ErrSpawn = errors.New("spawn is not supported")
)
