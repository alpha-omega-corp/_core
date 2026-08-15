package app

import (
	"bytes"
	"embed"
	"os"

	"github.com/alpha-omega-corp/_core/helpers"
	"github.com/spf13/viper"
)

// Config holds the application configuration.
type Config struct {
	Server   ServerConfig   `mapstructure:"server"`
	Database DatabaseConfig `mapstructure:"database"`
	Stripe   StripeConfig   `mapstructure:"stripe"`
	Debug    bool           `mapstructure:"debug"`
}

type ServerConfig struct {
	Host string `mapstructure:"host"`
	Port int    `mapstructure:"port"`
}

type DatabaseConfig struct {
	Url      string `mapstructure:"url"`
	Port     int    `mapstructure:"port"`
	Name     string `mapstructure:"name"`
	Username string `mapstructure:"username"`
	Password string `mapstructure:"password"`
}

// StripeConfig is everything a consumer needs to talk to Stripe. Only
// SecretKey is required: a service that leaves it empty is expected to keep
// serving whatever does not involve money.
type StripeConfig struct {
	SecretKey     string `mapstructure:"secret_key"`
	WebhookSecret string `mapstructure:"webhook_secret"`
	Currency      string `mapstructure:"currency"`

	// ApiBase points the client somewhere other than Stripe — `stripe-mock`,
	// typically, so a checkout can be exercised without a live account. Leave
	// it empty in production.
	ApiBase string `mapstructure:"api_base"`
}

// ConfigPathEnv names a config file on disk that is read instead of the
// embedded one.
//
// The embedded copy is what a binary is built with and stays the default, so
// nothing that does not set this variable changes behaviour. It is set by a
// container, where the config has to live on the host: baking it into the
// image would put the Stripe secret key in a layer, and would mean rebuilding
// to change a port.
const ConfigPathEnv = "CONFIG_PATH"

func LoadConfig(efs embed.FS) (*Config, error) {
	var config Config
	v := viper.New()
	v.SetConfigName("config")
	v.SetConfigType("yml")

	// Failure to read an explicitly-named file panics rather than falling back
	// to the embedded copy: a container that was pointed at a config and did
	// not get one is misconfigured, and quietly serving the build-time
	// defaults — another database, another Stripe account — is the worse
	// outcome of the two.
	data := helpers.Must(efs.ReadFile("config.yml"))
	if path := os.Getenv(ConfigPathEnv); path != "" {
		data = helpers.Must(os.ReadFile(path))
	}

	helpers.Check(v.ReadConfig(bytes.NewReader(data)), "failed to parse config")
	helpers.Check(v.Unmarshal(&config), "failed to unmarshal config")

	return &config, nil
}
