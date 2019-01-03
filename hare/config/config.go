package config

import "time"

type Config struct {
	N             int           // total number of active parties
	F             int           // number of dishonest parties
	SetSize       int           // max size of set in a consensus
	RoundDuration time.Duration // the duration of a single round
}

func DefaultConfig() Config {
	return Config{800, 400, 200, time.Second * time.Duration(15)}
}
