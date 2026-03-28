package config

import (
	"os"
	"time"

	"gopkg.in/yaml.v3"
)

type Config struct {
	Server ServerConfig `yaml:"server"`
	AWS    AWSConfig    `yaml:"aws"`
	S3     S3Config     `yaml:"s3"`
	Models []ModelConfig `yaml:"models"`
}

type ServerConfig struct {
	Port    int    `yaml:"port"`
	BaseURL string `yaml:"base_url"`
}

type AWSConfig struct {
	Region string `yaml:"region"`
}

type S3Config struct {
	Bucket        string        `yaml:"bucket"`
	Prefix        string        `yaml:"prefix"`
	FlushInterval time.Duration `yaml:"flush_interval"`
}

type ModelConfig struct {
	ID                    string  `yaml:"id"`
	Name                  string  `yaml:"name"`
	InputPricePerMillion  float64 `yaml:"input_price_per_million"`
	OutputPricePerMillion float64 `yaml:"output_price_per_million"`
	Enabled               bool    `yaml:"enabled"`
}

func Load(path string) (*Config, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}

	data = []byte(os.ExpandEnv(string(data)))

	var cfg Config
	if err := yaml.Unmarshal(data, &cfg); err != nil {
		return nil, err
	}

	if cfg.Server.Port == 0 {
		cfg.Server.Port = 8080
	}
	if cfg.AWS.Region == "" {
		cfg.AWS.Region = "eu-central-1"
	}
	if cfg.S3.Prefix == "" {
		cfg.S3.Prefix = "bedrockproxy"
	}
	if cfg.S3.FlushInterval == 0 {
		cfg.S3.FlushInterval = 5 * time.Minute
	}

	return &cfg, nil
}
