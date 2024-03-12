package internal

import (
	"errors"
	"fmt"
)

var ErrSupervisedNode = errors.New("merging of supervised smeshing nodes is not supported")

type ErrInvalidSchemaVersion struct {
	Expected int
	Actual   int
}

func (e ErrInvalidSchemaVersion) Error() string {
	return fmt.Sprintf("invalid schema version: expected %d got %d", e.Expected, e.Actual)
}
