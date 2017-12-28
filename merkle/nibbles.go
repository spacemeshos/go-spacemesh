package merkle

import "errors"

// Convert a hex char to its binary value
// s: must be in range "0",....."f"
func charToHex(s string) (byte, error) {
	switch s {
	case "0":
		return 0, nil
	case "1":
		return 1, nil
	case "2":
		return 2, nil
	case "3":
		return 3, nil
	case "4":
		return 4, nil
	case "5":
		return 5, nil
	case "6":
		return 6, nil
	case "7":
		return 7, nil
	case "8":
		return 8, nil
	case "9":
		return 9, nil
	case "a":
		return 10, nil
	case "b":
		return 11, nil
	case "c":
		return 12, nil
	case "d":
		return 13, nil
	case "e":
		return 14, nil
	case "f":
		return 15, nil
	}

	return 0, errors.New("input out of range. Expected 1 hex char string")
}
