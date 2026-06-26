package app

import (
	"bytes"
	"embed"
	"template/helpers"

	"github.com/spf13/viper"
)

// Config holds the application configuration.
type Config struct {
	Server   ServerConfig   `mapstructure:"server"`
	Database DatabaseConfig `mapstructure:"database"`
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

func LoadConfig(efs embed.FS) (*Config, error) {
	var config Config
	v := viper.New()
	v.SetConfigName("config")
	v.SetConfigType("yaml")

	data := helpers.Must(efs.ReadFile("config.yml"))
	helpers.Check(v.ReadConfig(bytes.NewReader(data)), "failed to parse config")
	helpers.Check(v.Unmarshal(&config), "failed to unmarshal config")

	return &config, nil
}
