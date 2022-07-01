package sql

import (
	"fmt"
	"strings"
)

func GenerateINPlaceholders(start, count int) string {
	placeholders := make([]string, 0, count)
	for i := start; i < start+count; i++ {
		placeholders = append(placeholders, fmt.Sprintf("?%d", i+1))
	}
	return strings.Join(placeholders, ",")
}
