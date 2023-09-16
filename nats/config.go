package nats

import "time"

type Config struct {
	NatsEnabled bool          `mapstructure:"nats-enabled"`
	NatsUrl     string        `mapstructure:"nats-url"`
	NatsMaxAge  time.Duration `mapstructure:"nats-max-age"`
}

func DefaultConfig() Config {
	return Config{
		NatsEnabled: false,
		NatsUrl:     "nats://0.0.0.0:4222",
		NatsMaxAge:  365 * 24 * time.Hour,
	}
}
