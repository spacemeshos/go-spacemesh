package config

// Config is the configuration of the Hare.
type Config struct {
	N int `mapstructure:"hare-committee-size"`  // total number of active parties

	/* number of dishonest parties or percent of total active nodes weight if weighted */
	F int `mapstructure:"hare-max-adversaries"`

	RoundDuration   int `mapstructure:"hare-round-duration-sec"` // the duration of a single round
	WakeupDelta     int `mapstructure:"hare-wakeup-delta"`       // the wakeup delta after tick
	ExpectedLeaders int `mapstructure:"hare-exp-leaders"`        // the expected number of leaders
	SuperHare       bool
	LimitIterations int  `mapstructure:"hare-limit-iterations"` // limit on number of iterations
	LimitConcurrent int  `mapstructure:"hare-limit-concurrent"` // limit number of concurrent CPs
	Weighted        bool `mapstructure:"weighted"`              // run weighted consensus
}

// DefaultConfig returns the default configuration for the hare.
func DefaultConfig() Config {
	return Config{10, 5, 2, 10, 5, false, 1000, 5, false}
}
