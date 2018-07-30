package version

import (
	"fmt"
	"strconv"
	"strings"
)

// CheckNodeVersion checks if a request version is more recent then the given min version. returns a bool and an error
func CheckNodeVersion(reqVersion string, minVersion string) (bool, error) {
	// TODO : semantic versioning comparison is a pain, refine this or use a lib

	// if same version string don't do anything
	if reqVersion == minVersion {
		return true, nil
	}

	// take each version number and convert to ints
	reqsplit, minsplit := strings.Split(reqVersion, "."), strings.Split(minVersion, ".")
	if len(reqsplit) != 3 || len(minsplit) != 3 {
		return false, fmt.Errorf("one of provided versions isn't valid %v / %v", reqVersion, minVersion)
	}
	numa, numb := [3]int64{}, [3]int64{}
	for i := 0; i < 3; i++ {
		var err error
		numa[i], err = strconv.ParseInt(reqsplit[i], 10, 8)
		if err != nil {
			return false, fmt.Errorf(" could not read version number %v", reqVersion)
		}
		numb[i], err = strconv.ParseInt(minsplit[i], 10, 8)
		if err != nil {
			return false, fmt.Errorf(" could not read version number %v", reqVersion)
		}
	}

	for i := 0; i < 2; i++ {
		if numa[i] > numb[i] {
			return true, nil
		}

		if numa[i] != numb[i] {
			return false, nil
		}
	}

	return false, nil
}
