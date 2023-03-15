package util

import "golang.org/x/exp/constraints"

// Min returns the smaller of the two inputs, both of type int.
func Min[T constraints.Ordered](a, b T) T {
	if a < b {
		return a
	}
	return b
}
