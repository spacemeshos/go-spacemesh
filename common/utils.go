package common

func Min(a, b int) int {
	if a < b {
		return a
	}
	return b
}

func Min32(a, b uint32) uint32 {
	if a < b {
		return a
	}
	return b
}

func Min64(a, b uint64) uint64 {
	if a < b {
		return a
	}
	return b
}
