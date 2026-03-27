package config

import (
	"os"

	"gopkg.in/yaml.v3"
)

type Config struct {
	Server   ServerConfig  `yaml:"server"`
	Database DatabaseConfig `yaml:"database"`
	AWS      AWSConfig     `yaml:"aws"`
	Models   []ModelConfig `yaml:"models"`
}

type ServerConfig struct {
	Port    int    `yaml:"port"`
	BaseURL string `yaml:"base_url"`
}

type DatabaseConfig struct {
	URL string `yaml:"url"`
}

type AWSConfig struct {
	Region string `yaml:"region"`
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

	return &cfg, nil
}
