package config

import (
	"os"

	"github.com/rs/zerolog/log"
)

const (
	// DefaultCertFile is the default path for the certificate file
	DefaultCertFile = "/etc/signing-server/cert.crt"
	// DefaultTimeStampServer is the default timestamp server URL
	DefaultTimeStampServer = "http://timestamp.acs.microsoft.com"
)

// Config holds the application configuration
type Config struct {
	RequestKey       string
	TokenPIN         string
	CertFile         string
	WorkingDirectory string
	TimeStampServer  string
}

// Load loads the configuration from environment variables
func Load() Config {
	return Config{
		RequestKey:       os.Getenv("REQUEST_KEY"),
		TokenPIN:         os.Getenv("TOKEN_PIN"),
		CertFile:         os.Getenv("CERT_FILE"),
		WorkingDirectory: os.TempDir(),
		TimeStampServer:  os.Getenv("TIMESTAMP_SERVER"),
	}
}

// Validate validates the configuration and sets defaults
func Validate(config *Config) {
	if config.RequestKey == "" {
		log.Fatal().Msg("REQUEST_KEY is not set")
	}
	if config.WorkingDirectory == "" {
		log.Fatal().Msg("Working directory is not set")
	}
	if config.CertFile == "" {
		config.CertFile = DefaultCertFile
	}
	if config.TimeStampServer == "" {
		config.TimeStampServer = DefaultTimeStampServer
	}
	if _, err := os.Stat(config.CertFile); err != nil {
		log.Fatal().Str("cert_file", config.CertFile).Msg("Certificate file does not exist")
	}
}
