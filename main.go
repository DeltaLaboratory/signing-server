package main

import (
	"os"

	"github.com/rs/zerolog"
	"github.com/rs/zerolog/log"

	"github.com/DeltaLaboratory/signing-server/internal/config"
	"github.com/DeltaLaboratory/signing-server/internal/server"
)

func init() {
	// Configure zerolog
	zerolog.TimeFieldFormat = zerolog.TimeFormatUnix
	log.Logger = log.Output(zerolog.ConsoleWriter{Out: os.Stdout})
}

func main() {
	// Load and validate configuration
	cfg := config.Load()
	config.Validate(&cfg)

	// Setup server
	app := server.Setup()
	server.SetupRoutes(app, cfg)

	// Start server
	if err := server.Start(app, ":80"); err != nil {
		log.Fatal().Err(err).Msg("Failed to start server")
	}
}
