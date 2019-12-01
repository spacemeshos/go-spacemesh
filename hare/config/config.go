package config

// Config is the configuration of the Hare.
type Config struct {
	N               int `mapstructure:"hare-committee-size"`     // total number of active parties
	F               int `mapstructure:"hare-max-adversaries"`    // number of dishonest parties
	RoundDuration   int `mapstructure:"hare-round-duration-sec"` // the duration of a single round
	WakeupDelta     int `mapstructure:"hare-wakeup-delta"`       // the wakeup delta after tick
	ExpectedLeaders int `mapstructure:"hare-exp-leaders"`        // the expected number of leaders
	LimitIterations int `mapstructure:"hare-limit-iterations"`   // limit on number of iterations
	LimitConcurrent int `mapstructure:"hare-limit-concurrent"`   // limit number of concurrent CPs
}

// DefaultConfig returns the default configuration for the hare.
func DefaultConfig() Config {
	return Config{10, 5, 2, 10, 5, 1000, 5}
}
