package tortoise

func globalOpinion(v vec, layerSize int, delta float64) vec {
	threshold := float64(globalThreshold*delta) * float64(layerSize)
	if float64(v[0]) > threshold {
		return support
	} else if float64(v[1]) > threshold {
		return against
	} else {
		return abstain
	}
}
