// Package config handles application configuration and setup
package config

import (
	"github.com/retroenv/retrogolib/log"
)

// CreateLogger creates a logger with appropriate settings
func CreateLogger(debug, quiet bool) *log.Logger {
	cfg := log.DefaultConfig()
	if debug {
		cfg.Level = log.DebugLevel
	} else if quiet {
		cfg.Level = log.ErrorLevel
	}

	return log.NewWithConfig(cfg)
}
