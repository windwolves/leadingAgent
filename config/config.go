package config

import (
	"github.com/spf13/viper"
)

// SpeechConfig 语音识别配置。
type SpeechConfig struct {
	TOS struct {
		Endpoint  string `mapstructure:"SPEECH_TOS_ENDPOINT"`
		Region    string `mapstructure:"SPEECH_TOS_REGION"`
		AccessKey string `mapstructure:"SPEECH_TOS_ACCESS_KEY"`
		SecretKey string `mapstructure:"SPEECH_TOS_SECRET_KEY"`
		Bucket    string `mapstructure:"SPEECH_TOS_BUCKET"`
	} `mapstructure:"speech_tos"`
	ASR struct {
		AppKey       string `mapstructure:"SPEECH_ASR_APP_KEY"`
		ResourceID   string `mapstructure:"SPEECH_ASR_RESOURCE_ID"`
		PollInterval int    `mapstructure:"SPEECH_ASR_POLL_INTERVAL"`
		PollTimeout  int    `mapstructure:"SPEECH_ASR_POLL_TIMEOUT"`
	} `mapstructure:"speech_asr"`
}

// DefaultSpeechConfig 返回带默认值的 SpeechConfig。
func DefaultSpeechConfig() SpeechConfig {
	cfg := SpeechConfig{}
	cfg.TOS.Endpoint = "https://tos-cn-beijing.volces.com"
	cfg.TOS.Region = "cn-beijing"
	cfg.TOS.Bucket = "speech-audio-temp"
	cfg.ASR.ResourceID = "volc.seedasr.auc"
	cfg.ASR.PollInterval = 2
	cfg.ASR.PollTimeout = 300
	return cfg
}

type Config struct {
	DeepSeekAPIKey          string `mapstructure:"DEEPSEEK_API_KEY"`
	DeepSeekAPIURL          string `mapstructure:"DEEPSEEK_API_URL"`
	DeepSeekModel           string `mapstructure:"DEEPSEEK_MODEL"`
	DeepSeekMaxTokens       int    `mapstructure:"DEEPSEEK_MAX_TOKENS"`
	DeepSeekReasoningEffort string `mapstructure:"DEEPSEEK_REASONING_EFFORT"`

	DoubaoAPIKey string `mapstructure:"DOUBAO_API_KEY"`
	DoubaoAPIURL string `mapstructure:"DOUBAO_API_URL"`
	DoubaoModel  string `mapstructure:"DOUBAO_MODEL"`

	Speech SpeechConfig `mapstructure:"speech"`
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

	defaults := DefaultSpeechConfig()
	if config.Speech.TOS.Endpoint == "" {
		config.Speech.TOS.Endpoint = defaults.TOS.Endpoint
	}
	if config.Speech.TOS.Region == "" {
		config.Speech.TOS.Region = defaults.TOS.Region
	}
	if config.Speech.TOS.Bucket == "" {
		config.Speech.TOS.Bucket = defaults.TOS.Bucket
	}
	if config.Speech.ASR.ResourceID == "" {
		config.Speech.ASR.ResourceID = defaults.ASR.ResourceID
	}
	if config.Speech.ASR.PollInterval == 0 {
		config.Speech.ASR.PollInterval = defaults.ASR.PollInterval
	}
	if config.Speech.ASR.PollTimeout == 0 {
		config.Speech.ASR.PollTimeout = defaults.ASR.PollTimeout
	}

	return &config, nil
}
