package main

import (
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"sync"
	"time"

	"github.com/gofiber/fiber/v2"
	"github.com/google/uuid"
	"github.com/rs/zerolog"
	"github.com/rs/zerolog/log"
)

const (
	// JobCleanupDelay is the delay before the job is removed from the map and working directory is cleaned
	JobCleanupDelay = 5 * time.Minute
	// DefaultCertFile is the default path for the certificate file
	DefaultCertFile = "/etc/signing-server/cert.crt"
	// MaxFileSize is the maximum file size limit (1GB)
	MaxFileSize            = 1024 * 1024 * 1024
	DefaultTimeStampServer = "http://timestamp.acs.microsoft.com"
)

// Job represents a signing job
type Job struct {
	ID         uuid.UUID `json:"id"`
	Processing bool      `json:"processing"`
	Success    bool      `json:"success"`
	Error      string    `json:"error"`
}

// CreateJobResponse represents the response for job creation
type CreateJobResponse struct {
	ID uuid.UUID `json:"id"`
}

var (
	jobMapLock = sync.RWMutex{}
	jobMap     = map[uuid.UUID]*Job{}
)

func init() {
	// Configure zerolog
	zerolog.TimeFieldFormat = zerolog.TimeFormatUnix
	log.Logger = log.Output(zerolog.ConsoleWriter{Out: os.Stdout})
}

// cleanupJob removes the job from the map and its working directory after a delay
func cleanupJob(id uuid.UUID, workingDirectory string) {
	time.Sleep(JobCleanupDelay)
	log.Info().Str("job_id", id.String()).Msg("Cleaning up job")
	jobMapLock.Lock()
	delete(jobMap, id)
	jobMapLock.Unlock()
	if err := os.RemoveAll(workingDirectory); err != nil {
		if !errors.Is(err, os.ErrNotExist) {
			log.Error().Err(err).Str("job_id", id.String()).Msg("Failed to cleanup working directory")
		}
	}
}

// processSigningJob handles the actual signing process
func processSigningJob(id uuid.UUID, cmd *exec.Cmd, jobWorkingDirectory string) {
	out, err := cmd.CombinedOutput()
	jobMapLock.Lock()
	defer jobMapLock.Unlock()
	if err != nil {
		jobMap[id].Processing = false
		jobMap[id].Success = false
		if out != nil {
			jobMap[id].Error = fmt.Sprintf("failed to sign file: %v: %s", err, out)
			log.Error().Err(err).Str("output", string(out)).Msg("Failed to sign file")
		} else {
			jobMap[id].Error = fmt.Sprintf("failed to sign file: %v", err)
			log.Error().Err(err).Msg("Failed to sign file")
		}
		log.Error().Str("job_id", id.String()).Msg("Job failed")
		_ = os.RemoveAll(jobWorkingDirectory)
		return
	}
	jobMap[id].Processing = false
	jobMap[id].Success = true
	log.Info().Str("job_id", id.String()).Str("output", string(out)).Msg("Job completed")
}

func sign(workingDirectory, tokenPIN, certFile, timeStampServer string) fiber.Handler {
	return func(ctx *fiber.Ctx) error {
		log.Info().Str("ip", ctx.IP()).Msg("Received request")
		id := uuid.New()
		jobWorkingDirectory := fmt.Sprintf("%s/%s", workingDirectory, id.String())

		if err := os.MkdirAll(jobWorkingDirectory, 0755); err != nil {
			log.Error().Err(err).Msg("Failed to create working directory")
			return ctx.Status(fiber.StatusInternalServerError).JSON(fiber.Map{
				"error": fmt.Sprintf("failed to create working directory: %v", err),
			})
		}

		filePath := fmt.Sprintf("%s/file", jobWorkingDirectory)
		file, err := os.OpenFile(filePath, os.O_CREATE|os.O_WRONLY, 0644)
		if err != nil {
			log.Error().Err(err).Msg("Failed to create file")
			return ctx.Status(fiber.StatusInternalServerError).JSON(fiber.Map{
				"error": fmt.Sprintf("failed to create file: %v", err),
			})
		}
		defer file.Close()

		if _, err := io.Copy(file, ctx.Context().RequestBodyStream()); err != nil {
			log.Error().Err(err).Msg("Failed to save file")
			return ctx.Status(fiber.StatusInternalServerError).JSON(fiber.Map{
				"error": fmt.Sprintf("failed to save file: %v", err),
			})
		}

		jobMapLock.Lock()
		jobMap[id] = &Job{
			ID:         id,
			Processing: true,
			Success:    false,
		}
		jobMapLock.Unlock()

		args := buildSigningArgs(tokenPIN, certFile, ctx, jobWorkingDirectory, timeStampServer)
		cmd := exec.Command("jsign", args...)

		go processSigningJob(id, cmd, jobWorkingDirectory)

		return ctx.JSON(CreateJobResponse{ID: id})
	}
}

func buildSigningArgs(tokenPIN, certFile string, ctx *fiber.Ctx, jobWorkingDirectory string, timeStampServer string) []string {
	args := []string{
		"--storetype", "PIV",
		"--storepass", tokenPIN,
		"--certfile", certFile,
		"-d", "sha384",
		"--tsaurl", timeStampServer,
	}
	if appName := ctx.Get("X-Application-Name"); appName != "" {
		args = append(args, "--name", appName)
	}
	if appURL := ctx.Get("X-Application-URL"); appURL != "" {
		args = append(args, "--url", appURL)
	}
	args = append(args, fmt.Sprintf("%s/file", jobWorkingDirectory))
	return args
}

func status() fiber.Handler {
	return func(ctx *fiber.Ctx) error {
		idStr := ctx.Params("id")
		id, err := uuid.Parse(idStr)
		if err != nil {
			return ctx.Status(fiber.StatusBadRequest).JSON(fiber.Map{
				"error": "invalid job id",
			})
		}

		jobMapLock.RLock()
		defer jobMapLock.RUnlock()
		job, ok := jobMap[id]
		if !ok {
			return ctx.Status(fiber.StatusNotFound).JSON(fiber.Map{
				"error": "job not found",
			})
		}
		return ctx.JSON(job)
	}
}

func download(workingDirectory string) fiber.Handler {
	return func(ctx *fiber.Ctx) error {
		idStr := ctx.Params("id")
		id, err := uuid.Parse(idStr)
		if err != nil {
			return ctx.Status(fiber.StatusBadRequest).JSON(fiber.Map{
				"error": "invalid job id",
			})
		}

		jobMapLock.RLock()
		job, ok := jobMap[id]
		jobMapLock.RUnlock()
		if !ok {
			for k, v := range jobMap {
				log.Debug().Str("job_id", k.String()).Bool("job", v.Success).Msg("Job")
			}

			log.Debug().Str("job_id", id.String()).Msg("Job not found")
			return ctx.Status(fiber.StatusNotFound).JSON(fiber.Map{
				"error": "job not found",
			})
		}

		if job.Processing {
			return ctx.Status(fiber.StatusAccepted).JSON(fiber.Map{
				"message": "job is still processing",
			})
		}

		if !job.Success {
			return ctx.Status(fiber.StatusInternalServerError).JSON(fiber.Map{
				"error": job.Error,
			})
		}

		filePath := fmt.Sprintf("%s/%s/file", workingDirectory, id)
		file, err := os.Open(filePath)
		if err != nil {
			return ctx.Status(fiber.StatusInternalServerError).JSON(fiber.Map{
				"error": fmt.Sprintf("failed to open file: %v", err),
			})
		}
		defer file.Close()

		go cleanupJob(id, fmt.Sprintf("%s/%s", workingDirectory, id))

		return ctx.SendStream(file)
	}
}

type Config struct {
	RequestKey       string
	TokenPIN         string
	CertFile         string
	WorkingDirectory string
	TimeStampServer  string
}

func loadConfig() Config {
	return Config{
		RequestKey:       os.Getenv("REQUEST_KEY"),
		TokenPIN:         os.Getenv("TOKEN_PIN"),
		CertFile:         os.Getenv("CERT_FILE"),
		WorkingDirectory: os.TempDir(),
		TimeStampServer:  os.Getenv("TIMESTAMP_SERVER"),
	}
}

func validateConfig(config *Config) {
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

func setupServer() *fiber.App {
	return fiber.New(fiber.Config{
		StreamRequestBody:       true,
		BodyLimit:               MaxFileSize,
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

func setupRoutes(server *fiber.App, config Config) {
	server.Use(func(ctx *fiber.Ctx) error {
		if ctx.Get("X-Request-Key") != config.RequestKey {
			log.Warn().Str("ip", ctx.IP()).Msg("Unauthorized request")
			return ctx.SendStatus(fiber.StatusUnauthorized)
		}
		return ctx.Next()
	})

	server.Post("/sign", sign(config.WorkingDirectory, config.TokenPIN, config.CertFile, config.TimeStampServer))
	server.Get("/status/:id", status())
	server.Get("/download/:id", download(config.WorkingDirectory))
}

func main() {
	config := loadConfig()
	validateConfig(&config)
	server := setupServer()
	setupRoutes(server, config)

	log.Info().Msg("Starting server on :80")
	if err := server.Listen(":80"); err != nil {
		log.Fatal().Err(err).Msg("Failed to start server")
	}
}
