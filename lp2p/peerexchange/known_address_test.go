package peerexchange

import (
	"math"
	"testing"
	"time"
)

func TstKnownAddressIsBad(ka *knownAddress) bool {
	return ka.isBad()
}

func TstKnownAddressChance(ka *knownAddress) float64 {
	return ka.Chance()
}

func tstNewKnownAddress(lastSeen time.Time, attempts int,
	lastattempt, lastsuccess time.Time, tried bool, refs int) *knownAddress {
	return &knownAddress{
		LastSeen:    lastSeen,
		Attempts:    attempts,
		LastAttempt: lastattempt,
		LastSuccess: lastsuccess,
		tried:       tried,
		refs:        refs,
	}
}

func TestChance(t *testing.T) {
	now := time.Unix(time.Now().Unix(), 0)
	var tests = []struct {
		addr     *knownAddress
		expected float64
	}{
		{
			//Test normal case
			tstNewKnownAddress(now.Add(-35*time.Second),
				0, time.Now().Add(-30*time.Minute), time.Now(), false, 0),
			1.0,
		}, {
			//Test case in which lastseen < 0
			tstNewKnownAddress(now.Add(20*time.Second),
				0, time.Now().Add(-30*time.Minute), time.Now(), false, 0),
			1.0,
		}, {
			//Test case in which lastattempt < 0
			tstNewKnownAddress(now.Add(-35*time.Second),
				0, time.Now().Add(30*time.Minute), time.Now(), false, 0),
			1.0 * .01,
		}, {
			//Test case in which lastattempt < ten minutes
			tstNewKnownAddress(now.Add(-35*time.Second),
				0, time.Now().Add(-5*time.Minute), time.Now(), false, 0),
			1.0 * .01,
		}, {
			//Test case with several failed attempts.
			tstNewKnownAddress(now.Add(-35*time.Second),
				2, time.Now().Add(-30*time.Minute), time.Now(), false, 0),
			1 * math.Pow(0.66, 2), // 2 attemps
		},
	}

	err := .0001
	for i, test := range tests {
		chance := TstKnownAddressChance(test.addr)
		if math.Abs(test.expected-chance) >= err {
			t.Errorf("case %d: got %f, expected %f", i, chance, test.expected)
		}
	}
}

func TestIsBad(t *testing.T) {
	now := time.Unix(time.Now().Unix(), 0)
	future := now.Add(35 * time.Minute)
	monthOld := now.Add(-43 * time.Hour * 24)
	secondsOld := now.Add(-2 * time.Second)
	minutesOld := now.Add(-27 * time.Minute)
	hoursOld := now.Add(-5 * time.Hour)
	zeroTime := time.Time{}

	futureNa := future
	minutesOldNa := minutesOld
	monthOldNa := monthOld
	currentNa := secondsOld

	//Test addresses that have been tried in the last minute.
	if TstKnownAddressIsBad(tstNewKnownAddress(futureNa, 3, secondsOld, zeroTime, false, 0)) {
		t.Errorf("test case 1: addresses that have been tried in the last minute are not bad.")
	}
	if TstKnownAddressIsBad(tstNewKnownAddress(monthOldNa, 3, secondsOld, zeroTime, false, 0)) {
		t.Errorf("test case 2: addresses that have been tried in the last minute are not bad.")
	}
	if TstKnownAddressIsBad(tstNewKnownAddress(currentNa, 3, secondsOld, zeroTime, false, 0)) {
		t.Errorf("test case 3: addresses that have been tried in the last minute are not bad.")
	}
	if TstKnownAddressIsBad(tstNewKnownAddress(currentNa, 3, secondsOld, monthOld, true, 0)) {
		t.Errorf("test case 4: addresses that have been tried in the last minute are not bad.")
	}
	if TstKnownAddressIsBad(tstNewKnownAddress(currentNa, 2, secondsOld, secondsOld, true, 0)) {
		t.Errorf("test case 5: addresses that have been tried in the last minute are not bad.")
	}

	//Test address that claims to be from the future.
	if !TstKnownAddressIsBad(tstNewKnownAddress(futureNa, 0, minutesOld, hoursOld, true, 0)) {
		t.Errorf("test case 6: addresses that claim to be from the future are bad.")
	}

	//Test address that has not been seen in over a month.
	if !TstKnownAddressIsBad(tstNewKnownAddress(monthOldNa, 0, minutesOld, hoursOld, true, 0)) {
		t.Errorf("test case 7: addresses more than a month old are bad.")
	}

	//It has failed at least three times and never succeeded.
	if !TstKnownAddressIsBad(tstNewKnownAddress(minutesOldNa, 3, minutesOld, zeroTime, true, 0)) {
		t.Errorf("test case 8: addresses that have never succeeded are bad.")
	}

	//It has failed ten times in the last week
	if !TstKnownAddressIsBad(tstNewKnownAddress(minutesOldNa, 10, minutesOld, monthOld, true, 0)) {
		t.Errorf("test case 9: addresses that have not succeeded in too long are bad.")
	}

	//Test an address that should work.
	if TstKnownAddressIsBad(tstNewKnownAddress(minutesOldNa, 2, minutesOld, hoursOld, true, 0)) {
		t.Errorf("test case 10: This should be a valid address.")
	}
}
