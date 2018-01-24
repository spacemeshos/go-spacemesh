package crypto

// MinInt32 returns x if x < y and y otherwise.
func MinInt32(x, y int32) int32 {
	if x < y {
		return x
	}
	return y
}

// MinInt returns x if x < y and y otherwise.
func MinInt(x, y int) int {
	if x < y {
		return x
	}
	return y
}

// MinInt64 returns x if x < y and y otherwise.
func MinInt64(x, y int64) int64 {
	if x < y {
		return x
	}
	return y
}
