package main

import (
	"fmt"
	"io"
	"os"
	"os/exec"
	"time"

	"github.com/gofiber/fiber/v2"
)

func sign(workingDirectory, requestKey, tokenPIN string) func(ctx *fiber.Ctx) error {
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

		// sign with osslsigncode with pkcs11 token
		//goland:noinspection HttpUrlsUsage
		cmd := exec.Command("osslsigncode", "sign", "-h", "sha384", "-pkcs11module", "/usr/lib/x86_64-linux-gnu/libykcs11.so", "-key", "pkcs11:id=%01", "-pass", tokenPIN, "-ts", "http://timestamp.sectigo.com", "-in", fmt.Sprintf("%s/%s", workingDirectory, "file"), "-out", fmt.Sprintf("%s/%s", workingDirectory, "signed"))
		if out, err := cmd.CombinedOutput(); err != nil {
			if out != nil {
				return ctx.Status(fiber.StatusInternalServerError).SendString(fmt.Sprintf("failed to sign file: %v: %s", err, out))
			} else {
				return ctx.Status(fiber.StatusInternalServerError).SendString(fmt.Sprintf("failed to sign file: %v", err))
			}
		}

		// send signed file
		signedFile, err := os.Open(fmt.Sprintf("%s/%s", workingDirectory, "signed"))
		if err != nil {
			return ctx.Status(fiber.StatusInternalServerError).SendString(fmt.Sprintf("failed to open signed file: %v", err))
		}

		return ctx.Status(fiber.StatusOK).SendStream(signedFile)
	}
}

func main() {
	requestKey := os.Getenv("REQUEST_KEY")
	tokenPIN := os.Getenv("TOKEN_PIN")
	workingDirectory := os.TempDir()

	if requestKey == "" {
		panic("REQUEST_KEY is not set")
	}

	if workingDirectory == "" {
		panic("Working directory is not set")
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

	server.Post("/sign", sign(workingDirectory, requestKey, tokenPIN))

	if err := server.Listen(":80"); err != nil {
		panic(err)
	}
}
