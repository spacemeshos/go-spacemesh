package peerexchange

import (
	"math"
	"time"
)

// knownAddress tracks information about a known network address that is used
// to determine how viable an address is.
type knownAddress struct {
	Addr        *addrInfo
	SrcAddr     *addrInfo
	Attempts    int
	LastSeen    time.Time
	LastAttempt time.Time
	LastSuccess time.Time
	tried       bool
	refs        int // reference count of new buckets
}

// Chance returns the selection probability for a known address.  The priority
// depends upon how recently the address has been seen, how recently it was last
// attempted and how often attempts to connect to it have failed.
func (ka *knownAddress) Chance() float64 {
	now := time.Now()
	lastAttempt := now.Sub(ka.LastAttempt)
	if lastAttempt < 0 {
		lastAttempt = 0
	}

	c := 1.0

	// Very recent attempts are less likely to be retried.
	if lastAttempt < 10*time.Minute {
		c *= 0.01
	}

	// deprioritize 66% after each failed attempt, but at most 1/28th to avoid the search taking forever or overly penalizing outages.
	c *= math.Pow(0.66, math.Min(float64(ka.Attempts), 8))

	// TODO : do this without floats ?

	return c
}

// isBad returns true if the address in question has not been tried in the last
// minute and meets one of the following criteria:
// 1) It claims to be from the future
// 2) It hasn't been seen in over a month
// 3) It has failed at least three times and never succeeded
// 4) It has failed ten times in the last week
// All addresses that meet these criteria are assumed to be worthless and not
// worth keeping hold of.
func (ka *knownAddress) isBad() bool {
	if ka.LastAttempt.After(time.Now().Add(-1 * time.Minute)) {
		return false
	}

	// From the future?
	if ka.LastSeen.After(time.Now().Add(10 * time.Minute)) {
		return true
	}

	// Over a month old?
	if ka.LastSeen.Before(time.Now().Add(-1 * numMissingDays * time.Hour * 24)) {
		return true
	}

	// Never succeeded?
	if ka.LastSuccess.IsZero() && ka.Attempts >= numRetries {
		return true
	}

	// Hasn't succeeded in too long?
	if !ka.LastSuccess.After(time.Now().Add(-1*minBadDays*time.Hour*24)) &&
		ka.Attempts >= maxFailures {
		return true
	}

	return false
}
