package coordinator

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

type Client struct {
	baseURL    string
	httpClient *http.Client
}

type JoinGroupRequest struct {
	AccessKey   string `json:"accessKey"`
	DisplayName string `json:"displayName"`
}

type JoinGroupResponse struct {
	MemberID string `json:"memberId"`
	Role     string `json:"role"`
}

type RegisterHostRequest struct {
	GroupID      string `json:"groupId"`
	MemberID     string `json:"memberId"`
	DeviceName   string `json:"deviceName"`
	Platform     string `json:"platform"`
	AgentVersion string `json:"agentVersion"`
}

type RegisterHostResponse struct {
	HostID    string `json:"hostId"`
	HostToken string `json:"hostToken"`
}

type HeartbeatRequest struct {
	GroupID               string  `json:"groupId"`
	HostID                string  `json:"hostId"`
	HostToken             string  `json:"hostToken"`
	Status                string  `json:"status"`
	LatestLocalSnapshotID *string `json:"latestLocalSnapshotId"`
}

type HeartbeatResponse struct {
	OK     bool   `json:"ok"`
	HostID string `json:"hostId"`
	Status string `json:"status"`
}

type apiError struct {
	Message string `json:"message"`
	Error   string `json:"error"`
}

func NewClient(baseURL string) (*Client, error) {
	baseURL = strings.TrimRight(strings.TrimSpace(baseURL), "/")
	parsed, err := url.Parse(baseURL)
	if err != nil || parsed.Scheme == "" || parsed.Host == "" {
		return nil, fmt.Errorf("invalid coordinator URL %q", baseURL)
	}

	return &Client{
		baseURL: baseURL,
		httpClient: &http.Client{
			Timeout: 10 * time.Second,
		},
	}, nil
}

func (c *Client) JoinGroup(ctx context.Context, groupID string, req JoinGroupRequest) (JoinGroupResponse, error) {
	var out JoinGroupResponse
	err := c.post(ctx, "/v1/groups/"+url.PathEscape(groupID)+"/join", req, &out)
	return out, err
}

func (c *Client) RegisterHost(ctx context.Context, req RegisterHostRequest) (RegisterHostResponse, error) {
	var out RegisterHostResponse
	err := c.post(ctx, "/v1/hosts/register", req, &out)
	return out, err
}

func (c *Client) SendHeartbeat(ctx context.Context, req HeartbeatRequest) (HeartbeatResponse, error) {
	var out HeartbeatResponse
	err := c.post(ctx, "/v1/hosts/heartbeat", req, &out)
	return out, err
}

func (c *Client) post(ctx context.Context, path string, in any, out any) error {
	payload, err := json.Marshal(in)
	if err != nil {
		return fmt.Errorf("encode request: %w", err)
	}

	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL+path, bytes.NewReader(payload))
	if err != nil {
		return fmt.Errorf("create request: %w", err)
	}
	httpReq.Header.Set("Content-Type", "application/json")

	resp, err := c.httpClient.Do(httpReq)
	if err != nil {
		return fmt.Errorf("coordinator request failed: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return fmt.Errorf("read coordinator response: %w", err)
	}

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return responseError(resp.StatusCode, body)
	}

	if err := json.Unmarshal(body, out); err != nil {
		return fmt.Errorf("decode coordinator response: %w", err)
	}

	return nil
}

func responseError(statusCode int, body []byte) error {
	var apiErr apiError
	if err := json.Unmarshal(body, &apiErr); err == nil && apiErr.Message != "" {
		return fmt.Errorf("coordinator rejected request (%d): %s", statusCode, apiErr.Message)
	}

	text := strings.TrimSpace(string(body))
	if text == "" {
		return fmt.Errorf("coordinator rejected request (%d)", statusCode)
	}

	return fmt.Errorf("coordinator rejected request (%d): %s", statusCode, text)
}

func ValidStatus(status string) bool {
	switch status {
	case "online", "standby", "hosting", "unhealthy", "offline":
		return true
	default:
		return false
	}
}

func ValidateHeartbeatRequest(req HeartbeatRequest) error {
	switch {
	case req.GroupID == "":
		return errors.New("group ID is required")
	case req.HostID == "":
		return errors.New("host ID is required")
	case req.HostToken == "":
		return errors.New("host token is required")
	case !ValidStatus(req.Status):
		return fmt.Errorf("invalid status %q", req.Status)
	default:
		return nil
	}
}
