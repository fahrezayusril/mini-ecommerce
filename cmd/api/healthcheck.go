package main

import (
	"context"
	"fmt"
	"net/http"
	"os"
	"strings"
	"time"
)

const (
	healthcheckCommand = "healthcheck"
	healthcheckTimeout = 3 * time.Second
	defaultPort        = "8080"
)

func runHealthcheck() int {
	port := strings.TrimSpace(os.Getenv("PORT"))
	if port == "" {
		port = defaultPort
	}

	ctx, cancel := context.WithTimeout(context.Background(), healthcheckTimeout)
	defer cancel()

	if err := checkHealth(ctx, "http://127.0.0.1:"+port+"/health"); err != nil {
		fmt.Fprintf(os.Stderr, "healthcheck failed: %v\n", err)
		return 1
	}
	return 0
}

func checkHealth(ctx context.Context, url string) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return err
	}

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("unexpected status %d", resp.StatusCode)
	}
	return nil
}
