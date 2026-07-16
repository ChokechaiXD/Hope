package main

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"time"
)

type readinessChecker struct {
	probe    func(context.Context, string) error
	timeout  time.Duration
	interval time.Duration
}

func defaultReadinessChecker() readinessChecker {
	client := &http.Client{Timeout: 500 * time.Millisecond}
	return readinessChecker{
		probe: func(ctx context.Context, address string) error {
			request, err := http.NewRequestWithContext(ctx, http.MethodGet, "http://"+address+"/v1/health", nil)
			if err != nil {
				return err
			}
			response, err := client.Do(request)
			if err != nil {
				return err
			}
			defer response.Body.Close()
			if response.StatusCode != http.StatusOK {
				return fmt.Errorf("health endpoint returned %s", response.Status)
			}
			var payload struct {
				Status string `json:"status"`
			}
			if err := json.NewDecoder(response.Body).Decode(&payload); err != nil {
				return fmt.Errorf("decode health response: %w", err)
			}
			if payload.Status != "ok" {
				return fmt.Errorf("health status is %q", payload.Status)
			}
			return nil
		},
		timeout:  5 * time.Second,
		interval: 100 * time.Millisecond,
	}
}

func (checker readinessChecker) wait(ctx context.Context, address string) error {
	timeout := checker.timeout
	if timeout <= 0 {
		timeout = 5 * time.Second
	}
	interval := checker.interval
	if interval <= 0 {
		interval = time.Millisecond
	}
	readyCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	var lastErr error
	for {
		if err := checker.probe(readyCtx, address); err == nil {
			return nil
		} else {
			lastErr = err
		}
		select {
		case <-readyCtx.Done():
			return fmt.Errorf("%w (last probe: %v)", readyCtx.Err(), lastErr)
		case <-ticker.C:
		}
	}
}
