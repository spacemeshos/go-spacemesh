package rand

import (
	"fmt"
	"math/rand"
	"sync"
	"time"
)

func init() {
	// initialize global pseudo random generator
	rand.Seed(time.Now().Unix())
}

/*
 * Top-level convenience functions
 */

var (
	globalSource = lockedSource{src: rand.NewSource(time.Now().UnixNano()).(rand.Source64)}
	randMu       sync.Mutex // rand.Read should not be called concurrently
	globalRand   = rand.New(&globalSource)
	letterRunes  = []rune("abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ")
)

// Seed uses the provided seed value to initialize the default Source to a
// deterministic state. If Seed is not called, the generator behaves as
// if seeded by Seed(1). Seed values that have the same remainder when
// divided by 2^31-1 generate the same pseudo-random sequence.
// Seed, unlike the Rand.Seed method, is safe for concurrent use.
func Seed(seed int64) {
	randMu.Lock()
	defer randMu.Unlock()

	globalRand.Seed(seed)
}

// Int63 returns a non-negative pseudo-random 63-bit integer as an int64
// from the default Source.
func Int63() int64 {
	randMu.Lock()
	defer randMu.Unlock()

	return globalRand.Int63()
}

// Uint32 returns a pseudo-random 32-bit value as a uint32
// from the default Source.
func Uint32() uint32 {
	randMu.Lock()
	defer randMu.Unlock()

	return globalRand.Uint32()
}

// Uint64 returns a pseudo-random 64-bit value as a uint64
// from the default Source.
func Uint64() uint64 {
	randMu.Lock()
	defer randMu.Unlock()

	return globalRand.Uint64()
}

// Int31 returns a non-negative pseudo-random 31-bit integer as an int32
// from the default Source.
func Int31() int32 {
	randMu.Lock()
	defer randMu.Unlock()

	return globalRand.Int31()
}

// Int returns a non-negative pseudo-random int from the default Source.
func Int() int {
	randMu.Lock()
	defer randMu.Unlock()

	return globalRand.Int()
}

// Int63n returns, as an int64, a non-negative pseudo-random number in [0,n)
// from the default Source.
// It panics if n <= 0.
func Int63n(n int64) int64 {
	randMu.Lock()
	defer randMu.Unlock()

	return globalRand.Int63n(n)
}

// Int31n returns, as an int32, a non-negative pseudo-random number in [0,n)
// from the default Source.
// It panics if n <= 0.
func Int31n(n int32) int32 {
	randMu.Lock()
	defer randMu.Unlock()

	return globalRand.Int31n(n)
}

// Intn returns, as an int, a non-negative pseudo-random number in [0,n)
// from the default Source.
// It panics if n <= 0.
func Intn(n int) int {
	randMu.Lock()
	defer randMu.Unlock()

	return globalRand.Intn(n)
}

// Float64 returns, as a float64, a pseudo-random number in [0.0,1.0)
// from the default Source.
func Float64() float64 {
	randMu.Lock()
	defer randMu.Unlock()

	return globalRand.Float64()
}

// Float32 returns, as a float32, a pseudo-random number in [0.0,1.0)
// from the default Source.
func Float32() float32 {
	randMu.Lock()
	defer randMu.Unlock()

	return globalRand.Float32()
}

// Perm returns, as a slice of n ints, a pseudo-random permutation of the integers [0,n)
// from the default Source.
func Perm(n int) []int {
	randMu.Lock()
	defer randMu.Unlock()

	return globalRand.Perm(n)
}

// Shuffle pseudo-randomizes the order of elements using the default Source.
// Parameter n is the number of elements. Shuffle panics if n < 0.
// Parameter swap swaps the elements with indexes i and j.
func Shuffle(n int, swap func(i, j int)) {
	randMu.Lock()
	defer randMu.Unlock()
	globalRand.Shuffle(n, swap)
}

// Read generates len(p) random bytes from the default Source and
// writes them into p. It always returns len(p) and a nil error.
// Read, unlike the Rand.Read method, is safe for concurrent use.
func Read(p []byte) (n int, err error) {
	randMu.Lock()
	defer randMu.Unlock()

	n, err = globalRand.Read(p)
	if err != nil {
		return n, fmt.Errorf("read: %w", err)
	}

	return n, nil
}

// NormFloat64 returns a normally distributed float64 in the range
// [-math.MaxFloat64, +math.MaxFloat64] with
// standard normal distribution (mean = 0, stddev = 1)
// from the default Source.
// To produce a different normal distribution, callers can
// adjust the output using:
//
//	sample = NormFloat64() * desiredStdDev + desiredMean
func NormFloat64() float64 {
	randMu.Lock()
	defer randMu.Unlock()

	return globalRand.NormFloat64()
}

// ExpFloat64 returns an exponentially distributed float64 in the range
// (0, +math.MaxFloat64] with an exponential distribution whose rate parameter
// (lambda) is 1 and whose mean is 1/lambda (1) from the default Source.
// To produce a distribution with a different rate parameter,
// callers can adjust the output using:
//
//	sample = ExpFloat64() / desiredRateParameter
func ExpFloat64() float64 {
	randMu.Lock()
	defer randMu.Unlock()

	return globalRand.ExpFloat64()
}

// String returns an n sized random string.
func String(n int) string {
	b := make([]rune, n)
	for i := range b {
		b[i] = letterRunes[rand.Intn(len(letterRunes))]
	}
	return string(b)
}

type lockedSource struct {
	lock sync.Mutex
	src  rand.Source64
}

func (ls *lockedSource) Int63() int64 {
	ls.lock.Lock()
	defer ls.lock.Unlock()
	return ls.src.Int63()
}

func (ls *lockedSource) Uint64() uint64 {
	ls.lock.Lock()
	defer ls.lock.Unlock()
	return ls.src.Uint64()
}

func (ls *lockedSource) Seed(seed int64) {
	ls.lock.Lock()
	defer ls.lock.Unlock()
	ls.src.Seed(seed)
}
