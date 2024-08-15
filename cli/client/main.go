package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"net/http"
	"os"
	"time"
)

var (
	flagInput  string
	flagOutput string

	envEndpoint     string
	envRequestToken string

	client = &http.Client{}
)

func createJob(file io.Reader) (int64, error) {
	req, err := http.NewRequest("POST", fmt.Sprintf("https://%s/sign", envEndpoint), file)
	if err != nil {
		return 0, fmt.Errorf("failed to create request: %w", err)
	}

	req.Header.Set("X-Request-Key", envRequestToken)

	resp, err := client.Do(req)
	if err != nil {
		return 0, fmt.Errorf("failed to send request: %w", err)
	}

	defer func() {
		if err := resp.Body.Close(); err != nil {
			fmt.Println("failed to close response body")
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
			fmt.Println("failed to close response body")
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
	flag.Parse()

	if envEndpoint == "" {
		fmt.Println("endpoint is required")
		os.Exit(1)
	}

	if envRequestToken == "" {
		fmt.Println("request token is required")
		os.Exit(1)
	}

	if flagInput == "" {
		fmt.Println("input file is required")
		os.Exit(2)
	}

	if flagOutput == "" {
		fmt.Println("output file is required")
		os.Exit(2)
	}

	file, err := os.Open(flagInput)
	if err != nil {
		fmt.Printf("failed to open input file: %v\n", err)
		os.Exit(3)
	}
	defer func() {
		if err := file.Close(); err != nil {
			fmt.Println("failed to close input file")
		}
	}()

	jobID, err := createJob(file)
	if err != nil {
		fmt.Printf("failed to create job: %v\n", err)
		os.Exit(4)
	}

	fmt.Printf("job created with id: %d\n", jobID)

	done := make(chan bool)

	if os.Getenv("CI") == "" {
		go spinner(done)
	} else {
		fmt.Println("processing...")
		go func() {
			<-done
		}()
	}

	for {
		job, err := getJobStatus(jobID)
		if err != nil {
			fmt.Printf("failed to get job status: %v\n", err)
			os.Exit(5)
		}

		if !job.Processing {
			if !job.Success {
				fmt.Printf("job failed: %s\n", job.Error)
				os.Exit(6)
			}

			done <- true
			break
		}
		time.Sleep(1 * time.Second)
	}

	out, err := os.Create(flagOutput)
	if err != nil {
		fmt.Printf("failed to create output file: %v\n", err)
		os.Exit(7)
	}

	defer func() {
		if err := out.Close(); err != nil {
			fmt.Println("failed to close output file")
		}
	}()

	resp, err := downloadFile(jobID)
	if err != nil {
		fmt.Printf("failed to download file: %v\n", err)
		os.Exit(6)
	}

	if _, err := io.Copy(out, resp); err != nil {
		fmt.Printf("failed to save output file: %v\n", err)
		os.Exit(8)
	}

	fmt.Println("file signed successfully")
}

func spinner(done <-chan bool) {
	startTime := time.Now()

	frames := []string{"◰", "◳", "◲", "◱"}
	ticker := time.NewTicker(100 * time.Millisecond)
	defer ticker.Stop()

	i := 0
	for {
		select {
		case <-done:
			return
		case <-ticker.C:
			fmt.Printf("\r%s processing... (%ds)", frames[i], int(time.Since(startTime).Seconds()))
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
