package config

import (
	"github.com/spf13/viper"
)

type Config struct {
	DeepSeekAPIKey string `mapstructure:"DEEPSEEK_API_KEY"`
	DeepSeekAPIURL string `mapstructure:"DEEPSEEK_API_URL"`
	DeepSeekModel  string `mapstructure:"DEEPSEEK_MODEL"`
}

func LoadConfig() (*Config, error) {
	viper.SetConfigFile(".env")
	viper.AutomaticEnv()

	if err := viper.ReadInConfig(); err != nil {
		return nil, err
	}

	var config Config
	if err := viper.Unmarshal(&config); err != nil {
		return nil, err
	}

	return &config, nil
}