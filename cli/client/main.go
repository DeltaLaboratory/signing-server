package main

import (
	"flag"
	"fmt"
	"io"
	"net/http"
	"os"
)

var (
	flagInput  string
	flagOutput string

	envEndpoint     string
	envRequestToken string
)

func main() {
	client := &http.Client{}

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

	req, err := http.NewRequest("POST", fmt.Sprintf("https://%s/sign", envEndpoint), file)
	if err != nil {
		fmt.Printf("failed to create request: %v\n", err)
		os.Exit(4)
	}

	req.Header.Set("X-Request-Key", envRequestToken)

	resp, err := client.Do(req)
	if err != nil {
		fmt.Printf("failed to send request: %v\n", err)
		os.Exit(5)
	}

	defer func() {
		if err := resp.Body.Close(); err != nil {
			fmt.Println("failed to close response body")
		}
	}()

	if resp.StatusCode != http.StatusOK {
		erst, err := io.ReadAll(resp.Body)
		if err != nil {
			fmt.Printf("failed to read response body: %s, %v\n", resp.Status, err)
		} else {
			fmt.Printf("failed to sign file: %s\n\t%s\n", resp.Status, erst)
		}
		os.Exit(6)
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

	if _, err := io.Copy(out, resp.Body); err != nil {
		fmt.Printf("failed to save output file: %v\n", err)
		os.Exit(8)
	}

	fmt.Println("file signed successfully")
}
