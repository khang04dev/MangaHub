package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

func apiURL(baseURL, path string) string {
	return strings.TrimRight(baseURL, "/") + path
}

func doJSON(ctx context.Context, method, url string, body any, token string) ([]byte, int, error) {
	var reader io.Reader
	if body != nil {
		payload, err := json.Marshal(body)
		if err != nil {
			return nil, 0, err
		}
		reader = bytes.NewReader(payload)
	}

	req, err := http.NewRequestWithContext(ctx, method, url, reader)
	if err != nil {
		return nil, 0, err
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}

	client := &http.Client{Timeout: 15 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return nil, 0, err
	}
	defer resp.Body.Close()

	data, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, resp.StatusCode, err
	}
	if resp.StatusCode >= 400 {
		msg := extractAPIError(data)
		if msg == "" {
			msg = string(data)
		}
		return data, resp.StatusCode, fmt.Errorf(msg)
	}
	return data, resp.StatusCode, nil
}

func extractAPIError(data []byte) string {
	var payload struct {
		Error   string `json:"error"`
		Details string `json:"details"`
	}
	if err := json.Unmarshal(data, &payload); err != nil {
		return ""
	}
	if payload.Error == "" {
		return ""
	}
	if payload.Details == "" {
		return payload.Error
	}
	return payload.Error + ": " + payload.Details
}
