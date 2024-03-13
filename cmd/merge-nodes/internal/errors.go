package internal

import (
	"errors"
)

var (
	ErrSupervisedNode = errors.New("merging of supervised smeshing nodes is not supported")
	ErrInvalidSchema  = errors.New("database has an invalid schema version")
)
