package repl

import (
	"os"
	"strings"

	"github.com/spacemeshos/go-spacemesh/log"
)

func setVariables(values map[string]string) error {
	for key, value := range values {
		err := os.Setenv(key, value)
		if err != nil {
			return err
		}
	}

	return nil
}

func echo(input string) {
	variables := strings.Split(input, " ")
	for _, v := range variables {
		fields := strings.SplitN(v, "=", 2)
		if len(fields) > 0 {
			var value string
			key := strings.TrimSpace(fields[0])
			if len(fields) > 1 {
				value = strings.TrimSpace(fields[1])
			}

			err := os.Setenv(key, value)
			if err != nil {
				log.Debug(err.Error())
			}
		}
	}
}
