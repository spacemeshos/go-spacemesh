package discovery

import (
	"math"
	"testing"
	"time"
)

func TstKnownAddressIsBad(ka *KnownAddress) bool {
	return ka.isBad()
}

func TstKnownAddressChance(ka *KnownAddress) float64 {
	return ka.chance()
}

func TstKnownAddressNeedsPing(ka *KnownAddress) bool {
	return ka.NeedsPing()
}

func TstNewKnownAddress(lastSeen time.Time, attempts int,
	lastattempt, lastsuccess time.Time, lastping time.Time, tried bool, refs int) *KnownAddress {
	return &KnownAddress{lastSeen: lastSeen, attempts: attempts, lastattempt: lastattempt,
		lastsuccess: lastsuccess, lastping: lastping, tried: tried, refs: refs}
}

func TestChance(t *testing.T) {
	now := time.Unix(time.Now().Unix(), 0)
	var tests = []struct {
		addr     *KnownAddress
		expected float64
	}{
		{
			//Test normal case
			TstNewKnownAddress(now.Add(-35*time.Second),
				0, time.Now().Add(-30*time.Minute), time.Now(), time.Now(), false, 0),
			1.0,
		}, {
			//Test case in which lastseen < 0
			TstNewKnownAddress(now.Add(20*time.Second),
				0, time.Now().Add(-30*time.Minute), time.Now(), time.Now(), false, 0),
			1.0,
		}, {
			//Test case in which lastattempt < 0
			TstNewKnownAddress(now.Add(-35*time.Second),
				0, time.Now().Add(30*time.Minute), time.Now(), time.Now(), false, 0),
			1.0 * .01,
		}, {
			//Test case in which lastattempt < ten minutes
			TstNewKnownAddress(now.Add(-35*time.Second),
				0, time.Now().Add(-5*time.Minute), time.Now(), time.Now(), false, 0),
			1.0 * .01,
		}, {
			//Test case with several failed attempts.
			TstNewKnownAddress(now.Add(-35*time.Second),
				2, time.Now().Add(-30*time.Minute), time.Now(), time.Now(), false, 0),
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
	if TstKnownAddressIsBad(TstNewKnownAddress(futureNa, 3, secondsOld, zeroTime, time.Now(), false, 0)) {
		t.Errorf("test case 1: addresses that have been tried in the last minute are not bad.")
	}
	if TstKnownAddressIsBad(TstNewKnownAddress(monthOldNa, 3, secondsOld, zeroTime, time.Now(), false, 0)) {
		t.Errorf("test case 2: addresses that have been tried in the last minute are not bad.")
	}
	if TstKnownAddressIsBad(TstNewKnownAddress(currentNa, 3, secondsOld, zeroTime, time.Now(), false, 0)) {
		t.Errorf("test case 3: addresses that have been tried in the last minute are not bad.")
	}
	if TstKnownAddressIsBad(TstNewKnownAddress(currentNa, 3, secondsOld, monthOld, time.Now(), true, 0)) {
		t.Errorf("test case 4: addresses that have been tried in the last minute are not bad.")
	}
	if TstKnownAddressIsBad(TstNewKnownAddress(currentNa, 2, secondsOld, secondsOld, time.Now(), true, 0)) {
		t.Errorf("test case 5: addresses that have been tried in the last minute are not bad.")
	}

	//Test address that claims to be from the future.
	if !TstKnownAddressIsBad(TstNewKnownAddress(futureNa, 0, minutesOld, hoursOld, time.Now(), true, 0)) {
		t.Errorf("test case 6: addresses that claim to be from the future are bad.")
	}

	//Test address that has not been seen in over a month.
	if !TstKnownAddressIsBad(TstNewKnownAddress(monthOldNa, 0, minutesOld, hoursOld, time.Now(), true, 0)) {
		t.Errorf("test case 7: addresses more than a month old are bad.")
	}

	//It has failed at least three times and never succeeded.
	if !TstKnownAddressIsBad(TstNewKnownAddress(minutesOldNa, 3, minutesOld, zeroTime, time.Now(), true, 0)) {
		t.Errorf("test case 8: addresses that have never succeeded are bad.")
	}

	//It has failed ten times in the last week
	if !TstKnownAddressIsBad(TstNewKnownAddress(minutesOldNa, 10, minutesOld, monthOld, time.Now(), true, 0)) {
		t.Errorf("test case 9: addresses that have not succeeded in too long are bad.")
	}

	//Test an address that should work.
	if TstKnownAddressIsBad(TstNewKnownAddress(minutesOldNa, 2, minutesOld, hoursOld, time.Now(), true, 0)) {
		t.Errorf("test case 10: This should be a valid address.")
	}
}

func TestNeedsPing(t *testing.T) {
	justnow := time.Now()
	never := time.Unix(0, 0)
	threshold := time.Now().Add(-1 * pingInterval * time.Hour)

	// Test an address that was just pinged.
	if TstKnownAddressNeedsPing(TstNewKnownAddress(time.Now(),
		0, time.Now(), time.Now(), justnow, false, 0)) {
		t.Errorf("test case 1: recently-pinged address should not need ping.")
	}
	// Test an address that was never pinged.
	if !TstKnownAddressNeedsPing(TstNewKnownAddress(time.Now(),
		0, time.Now(), time.Now(), never, false, 0)) {
		t.Errorf("test case 2: never-pinged address should need ping.")
	}

	// CORNER CASES
	// Test an address pinged just before the threshold.
	if !TstKnownAddressNeedsPing(TstNewKnownAddress(time.Now(),
		0, time.Now(), time.Now(), threshold.Add(-1), false, 0)) {
		t.Errorf("test case 3: address pinged just before threshold should need ping.")
	}

	// Test an address pinged just after the threshold. NOTE: a few ms will elapse between when we construct this test
	// and when it actually runs, so add a minute here to be safe.
	if TstKnownAddressNeedsPing(TstNewKnownAddress(time.Now(),
		0, time.Now(), time.Now(), threshold.Add(1*time.Minute), false, 0)) {
		t.Errorf("test case 4: address pinged just after threshold should not need ping.")
	}
}
