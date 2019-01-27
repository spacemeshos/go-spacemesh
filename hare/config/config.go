package config

import "time"

type Config struct {
	N             int           // total number of active parties
	F             int           // number of dishonest parties
	SetSize       int           // max size of set in a consensus
	RoundDuration time.Duration // the duration of a single round
}

func DefaultConfig() Config {
	return Config{2, 1, 2, 1 *time.Second}
}
