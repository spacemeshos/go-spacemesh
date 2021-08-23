package config

// Config is the configuration of the Hare.
type Config struct {
	N               int `mapstructure:"hare-committee-size"`     // total number of active parties
	F               int `mapstructure:"hare-max-adversaries"`    // number of dishonest parties
	RoundDuration   int `mapstructure:"hare-round-duration-sec"` // the duration of a single round
	WakeupDelta     int `mapstructure:"hare-wakeup-delta"`       // the wakeup delta after tick
	ExpectedLeaders int `mapstructure:"hare-exp-leaders"`        // the expected number of leaders
	SuperHare       bool
	LimitIterations int `mapstructure:"hare-limit-iterations"` // limit on number of iterations
	LimitConcurrent int `mapstructure:"hare-limit-concurrent"` // limit number of concurrent CPs
}

// DefaultConfig returns the default configuration for the hare.
func DefaultConfig() Config {
	return Config{
		N:               10,
		F:               5,
		RoundDuration:   10,
		WakeupDelta:     10,
		ExpectedLeaders: 5,
		LimitIterations: 50,
		LimitConcurrent: 5,
	}
}
