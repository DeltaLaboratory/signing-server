package main

import (
	"fmt"
	"io"
	"os"
	"os/exec"
	"time"

	"github.com/gofiber/fiber/v2"
)

var jobMap = make(map[int64]*Job)

func sign(workingDirectory, requestKey, tokenPIN, certFile string) func(ctx *fiber.Ctx) error {
	return func(ctx *fiber.Ctx) error {
		if ctx.Get("X-Request-Key") != requestKey {
			return ctx.SendStatus(fiber.StatusUnauthorized)
		}

		// create working directory
		ts := time.Now().UnixMilli()
		workingDirectory = fmt.Sprintf("%s/%d", workingDirectory, ts)
		if err := os.MkdirAll(workingDirectory, 0755); err != nil {
			return ctx.Status(fiber.StatusInternalServerError).SendString(fmt.Sprintf("failed to create working directory: %v", err))
		}

		// save
		file, err := os.OpenFile(fmt.Sprintf("%s/%s", workingDirectory, "file"), os.O_CREATE|os.O_WRONLY, 0644)
		if err != nil {
			return ctx.Status(fiber.StatusInternalServerError).SendString(fmt.Sprintf("failed to create file: %v", err))
		}

		requestStream := ctx.Context().RequestBodyStream()
		if _, err := io.Copy(file, requestStream); err != nil {
			return ctx.Status(fiber.StatusInternalServerError).SendString(fmt.Sprintf("failed to save file: %v", err))
		}

		jobMap[ts] = &Job{
			ID:         ts,
			Processing: true,
			Success:    false,
			Error:      "",
		}

		go func() {
			// sign with osslsigncode with pkcs11 token
			//goland:noinspection HttpUrlsUsage
			cmd := exec.Command("osslsigncode", "sign", "-h", "sha384", "-pkcs11module", "/usr/lib/x86_64-linux-gnu/libykcs11.so", "-certs", certFile, "-key", "pkcs11:id=%01", "-pass", tokenPIN, "-ts", "http://timestamp.sectigo.com", "-in", fmt.Sprintf("%s/%s", workingDirectory, "file"), "-out", fmt.Sprintf("%s/%s", workingDirectory, "signed"))
			if out, err := cmd.CombinedOutput(); err != nil {
				if out != nil {
					jobMap[ts].Processing = false
					jobMap[ts].Success = false
					jobMap[ts].Error = fmt.Sprintf("failed to sign file: %v: %s", err, out)
				} else {
					jobMap[ts].Processing = false
					jobMap[ts].Success = false
					jobMap[ts].Error = fmt.Sprintf("failed to sign file: %v", err)
				}
			}

			jobMap[ts].Processing = false
			jobMap[ts].Success = true
		}()

		return ctx.JSON(CreateJobResponse{ID: ts})
	}
}

func status(requestKey string) func(*fiber.Ctx) error {
	return func(ctx *fiber.Ctx) error {
		if ctx.Get("X-Request-Key") != requestKey {
			return ctx.SendStatus(fiber.StatusUnauthorized)
		}

		id, err := ctx.ParamsInt("id")
		if err != nil {
			return ctx.Status(fiber.StatusBadRequest).SendString("invalid job id")
		}

		job, ok := jobMap[int64(id)]
		if !ok {
			return ctx.Status(fiber.StatusNotFound).SendString("job not found")
		}

		return ctx.JSON(job)
	}
}

func download(workingDirectory, requestKey string) func(*fiber.Ctx) error {
	return func(ctx *fiber.Ctx) error {
		if ctx.Get("X-Request-Key") != requestKey {
			return ctx.SendStatus(fiber.StatusUnauthorized)
		}

		id, err := ctx.ParamsInt("id")
		if err != nil {
			return ctx.Status(fiber.StatusBadRequest).SendString("invalid job id")
		}

		job, ok := jobMap[int64(id)]
		if !ok {
			return ctx.Status(fiber.StatusNotFound).SendString("job not found")
		}

		if job.Processing {
			return ctx.Status(fiber.StatusAccepted).SendString("job is still processing")
		}

		if !job.Success {
			return ctx.Status(fiber.StatusInternalServerError).SendString(job.Error)
		}

		file, err := os.Open(fmt.Sprintf("%s/%d/%s", workingDirectory, id, "signed"))
		if err != nil {
			return ctx.Status(fiber.StatusInternalServerError).SendString(fmt.Sprintf("failed to open file: %v", err))
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
		panic("REQUEST_KEY is not set")
	}

	if workingDirectory == "" {
		panic("Working directory is not set")
	}

	if certFile == "" {
		certFile = "/etc/signing-server/cert.crt"

		if _, err := os.Stat(certFile); err != nil {
			fmt.Println("CERT_FILE is not set and default file does not exist")
			os.Exit(1)
		}

		fmt.Printf("CERT_FILE is not set, using default: %s\n", certFile)
	} else {
		if _, err := os.Stat(certFile); err != nil {
			fmt.Printf("CERT_FILE does not exist: %s\n", certFile)
			os.Exit(1)
		}
	}

	defer func() {
		// Cleanup working directory
		if err := os.RemoveAll(workingDirectory); err != nil {
			fmt.Printf("Failed to cleanup working directory: %v\n", err)
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

	server.Post("/sign", sign(workingDirectory, requestKey, tokenPIN, certFile))
	server.Get("/status/:id", status(requestKey))
	server.Get("/download/:id", download(workingDirectory, requestKey))

	if err := server.Listen(":80"); err != nil {
		panic(err)
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
