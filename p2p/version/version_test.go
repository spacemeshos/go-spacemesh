package version

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestCheckNodeVersion(t *testing.T) {
	//t.Skip("We should implement or import deep semantic version comparison")
	type versionTest []struct {
		version    string
		minVersion string
	}

	testNewClient := versionTest{
		{"someclient/0.0.1", "0.0.1"},
		{"anotherclient/1.0.0", "0.0.1"},
		{"someclient/0.2.1", "0.0.1"},
		{"someclient/1.3.1", "1.0.1"},
		{"someclient/3.0.0", "0.5.3"},
		{"someclient/0.2.0", "0.0.9"},
		{"someclient/5.5.1", "5.0.0"},
	}

	testNotValid := versionTest{
		{"verybadclient/x.d.1", "0.0.1"},
		{"ver/ybadclient/0.0.1", "0.0.1"},
		{"verybadclient/x.2.1", "0.0.1"},
		{"verybadclient/h.d.a", "0.0.1"},
		//{"verybadclient/2.c2.4", "0.0.1" },
		{"verybadclient/x.d.؆", "0.0.1"},
		{"verybadclient/ੳ.ڪ.1", "0.0.1"},
		{"verybadclient/ੳ.ڪ.؆", "0.0.1"},
	}

	testTooOld := versionTest{
		{"oldclient/1.1.1", "2.0.0"},
		{"oldclient/1.1.1", "3.1.1"},
		{"oldclient/1.2.1", "1.2.4"},
		{"oldclient/0.3.1", "1.0.0"},
		{"oldclient/4.4.1", "5.0.0"},
		{"oldclient/1.0.5", "1.1.0"},
		{"oldclient/1.0.9", "1.1.0"},
	}

	getSemver := func(s string) string {
		splitted := strings.Split(s, "/")
		if len(splitted) != 2 {
			return ""
		}
		return splitted[1]
	}

	for _, te := range testNewClient {
		ok, err := CheckNodeVersion(getSemver(te.version), te.minVersion)
		assert.NoError(t, err, "Should'nt return error")
		assert.True(t, ok, "Should return true on same or higher version")
	}

	for _, te := range testNotValid {
		ok, err := CheckNodeVersion(getSemver(te.version), te.minVersion)
		assert.Error(t, err, "not valid version should error")
		assert.False(t, ok, "should return false on non valid version")
	}

	for _, te := range testTooOld {
		ok, err := CheckNodeVersion(getSemver(te.version), te.minVersion)
		assert.False(t, ok, "Should return false when version is older than min version")
		assert.NoError(t, err, "Shuold'nt return error when client is older")
	}

}
