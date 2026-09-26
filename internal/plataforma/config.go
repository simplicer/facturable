package plataforma

import (
	"encoding/json"
	"os"
)

// Config is the whole runtime configuration (etc/config.json schema).
type Config struct {
	Env    string        `json:"env"`
	API    APIConfig     `json:"api"`
	Relay  RelayConfig   `json:"relay"`
	AEAT   AEATConfig    `json:"aeat"`
	Log    LogConfig     `json:"log"`
}

type APIConfig struct{ Listen string; Database string }
type RelayConfig struct {
	TTLDays            int `json:"ttl_days"`
	PollIntervalSecs   int `json:"poll_interval_seconds"`
}
type AEATConfig struct{ Environment string `json:"environment"` }
type LogConfig struct {
	Level string `json:"level"`
	Dir   string `json:"dir"`
}

// LoadConfig reads a config file, applying defaults for missing values.
func LoadConfig(path string) (*Config, error) {
	c := &Config{Env: "dev", API: APIConfig{Listen: ":8080", Database: "var/api.sqlite"}, Relay: RelayConfig{TTLDays: 7, PollIntervalSecs: 45}, AEAT: AEATConfig{Environment: "preprod"}, Log: LogConfig{Level: "info", Dir: "var/log"}}
	b, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return c, nil
		}
		return nil, err
	}
	if err := json.Unmarshal(b, c); err != nil {
		return nil, err
	}
	return c, nil
}
