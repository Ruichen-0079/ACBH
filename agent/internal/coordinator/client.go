package coordinator

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/Ruichen-0079/ACBH/agent/internal/manifest"
)

const maxCoordinatorResponseBytes = 32 * 1024 * 1024

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

type UploadObjectRequest struct {
	GroupID       string `json:"groupId"`
	HostID        string `json:"hostId"`
	HostToken     string `json:"hostToken"`
	SHA256        string `json:"sha256"`
	ContentBase64 string `json:"contentBase64"`
}

type UploadObjectResponse struct {
	OK     bool   `json:"ok"`
	SHA256 string `json:"sha256"`
	Exists bool   `json:"exists"`
}

type UploadManifestRequest struct {
	GroupID      string                `json:"groupId"`
	HostID       string                `json:"hostId"`
	HostToken    string                `json:"hostToken"`
	ArtifactKind manifest.ArtifactKind `json:"artifactKind"`
	ArtifactID   string                `json:"artifactId"`
	Manifest     manifest.Manifest     `json:"manifest"`
}

type UploadManifestResponse struct {
	OK           bool                  `json:"ok"`
	ArtifactKind manifest.ArtifactKind `json:"artifactKind"`
	ArtifactID   string                `json:"artifactId"`
	Status       string                `json:"status"`
}

type ArtifactMetadata struct {
	GroupID            string                `json:"groupId"`
	ArtifactKind       manifest.ArtifactKind `json:"artifactKind"`
	ArtifactID         string                `json:"artifactId"`
	ParentArtifactID   *string               `json:"parentArtifactId"`
	ServerPackVersion  *string               `json:"serverPackVersion"`
	CreatorHostID      string                `json:"creatorHostId"`
	CreatedAt          string                `json:"createdAt"`
	UpdatedAt          string                `json:"updatedAt"`
	Status             string                `json:"status"`
	ManifestSHA256     string                `json:"manifestSha256"`
	ManifestObjectPath string                `json:"manifestObjectPath"`
	FileCount          int                   `json:"fileCount"`
	TotalBytes         int64                 `json:"totalBytes"`
}

type DownloadManifestResponse struct {
	Metadata ArtifactMetadata  `json:"metadata"`
	Manifest manifest.Manifest `json:"manifest"`
}

type DownloadObjectResponse struct {
	SHA256        string `json:"sha256"`
	ContentBase64 string `json:"contentBase64"`
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

func (c *Client) UploadObject(ctx context.Context, req UploadObjectRequest) (UploadObjectResponse, error) {
	var out UploadObjectResponse
	err := c.post(ctx, "/v1/artifacts/objects", req, &out)
	return out, err
}

func (c *Client) UploadManifest(ctx context.Context, req UploadManifestRequest) (UploadManifestResponse, error) {
	var out UploadManifestResponse
	err := c.post(ctx, "/v1/artifacts/manifests", req, &out)
	return out, err
}

func (c *Client) GetLatestArtifact(ctx context.Context, groupID string, artifactKind manifest.ArtifactKind) (ArtifactMetadata, error) {
	var out ArtifactMetadata
	err := c.get(ctx, "/v1/groups/"+url.PathEscape(groupID)+"/artifacts/latest?artifactKind="+url.QueryEscape(string(artifactKind)), &out)
	return out, err
}

func (c *Client) DownloadManifest(ctx context.Context, groupID string, artifactKind manifest.ArtifactKind, artifactID string) (DownloadManifestResponse, error) {
	var out DownloadManifestResponse
	err := c.get(ctx, "/v1/groups/"+url.PathEscape(groupID)+"/artifacts/"+url.PathEscape(string(artifactKind))+"/"+url.PathEscape(artifactID)+"/manifest", &out)
	return out, err
}

func (c *Client) DownloadObject(ctx context.Context, groupID string, sha256 string) ([]byte, error) {
	var out DownloadObjectResponse
	if err := c.get(ctx, "/v1/groups/"+url.PathEscape(groupID)+"/artifacts/objects/"+url.PathEscape(sha256), &out); err != nil {
		return nil, err
	}
	if out.SHA256 != sha256 {
		return nil, fmt.Errorf("coordinator returned object sha256 %q, want %q", out.SHA256, sha256)
	}
	content, err := base64.StdEncoding.DecodeString(out.ContentBase64)
	if err != nil {
		return nil, fmt.Errorf("decode object content: %w", err)
	}
	return content, nil
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

	body, err := io.ReadAll(io.LimitReader(resp.Body, maxCoordinatorResponseBytes))
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

func (c *Client) get(ctx context.Context, path string, out any) error {
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodGet, c.baseURL+path, nil)
	if err != nil {
		return fmt.Errorf("create request: %w", err)
	}

	resp, err := c.httpClient.Do(httpReq)
	if err != nil {
		return fmt.Errorf("coordinator request failed: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(io.LimitReader(resp.Body, maxCoordinatorResponseBytes))
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
