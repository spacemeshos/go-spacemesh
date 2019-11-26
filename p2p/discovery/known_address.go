package discovery

import (
	"github.com/spacemeshos/go-spacemesh/p2p/node"
	"math"
	"time"
)

// KnownAddress tracks information about a known network address that is used
// to determine how viable an address is.
type KnownAddress struct {
	na          *node.NodeInfo
	srcAddr     *node.NodeInfo
	attempts    int
	lastSeen    time.Time
	lastattempt time.Time
	lastsuccess time.Time
	lastping	time.Time // last successful ping response
	tried       bool
	refs        int // reference count of new buckets
}

func (ka *KnownAddress) DiscNode() *node.NodeInfo {
	return ka.na
}

// LastAttempt returns the last time the known address was attempted.
func (ka *KnownAddress) LastAttempt() time.Time {
	return ka.lastattempt
}

// NeedsPing returns whether we need to ping this node again.
func (ka *KnownAddress) NeedsPing() bool {
	if ka.lastping.Before(time.Now().Add(-1 * pingInterval * time.Hour)) {
		return true
	}
	return false
}

// Mark this address as having just been successfully roundtrip pinged.
func (ka *KnownAddress) updatePing() {
	ka.lastping = time.Now()
}

// chance returns the selection probability for a known address.  The priority
// depends upon how recently the address has been seen, how recently it was last
// attempted and how often attempts to connect to it have failed.
func (ka *KnownAddress) chance() float64 {
	now := time.Now()
	lastAttempt := now.Sub(ka.lastattempt)

	if lastAttempt < 0 {
		lastAttempt = 0
	}

	c := 1.0

	// Very recent attempts are less likely to be retried.
	if lastAttempt < 10*time.Minute {
		c *= 0.01
	}

	// deprioritize 66% after each failed attempt, but at most 1/28th to avoid the search taking forever or overly penalizing outages.
	c *= math.Pow(0.66, math.Min(float64(ka.attempts), 8))

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
func (ka *KnownAddress) isBad() bool {
	if ka.lastattempt.After(time.Now().Add(-1 * time.Minute)) {
		return false
	}

	//// From the future?
	if ka.lastSeen.After(time.Now().Add(10 * time.Minute)) {
		return true
	}

	// Over a month old?
	if ka.lastSeen.Before(time.Now().Add(-1 * numMissingDays * time.Hour * 24)) {
		return true
	}

	// Never succeeded?
	if ka.lastsuccess.IsZero() && ka.attempts >= numRetries {
		return true
	}

	// Hasn't succeeded in too long?
	if !ka.lastsuccess.After(time.Now().Add(-1*minBadDays*time.Hour*24)) &&
		ka.attempts >= maxFailures {
		return true
	}

	return false
}
