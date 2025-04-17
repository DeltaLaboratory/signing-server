package handlers

import (
	"fmt"
	"io"
	"os"
	"os/exec"
	"strings"

	"github.com/gofiber/fiber/v2"
	"github.com/google/uuid"
	"github.com/rs/zerolog/log"

	"github.com/DeltaLaboratory/signing-server/internal/job"
	"github.com/DeltaLaboratory/signing-server/pkg/models"
)

// SignHandler handles the sign endpoint
func SignHandler(workingDirectory, tokenPIN, certFile, timeStampServer string) fiber.Handler {
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

		// Extract file extension from X-Filename header
		extension := ""
		if filename := ctx.Get("X-Filename"); filename != "" {
			// Find the last dot in the filename
			lastDotIndex := strings.LastIndex(filename, ".")
			if lastDotIndex != -1 {
				extension = filename[lastDotIndex:]
			}
		}

		// Use the extension in the temporary file name
		filePath := fmt.Sprintf("%s/file%s", jobWorkingDirectory, extension)
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

		job.CreateJob(id, extension)

		args := buildSigningArgs(tokenPIN, certFile, ctx, jobWorkingDirectory, timeStampServer, extension)
		cmd := exec.Command("jsign", args...)

		go job.ProcessSigningJob(id, cmd, jobWorkingDirectory)

		return ctx.JSON(models.CreateJobResponse{ID: id})
	}
}

// StatusHandler handles the status endpoint
func StatusHandler() fiber.Handler {
	return func(ctx *fiber.Ctx) error {
		idStr := ctx.Params("id")
		id, err := uuid.Parse(idStr)
		if err != nil {
			return ctx.Status(fiber.StatusBadRequest).JSON(fiber.Map{
				"error": "invalid job id",
			})
		}

		jobObj, ok := job.GetJob(id)
		if !ok {
			return ctx.Status(fiber.StatusNotFound).JSON(fiber.Map{
				"error": "job not found",
			})
		}
		return ctx.JSON(jobObj)
	}
}

// DownloadHandler handles the download endpoint
func DownloadHandler(workingDirectory string) fiber.Handler {
	return func(ctx *fiber.Ctx) error {
		idStr := ctx.Params("id")
		id, err := uuid.Parse(idStr)
		if err != nil {
			return ctx.Status(fiber.StatusBadRequest).JSON(fiber.Map{
				"error": "invalid job id",
			})
		}

		jobObj, ok := job.GetJob(id)
		if !ok {
			log.Debug().Str("job_id", id.String()).Msg("Job not found")
			return ctx.Status(fiber.StatusNotFound).JSON(fiber.Map{
				"error": "job not found",
			})
		}

		if jobObj.Processing {
			return ctx.Status(fiber.StatusAccepted).JSON(fiber.Map{
				"message": "job is still processing",
			})
		}

		if !jobObj.Success {
			return ctx.Status(fiber.StatusInternalServerError).JSON(fiber.Map{
				"error": jobObj.Error,
			})
		}

		go job.CleanupJob(id, fmt.Sprintf("%s/%s", workingDirectory, id))

		// Use the stored extension when serving the file
		filePath := fmt.Sprintf("%s/%s/file%s", workingDirectory, id, jobObj.Extension)
		if jobObj.Extension != "" {
			// Set the Content-Disposition header to include the original filename with extension
			ctx.Set("Content-Disposition", fmt.Sprintf("attachment; filename=signed%s", jobObj.Extension))
		}
		return ctx.SendFile(filePath)
	}
}

// buildSigningArgs builds the arguments for the jsign command
func buildSigningArgs(tokenPIN, certFile string, ctx *fiber.Ctx, jobWorkingDirectory string, timeStampServer string, extension string) []string {
	args := []string{
		"--storetype", "PIV",
		"--storepass", tokenPIN,
		"--certfile", certFile,
	}

	// Get digest algorithm from header or use default
	algorithm := ctx.Get("X-Algorithm")
	if algorithm == "" {
		algorithm = "sha384"
	}
	args = append(args, "-d", algorithm)

	// Get timestamp mode from header
	tsMode := ctx.Get("X-Timestamp-Mode")

	// Handle timestamp server URL and mode
	customTsUrl := ctx.Get("X-Timestamp-Server")
	if customTsUrl != "" {
		// Use custom timestamp server if provided
		args = append(args, "--tsaurl", customTsUrl)
	} else if timeStampServer != "" {
		// Otherwise use the configured one
		args = append(args, "--tsaurl", timeStampServer)
	}

	// Add timestamp mode if specified
	if tsMode != "" {
		args = append(args, "--tsmode", tsMode)
	} else {
		// Default to "all"
		args = append(args, "--tsmode", "RFC3161")
	}

	if appName := ctx.Get("X-Application-Name"); appName != "" {
		args = append(args, "--name", appName)
	}
	if appURL := ctx.Get("X-Application-URL"); appURL != "" {
		args = append(args, "--url", appURL)
	}
	args = append(args, fmt.Sprintf("%s/file%s", jobWorkingDirectory, extension))
	return args
}
