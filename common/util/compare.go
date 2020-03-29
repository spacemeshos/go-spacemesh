package util

// Min returns the smaller of the two inputs, both of type int.
func Min(a, b int) int {
	if a < b {
		return a
	}
	return b
}

// Min32 returns the smaller of the two inputs, both of type uint32.
func Min32(a, b uint32) uint32 {
	if a < b {
		return a
	}
	return b
}

// Min64 returns the smaller of the two inputs, both of type uint64.
func Min64(a, b uint64) uint64 {
	if a < b {
		return a
	}
	return b
}
