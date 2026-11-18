package config

import (
	"os"
)

func (c *Config) applyEnv() {
	if v := os.Getenv("LOCAL_LLM_URL"); v != "" {
		c.BaseURL, c.EndpointSet = v, true
	}
	if v := os.Getenv("LOCAL_LLM_MODEL"); v != "" {
		c.Model = v
	}
}
