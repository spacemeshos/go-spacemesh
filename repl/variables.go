package repl

import (
	"bufio"
	"fmt"
	"os"
	"strings"
)

func setVariables(path string) error {
	f, err := os.Open(path)

	if err != nil {
		return err
	} else {
		defer f.Close()

		scanner := bufio.NewScanner(f)
		for scanner.Scan() {
			line := scanner.Text()
			key, value := parseLine(line)
			if key == "" {
				continue
			}

			err := os.Setenv(key, value)
			if err != nil {
				return err
			}

		}
	}

	return nil
}

func parseLine(line string) (key, value string) {
	line = strings.TrimSpace(line)
	fields := strings.SplitN(line, "=", 2)
	if len(fields) > 0 {
		key = strings.TrimSpace(fields[0])
		if len(fields) > 1 {
			value = strings.TrimSpace(fields[1])
		}
	}
	return key, value
}

func echo(key string) {
	fmt.Println(os.Getenv(key))
}
