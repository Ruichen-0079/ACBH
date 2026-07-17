package hobbyagent

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

type HTTPError struct {
	StatusCode int
	Message    string
}

func (e *HTTPError) Error() string { return e.Message }

type CoordinatorClient struct {
	HTTPClient *http.Client
}

func (c CoordinatorClient) Info(ctx context.Context, config Config) (CoordinatorInfo, error) {
	var response CoordinatorInfo
	if err := c.request(ctx, config, http.MethodGet, "/v1/info", nil, &response); err != nil {
		return CoordinatorInfo{}, err
	}
	return response, nil
}

func (c CoordinatorClient) Heartbeat(ctx context.Context, config Config, heartbeat Heartbeat) (CoordinatorStatus, error) {
	var response CoordinatorStatus
	if err := c.request(ctx, config, http.MethodPost, "/v1/heartbeat", heartbeat, &response); err != nil {
		return CoordinatorStatus{}, err
	}
	return response, nil
}

func (c CoordinatorClient) request(ctx context.Context, config Config, method, path string, body, result any) error {
	base, err := coordinatorURL(config)
	if err != nil {
		return err
	}
	var reader io.Reader
	if body != nil {
		data, err := json.Marshal(body)
		if err != nil {
			return err
		}
		reader = bytes.NewReader(data)
	}
	request, err := http.NewRequestWithContext(ctx, method, base+path, reader)
	if err != nil {
		return err
	}
	request.Header.Set("Authorization", "Bearer "+config.AccessToken)
	if body != nil {
		request.Header.Set("Content-Type", "application/json")
	}
	client := c.HTTPClient
	if client == nil {
		client = &http.Client{Timeout: 10 * time.Second}
	}
	response, err := client.Do(request)
	if err != nil {
		return err
	}
	defer response.Body.Close()
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		limited, _ := io.ReadAll(io.LimitReader(response.Body, 4096))
		message := strings.TrimSpace(string(limited))
		if message == "" {
			message = response.Status
		}
		message = strings.ReplaceAll(message, config.AccessToken, "[REDACTED]")
		return &HTTPError{StatusCode: response.StatusCode, Message: message}
	}
	if result == nil {
		return nil
	}
	decoder := json.NewDecoder(io.LimitReader(response.Body, 1024*1024))
	if err := decoder.Decode(result); err != nil {
		return fmt.Errorf("decode Coordinator response: %w", err)
	}
	return nil
}

func coordinatorURL(config Config) (string, error) {
	host := strings.TrimSpace(config.CoordinatorHost)
	if host == "" {
		return "", errors.New("coordinator host is empty")
	}
	if !strings.Contains(host, "://") {
		host = "http://" + host
	}
	parsed, err := url.Parse(host)
	if err != nil {
		return "", err
	}
	if parsed.Hostname() == "" {
		return "", errors.New("invalid coordinator host")
	}
	if parsed.Port() == "" {
		parsed.Host = fmt.Sprintf("%s:%d", parsed.Hostname(), config.normalized().CoordinatorPort)
	}
	return strings.TrimRight(parsed.String(), "/"), nil
}
