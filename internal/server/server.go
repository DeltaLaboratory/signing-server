package server

import (
	"github.com/gofiber/fiber/v2"
	"github.com/rs/zerolog/log"

	"github.com/DeltaLaboratory/signing-server/internal/config"
	"github.com/DeltaLaboratory/signing-server/internal/handlers"
	"github.com/DeltaLaboratory/signing-server/internal/job"
)

// Setup creates and configures a new Fiber server
func Setup() *fiber.App {
	return fiber.New(fiber.Config{
		StreamRequestBody:       true,
		BodyLimit:               job.MaxFileSize,
		EnableIPValidation:      true,
		ProxyHeader:             "X-Forwarded-For",
		EnableTrustedProxyCheck: true,
		TrustedProxies: []string{
			"10.0.0.0/8",
			"172.16.0.0/12",
			"192.168.0.0/16",
			"169.254.0.0/16",
		},
	})
}

// SetupRoutes configures the routes for the server
func SetupRoutes(server *fiber.App, config config.Config) {
	// Authentication middleware
	server.Use(func(ctx *fiber.Ctx) error {
		if ctx.Get("X-Request-Key") != config.RequestKey {
			log.Warn().Str("ip", ctx.IP()).Msg("Unauthorized request")
			return ctx.SendStatus(fiber.StatusUnauthorized)
		}
		return ctx.Next()
	})

	// Routes
	server.Post("/sign", handlers.SignHandler(config.WorkingDirectory, config.TokenPIN, config.CertFile, config.TimeStampServer))
	server.Get("/status/:id", handlers.StatusHandler())
	server.Get("/download/:id", handlers.DownloadHandler(config.WorkingDirectory))
}

// Start starts the server on the specified port
func Start(server *fiber.App, port string) error {
	log.Info().Msgf("Starting server on %s", port)
	return server.Listen(port)
}
