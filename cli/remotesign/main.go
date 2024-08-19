package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"net/http"
	"os"
	"time"

	"github.com/rs/zerolog"
	"github.com/rs/zerolog/log"
)

var (
	flagInput  string
	flagOutput string

	flagApplicationName string
	flagApplicationURL  string

	envEndpoint     string
	envRequestToken string

	client = &http.Client{}
)

func init() {
	zerolog.TimeFieldFormat = zerolog.TimeFormatUnix
	log.Logger = log.Output(zerolog.ConsoleWriter{Out: os.Stderr})
}

func createJob(file io.Reader) (int64, error) {
	req, err := http.NewRequest("POST", fmt.Sprintf("https://%s/sign", envEndpoint), file)
	if err != nil {
		return 0, fmt.Errorf("failed to create request: %w", err)
	}

	req.Header.Set("X-Request-Key", envRequestToken)
	if flagApplicationName != "" {
		req.Header.Set("X-Application-Name", flagApplicationName)
	}

	if flagApplicationURL != "" {
		req.Header.Set("X-Application-URL", flagApplicationURL)
	}

	resp, err := client.Do(req)
	if err != nil {
		return 0, fmt.Errorf("failed to send request: %w", err)
	}

	defer func() {
		if err := resp.Body.Close(); err != nil {
			log.Error().Err(err).Msg("Failed to close response body")
		}
	}()

	if resp.StatusCode != http.StatusOK {
		erst, err := io.ReadAll(resp.Body)
		if err != nil {
			return 0, fmt.Errorf("failed to read response body: %s, %w", resp.Status, err)
		} else {
			return 0, fmt.Errorf("failed to create job: %s\n\t%s\n", resp.Status, erst)
		}
	}

	var cjresp CreateJobResponse
	if err := json.NewDecoder(resp.Body).Decode(&cjresp); err != nil {
		return 0, fmt.Errorf("failed to decode response: %w", err)
	}

	return cjresp.ID, nil
}

func getJobStatus(jobID int64) (*Job, error) {
	req, err := http.NewRequest("GET", fmt.Sprintf("https://%s/status/%d", envEndpoint, jobID), nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	req.Header.Set("X-Request-Key", envRequestToken)

	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("failed to send request: %w", err)
	}

	defer func() {
		if err := resp.Body.Close(); err != nil {
			log.Error().Err(err).Msg("Failed to close response body")
		}
	}()

	if resp.StatusCode != http.StatusOK {
		erst, err := io.ReadAll(resp.Body)
		if err != nil {
			return nil, fmt.Errorf("failed to read response body: %s, %w", resp.Status, err)
		} else {
			return nil, fmt.Errorf("failed to get job status: %s\n\t%s\n", resp.Status, erst)
		}
	}

	var job Job
	if err := json.NewDecoder(resp.Body).Decode(&job); err != nil {
		return nil, fmt.Errorf("failed to decode response: %w", err)
	}

	return &job, nil
}

func downloadFile(jobID int64) (io.ReadCloser, error) {
	req, err := http.NewRequest("GET", fmt.Sprintf("https://%s/download/%d", envEndpoint, jobID), nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	req.Header.Set("X-Request-Key", envRequestToken)

	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("failed to send request: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		erst, err := io.ReadAll(resp.Body)
		if err != nil {
			return nil, fmt.Errorf("failed to read response body: %s, %w", resp.Status, err)
		} else {
			return nil, fmt.Errorf("failed to download file: %s\n\t%s\n", resp.Status, erst)
		}
	}

	return resp.Body, nil
}

func main() {
	envEndpoint = os.Getenv("ENDPOINT")
	envRequestToken = os.Getenv("REQUEST_TOKEN")

	flag.StringVar(&flagInput, "input", "", "input file")
	flag.StringVar(&flagOutput, "output", "", "output file")
	flag.StringVar(&flagApplicationName, "appname", "", "application name")
	flag.StringVar(&flagApplicationURL, "appurl", "", "application url")
	flag.Parse()

	if envEndpoint == "" {
		log.Fatal().Msg("Endpoint is required")
	}

	if envRequestToken == "" {
		log.Fatal().Msg("Request token is required")
	}

	if flagInput == "" {
		log.Fatal().Msg("Input file is required")
	}

	if flagOutput == "" {
		log.Fatal().Msg("Output file is required")
	}

	file, err := os.Open(flagInput)
	if err != nil {
		log.Fatal().Err(err).Msg("Failed to open input file")
	}

	jobID, err := createJob(file)
	if err != nil {
		log.Fatal().Err(err).Msg("Failed to create job")
	}

	log.Info().Int64("jobID", jobID).Msg("Job created")

	done := make(chan bool)

	if os.Getenv("CI") == "" {
		go spinner(done)
	} else {
		log.Info().Msg("Processing...")
		go func() {
			<-done
		}()
	}

	for {
		job, err := getJobStatus(jobID)
		if err != nil {
			log.Fatal().Err(err).Msg("Failed to get job status")
		}

		if !job.Processing {
			if !job.Success {
				done <- true
				log.Fatal().Str("error", job.Error).Msg("Job failed")
			}

			done <- true
			break
		}
		time.Sleep(1 * time.Second)
	}

	out, err := os.Create(flagOutput)
	if err != nil {
		log.Fatal().Err(err).Msg("Failed to create output file")
	}

	defer func() {
		if err := out.Close(); err != nil {
			log.Error().Err(err).Msg("Failed to close output file")
		}
	}()

	resp, err := downloadFile(jobID)
	if err != nil {
		log.Fatal().Err(err).Msg("Failed to download file")
	}

	if _, err := io.Copy(out, resp); err != nil {
		log.Fatal().Err(err).Msg("Failed to save output file")
	}

	log.Info().Msg("File signed successfully")
}

func spinner(done <-chan bool) {
	startTime := time.Now()
	frames := []string{"⣾", "⣽", "⣻", "⢿", "⡿", "⣟", "⣯", "⣷"}
	ticker := time.NewTicker(100 * time.Millisecond)
	defer ticker.Stop()

	i := 0
	for {
		select {
		case <-done:
			fmt.Printf("\033[2K\r✓ Done (%ds)\n", int(time.Since(startTime).Seconds()))
			return
		case <-ticker.C:
			fmt.Printf("\r%s Processing... (%ds)", frames[i], int(time.Since(startTime).Seconds()))
			i = (i + 1) % len(frames)
		}
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
