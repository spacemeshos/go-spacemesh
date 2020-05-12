package config

const (
	// LocalServerEndpoint is the default endpoint for a local influxdb instance
	LocalServerEndpoint = "http://localhost:9999"
)

// ConfigValues specifies  default values for node config params.
var (
	ConfigValues = DefaultConfig()
)

// Config specifies the parameters for telemetry.
type Config struct {
	Bucket       string `mapstructure:"bucket"`
	Endpoint	 string `mapstructure:"endpoint"`
	Organization string `mapstructure:"organization"`
	Token		 string `mapstructure:"token"`
}

// DefaultConfig defines the default tymesync configuration
func DefaultConfig() Config {

	// TimeConfigValues defines default values for all time and ntp related params.
	var ConfigValues = Config{
		Bucket:       "my-bucket",
		Endpoint:	  LocalServerEndpoint,
		Organization: "my-org",
		Token:  	  "my-token",
	}

	return ConfigValues
}
