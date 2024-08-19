package main

import (
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"time"

	"github.com/gofiber/fiber/v2"
	"github.com/rs/zerolog"
	"github.com/rs/zerolog/log"
)

const (
	// JobCleanupDelay is the delay before the job is removed from the map, cleanup working directory
	JobCleanupDelay = 5 * time.Minute
)

var jobMap = make(map[int64]*Job)

func init() {
	zerolog.TimeFieldFormat = zerolog.TimeFormatUnix
	log.Logger = log.Output(zerolog.ConsoleWriter{Out: os.Stdout})
}

func sign(workingDirectory, tokenPIN, certFile string) func(ctx *fiber.Ctx) error {
	return func(ctx *fiber.Ctx) error {
		log.Info().Str("ip", ctx.IP()).Msg("Received request")

		// create working directory
		ts := time.Now().UnixMilli()
		workingDirectory := fmt.Sprintf("%s/%d", workingDirectory, ts)

		log.Info().Int64("job_id", ts).Str("working_dir", workingDirectory).Msg("Working directory")
		if err := os.MkdirAll(workingDirectory, 0755); err != nil {
			log.Error().Err(err).Msg("Failed to create working directory")
			return ctx.Status(fiber.StatusInternalServerError).SendString(fmt.Sprintf("failed to create working directory: %v", err))
		}

		file, err := os.OpenFile(fmt.Sprintf("%s/%s", workingDirectory, "file"), os.O_CREATE|os.O_WRONLY, 0644)
		if err != nil {
			log.Error().Err(err).Msg("Failed to create file")
			return ctx.Status(fiber.StatusInternalServerError).SendString(fmt.Sprintf("failed to create file: %v", err))
		}

		log.Info().Str("file", file.Name()).Msg("Saving file")

		requestStream := ctx.Context().RequestBodyStream()
		if _, err := io.Copy(file, requestStream); err != nil {
			log.Error().Err(err).Msg("Failed to save file")
			return ctx.Status(fiber.StatusInternalServerError).SendString(fmt.Sprintf("failed to save file: %v", err))
		}

		jobMap[ts] = &Job{
			ID:         ts,
			Processing: true,
			Success:    false,
			Error:      "",
		}

		log.Info().Int64("job_id", ts).Msg("Processing job")

		//goland:noinspection HttpUrlsUsage
		args := []string{"--storetype", "PIV", "--storepass", tokenPIN, "--certfile", certFile, "-d", "sha384", "--tsaurl", "http://timestamp.sectigo.com"}
		if ctx.Get("X-Application-Name") != "" {
			args = append(args, "--name", ctx.Get("X-Application-Name"))
		}
		if ctx.Get("X-Application-URL") != "" {
			args = append(args, "--url", ctx.Get("X-Application-URL"))
		}
		args = append(args, fmt.Sprintf("%s/%s", workingDirectory, "file"))

		go func() {
			cmd := exec.Command("jsign", args...)
			out, err := cmd.CombinedOutput()
			if err != nil {
				jobMap[ts].Processing = false
				jobMap[ts].Success = false
				if out != nil {
					jobMap[ts].Error = fmt.Sprintf("failed to sign file: %v: %s", err, out)
					log.Error().Err(err).Str("output", string(out)).Msg("Failed to sign file")
				} else {
					jobMap[ts].Error = fmt.Sprintf("failed to sign file: %v", err)
					log.Error().Err(err).Msg("Failed to sign file")
				}
				log.Error().Int64("job_id", ts).Msg("Job failed")

				_ = file.Close()
				_ = os.RemoveAll(workingDirectory)
				return
			}

			jobMap[ts].Processing = false
			jobMap[ts].Success = true
			log.Info().Int64("job_id", ts).Str("output", string(out)).Msg("Job completed")

			go func() {
				time.Sleep(JobCleanupDelay)
				delete(jobMap, ts)

				if err := os.RemoveAll(workingDirectory); err != nil {
					if errors.Is(err, os.ErrNotExist) {
						return
					}
					log.Error().Err(err).Int64("job_id", ts).Msg("Failed to cleanup working directory")
				}
			}()
		}()

		return ctx.JSON(CreateJobResponse{ID: ts})
	}
}

func status() func(*fiber.Ctx) error {
	return func(ctx *fiber.Ctx) error {
		id, err := ctx.ParamsInt("id")
		if err != nil {
			log.Error().Err(err).Msg("Invalid job id")
			return ctx.Status(fiber.StatusBadRequest).SendString("invalid job id")
		}

		job, ok := jobMap[int64(id)]
		if !ok {
			log.Error().Int("job_id", id).Msg("Job not found")
			return ctx.Status(fiber.StatusNotFound).SendString("job not found")
		}

		return ctx.JSON(job)
	}
}

func download(workingDirectory string) func(*fiber.Ctx) error {
	return func(ctx *fiber.Ctx) error {
		id, err := ctx.ParamsInt("id")
		if err != nil {
			log.Error().Err(err).Msg("Invalid job id")
			return ctx.Status(fiber.StatusBadRequest).SendString("invalid job id")
		}

		job, ok := jobMap[int64(id)]
		if !ok {
			log.Error().Int("job_id", id).Msg("Job not found")
			return ctx.Status(fiber.StatusNotFound).SendString("job not found")
		}

		if job.Processing {
			log.Info().Int("job_id", id).Msg("Job is still processing")
			return ctx.Status(fiber.StatusAccepted).SendString("job is still processing")
		}

		if !job.Success {
			log.Error().Int("job_id", id).Str("error", job.Error).Msg("Job failed")
			return ctx.Status(fiber.StatusInternalServerError).SendString(job.Error)
		}

		file, err := os.Open(fmt.Sprintf("%s/%d/%s", workingDirectory, id, "file"))
		if err != nil {
			log.Error().Err(err).Int("job_id", id).Msg("Failed to open file")
			return ctx.Status(fiber.StatusInternalServerError).SendString(fmt.Sprintf("failed to open file: %v", err))
		}

		log.Info().Str("file", file.Name()).Str("ip", ctx.IP()).Msg("Serving file")

		delete(jobMap, int64(id))
		if err := os.RemoveAll(fmt.Sprintf("%s/%d", workingDirectory, id)); err != nil {
			log.Error().Err(err).Int("job_id", id).Msg("Failed to cleanup working directory")
		}

		return ctx.SendStream(file)
	}
}

func main() {
	requestKey := os.Getenv("REQUEST_KEY")
	tokenPIN := os.Getenv("TOKEN_PIN")
	certFile := os.Getenv("CERT_FILE")
	workingDirectory := os.TempDir()

	if requestKey == "" {
		log.Fatal().Msg("REQUEST_KEY is not set")
	}

	if workingDirectory == "" {
		log.Fatal().Msg("Working directory is not set")
	}

	if certFile == "" {
		certFile = "/etc/signing-server/cert.crt"

		if _, err := os.Stat(certFile); err != nil {
			log.Fatal().Str("cert_file", certFile).Msg("CERT_FILE is not set and default file does not exist")
		}

		log.Info().Str("cert_file", certFile).Msg("CERT_FILE is not set, using default")
	} else {
		if _, err := os.Stat(certFile); err != nil {
			log.Fatal().Str("cert_file", certFile).Msg("CERT_FILE does not exist")
		}
	}

	defer func() {
		// Cleanup working directory
		if err := os.RemoveAll(workingDirectory); err != nil {
			log.Error().Err(err).Msg("Failed to cleanup working directory")
		}
	}()

	server := fiber.New(fiber.Config{
		Prefork:           true,
		StreamRequestBody: true,

		// Normally executable files does not exceed 1GB
		BodyLimit: 1024 * 1024 * 1024,

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

	server.Use(func(ctx *fiber.Ctx) error {
		if ctx.Get("X-Request-Key") != requestKey {
			log.Warn().Str("ip", ctx.IP()).Msg("Unauthorized request")
			return ctx.SendStatus(fiber.StatusUnauthorized)
		}

		return ctx.Next()
	})

	server.Post("/sign", sign(workingDirectory, tokenPIN, certFile))
	server.Get("/status/:id", status())
	server.Get("/download/:id", download(workingDirectory))

	log.Info().Msg("Starting server on :80")
	if err := server.Listen(":80"); err != nil {
		log.Fatal().Err(err).Msg("Failed to start server")
	}
}

type CreateJobResponse struct {
	ID int64 `json:"id"`
}

type Job struct {
	ID         int64  `json:"id"`
	Processing bool   `json:"processing"`
	Success    bool   `json:"success"`
	Error      string `json:"error"`
}
