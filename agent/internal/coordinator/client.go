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
	GroupID               string            `json:"groupId"`
	HostID                string            `json:"hostId"`
	HostToken             string            `json:"hostToken"`
	Status                string            `json:"status"`
	LatestLocalSnapshotID *string           `json:"latestLocalSnapshotId"`
	LatestLocalArtifacts  map[string]string `json:"latestLocalArtifacts,omitempty"`
	HostScoreHints        *HostScoreHints   `json:"hostScoreHints,omitempty"`
	Connection            *HostConnection   `json:"connection,omitempty"`
}

type HostScoreHints struct {
	CPUCores         int   `json:"cpuCores,omitempty"`
	MemoryTotalBytes int64 `json:"memoryTotalBytes,omitempty"`
	DiskFreeBytes    int64 `json:"diskFreeBytes,omitempty"`
	JavaAvailable    *bool `json:"javaAvailable,omitempty"`
}

type HostConnection struct {
	Host    string `json:"host"`
	Port    int    `json:"port"`
	Network string `json:"network"`
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
	Size   int64  `json:"size"`
}

type UploadManifestRequest struct {
	GroupID        string                `json:"groupId"`
	HostID         string                `json:"hostId"`
	HostToken      string                `json:"hostToken"`
	ArtifactKind   manifest.ArtifactKind `json:"artifactKind"`
	ArtifactID     string                `json:"artifactId"`
	Manifest       manifest.Manifest     `json:"manifest"`
	HostGeneration *int                  `json:"-"`
}

type UploadManifestResponse struct {
	OK           bool                  `json:"ok"`
	ArtifactKind manifest.ArtifactKind `json:"artifactKind"`
	ArtifactID   string                `json:"artifactId"`
	Status       string                `json:"status"`
}

type ArtifactAuth struct {
	GroupID   string
	HostID    string
	HostToken string
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

type ElectionAuthRequest struct {
	GroupID   string `json:"groupId"`
	HostID    string `json:"hostId"`
	HostToken string `json:"hostToken"`
}

type ElectionCandidate struct {
	HostID               string            `json:"hostId"`
	Eligible             bool              `json:"eligible"`
	Score                int               `json:"score"`
	Reasons              []string          `json:"reasons"`
	LatestLocalArtifacts map[string]string `json:"latestLocalArtifacts"`
	LastHeartbeatAt      *string           `json:"lastHeartbeatAt"`
}

type ElectionResult struct {
	ElectionID            string              `json:"electionId"`
	GroupID               string              `json:"groupId"`
	Reason                string              `json:"reason"`
	SelectedHostID        *string             `json:"selectedHostId"`
	CurrentHostGeneration int                 `json:"currentHostGeneration"`
	AssignmentID          *string             `json:"assignmentId"`
	Candidates            []ElectionCandidate `json:"candidates"`
	CreatedAt             string              `json:"createdAt"`
}

type TakeoverAssignment struct {
	AssignmentID                string            `json:"assignmentId"`
	GroupID                     string            `json:"groupId"`
	HostID                      string            `json:"hostId"`
	Reason                      string            `json:"reason"`
	Status                      string            `json:"status"`
	TakeoverToken               string            `json:"takeoverToken,omitempty"`
	CurrentHostGeneration       int               `json:"currentHostGeneration"`
	LatestArtifactsAtAssignment map[string]string `json:"latestArtifactsAtAssignment"`
	CreatedAt                   string            `json:"createdAt"`
	ExpiresAt                   string            `json:"expiresAt"`
	AcceptedAt                  *string           `json:"acceptedAt"`
	CompletedAt                 *string           `json:"completedAt"`
	FailedAt                    *string           `json:"failedAt"`
	FailureReason               *string           `json:"failureReason"`
}

type ElectionStatusResponse struct {
	GroupID                  string              `json:"groupId"`
	CurrentHostID            *string             `json:"currentHostId"`
	CurrentHostGeneration    int                 `json:"currentHostGeneration"`
	LastElection             *ElectionResult     `json:"lastElection"`
	ActiveTakeoverAssignment *TakeoverAssignment `json:"activeTakeoverAssignment"`
}

type ElectionRunResponse struct {
	OK             bool                `json:"ok"`
	GroupID        string              `json:"groupId"`
	SelectedHostID *string             `json:"selectedHostId"`
	Candidates     []ElectionCandidate `json:"candidates"`
	Election       ElectionResult      `json:"election"`
	Assignment     *TakeoverAssignment `json:"assignment"`
}

type ElectionCheckTimeoutResponse struct {
	ElectionNeeded bool                 `json:"electionNeeded"`
	Election       *ElectionRunResponse `json:"election"`
}

type TakeoverPollResponse struct {
	Assignment *TakeoverAssignment `json:"assignment"`
}

type TakeoverPollRequest struct {
	ElectionAuthRequest
	DryRun bool `json:"dryRun,omitempty"`
}

type TakeoverActionRequest struct {
	GroupID       string `json:"groupId"`
	HostID        string `json:"hostId"`
	HostToken     string `json:"hostToken"`
	AssignmentID  string `json:"assignmentId"`
	TakeoverToken string `json:"takeoverToken"`
}

type TakeoverFailRequest struct {
	TakeoverActionRequest
	FailureReason string `json:"failureReason"`
}

type TakeoverActionResponse struct {
	OK         bool               `json:"ok"`
	Assignment TakeoverAssignment `json:"assignment"`
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
			Timeout: 5 * time.Minute,
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

func (c *Client) GetElectionStatus(ctx context.Context, auth ArtifactAuth) (ElectionStatusResponse, error) {
	var out ElectionStatusResponse
	err := c.get(ctx, "/v1/groups/"+url.PathEscape(auth.GroupID)+"/election/status", auth, &out)
	return out, err
}

func (c *Client) CheckElectionTimeout(ctx context.Context, req ElectionAuthRequest) (ElectionCheckTimeoutResponse, error) {
	var out ElectionCheckTimeoutResponse
	err := c.post(ctx, "/v1/groups/"+url.PathEscape(req.GroupID)+"/election/check-timeout", req, &out)
	return out, err
}

func (c *Client) PollTakeover(ctx context.Context, req TakeoverPollRequest) (TakeoverPollResponse, error) {
	var out TakeoverPollResponse
	err := c.post(ctx, "/v1/hosts/takeover/poll", req, &out)
	return out, err
}

func (c *Client) AcceptTakeover(ctx context.Context, req TakeoverActionRequest) (TakeoverActionResponse, error) {
	var out TakeoverActionResponse
	err := c.post(ctx, "/v1/hosts/takeover/accept", req, &out)
	return out, err
}

func (c *Client) CompleteTakeover(ctx context.Context, req TakeoverActionRequest) (TakeoverActionResponse, error) {
	var out TakeoverActionResponse
	err := c.post(ctx, "/v1/hosts/takeover/complete", req, &out)
	return out, err
}

func (c *Client) FailTakeover(ctx context.Context, req TakeoverFailRequest) (TakeoverActionResponse, error) {
	var out TakeoverActionResponse
	err := c.post(ctx, "/v1/hosts/takeover/fail", req, &out)
	return out, err
}

func (c *Client) UploadObject(ctx context.Context, req UploadObjectRequest) (UploadObjectResponse, error) {
	var out UploadObjectResponse
	err := c.post(ctx, "/v1/artifacts/objects", req, &out)
	return out, err
}

func (c *Client) UploadObjectStream(
	ctx context.Context,
	auth ArtifactAuth,
	sha256 string,
	content io.Reader,
	size int64,
) (UploadObjectResponse, error) {
	httpReq, err := http.NewRequestWithContext(
		ctx,
		http.MethodPut,
		c.baseURL+"/v1/artifacts/objects/"+url.PathEscape(sha256),
		content,
	)
	if err != nil {
		return UploadObjectResponse{}, fmt.Errorf("create request: %w", err)
	}
	httpReq.Header.Set("Content-Type", "application/octet-stream")
	httpReq.Header.Set("X-ACBH-Group-ID", auth.GroupID)
	httpReq.Header.Set("X-ACBH-Host-ID", auth.HostID)
	httpReq.Header.Set("X-ACBH-Host-Token", auth.HostToken)
	if size >= 0 {
		httpReq.ContentLength = size
	}

	resp, err := c.httpClient.Do(httpReq)
	if err != nil {
		return UploadObjectResponse{}, fmt.Errorf("coordinator request failed: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(io.LimitReader(resp.Body, maxCoordinatorResponseBytes))
	if err != nil {
		return UploadObjectResponse{}, fmt.Errorf("read coordinator response: %w", err)
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return UploadObjectResponse{}, responseError(resp.StatusCode, body)
	}

	var out UploadObjectResponse
	if err := json.Unmarshal(body, &out); err != nil {
		return UploadObjectResponse{}, fmt.Errorf("decode coordinator response: %w", err)
	}
	if out.SHA256 != sha256 {
		return UploadObjectResponse{}, fmt.Errorf("coordinator returned object sha256 %q, want %q", out.SHA256, sha256)
	}
	return out, nil
}

func (c *Client) UploadManifest(ctx context.Context, req UploadManifestRequest) (UploadManifestResponse, error) {
	payload, err := json.Marshal(req)
	if err != nil {
		return UploadManifestResponse{}, fmt.Errorf("encode request: %w", err)
	}
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL+"/v1/artifacts/manifests", bytes.NewReader(payload))
	if err != nil {
		return UploadManifestResponse{}, fmt.Errorf("create request: %w", err)
	}
	httpReq.Header.Set("Content-Type", "application/json")
	if req.HostGeneration != nil {
		httpReq.Header.Set("X-ACBH-Host-Generation", fmt.Sprintf("%d", *req.HostGeneration))
	}

	resp, err := c.httpClient.Do(httpReq)
	if err != nil {
		return UploadManifestResponse{}, fmt.Errorf("coordinator request failed: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(io.LimitReader(resp.Body, maxCoordinatorResponseBytes))
	if err != nil {
		return UploadManifestResponse{}, fmt.Errorf("read coordinator response: %w", err)
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return UploadManifestResponse{}, responseError(resp.StatusCode, body)
	}

	var out UploadManifestResponse
	if err := json.Unmarshal(body, &out); err != nil {
		return UploadManifestResponse{}, fmt.Errorf("decode coordinator response: %w", err)
	}
	return out, nil
}

type GcRequest struct {
	DryRun           bool `json:"dryRun"`
	RetentionPerKind int  `json:"retentionPerKind,omitempty"`
	MinAgeMs         int  `json:"minAgeMs,omitempty"`
}

type GcDeletedArtifact struct {
	GroupID      string `json:"groupId"`
	ArtifactKind string `json:"artifactKind"`
	ArtifactID   string `json:"artifactId"`
	Status       string `json:"status"`
}

type GcResponse struct {
	DryRun               bool                `json:"dryRun"`
	DeletedArtifacts     []GcDeletedArtifact `json:"deletedArtifacts"`
	DeletedObjectCount   int                 `json:"deletedObjectCount"`
	ProtectedArtifactIds []string            `json:"protectedArtifactIds"`
}

func (c *Client) RunGC(ctx context.Context, req GcRequest, auth ArtifactAuth, generation *int) (GcResponse, error) {
	payload, err := json.Marshal(req)
	if err != nil {
		return GcResponse{}, fmt.Errorf("encode request: %w", err)
	}
	httpReq, err := http.NewRequestWithContext(
		ctx,
		http.MethodPost,
		c.baseURL+"/v1/groups/"+url.PathEscape(auth.GroupID)+"/artifacts/gc",
		bytes.NewReader(payload),
	)
	if err != nil {
		return GcResponse{}, fmt.Errorf("create request: %w", err)
	}
	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("X-ACBH-Host-ID", auth.HostID)
	httpReq.Header.Set("X-ACBH-Host-Token", auth.HostToken)
	if generation != nil {
		httpReq.Header.Set("X-ACBH-Host-Generation", fmt.Sprintf("%d", *generation))
	}

	resp, err := c.httpClient.Do(httpReq)
	if err != nil {
		return GcResponse{}, fmt.Errorf("coordinator request failed: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(io.LimitReader(resp.Body, maxCoordinatorResponseBytes))
	if err != nil {
		return GcResponse{}, fmt.Errorf("read coordinator response: %w", err)
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return GcResponse{}, responseError(resp.StatusCode, body)
	}

	var out GcResponse
	if err := json.Unmarshal(body, &out); err != nil {
		return GcResponse{}, fmt.Errorf("decode coordinator response: %w", err)
	}
	return out, nil
}

func (c *Client) GetLatestArtifact(ctx context.Context, auth ArtifactAuth, artifactKind manifest.ArtifactKind) (ArtifactMetadata, error) {
	var out ArtifactMetadata
	err := c.get(ctx, "/v1/groups/"+url.PathEscape(auth.GroupID)+"/artifacts/latest?artifactKind="+url.QueryEscape(string(artifactKind)), auth, &out)
	return out, err
}

func (c *Client) DownloadManifest(ctx context.Context, auth ArtifactAuth, artifactKind manifest.ArtifactKind, artifactID string) (DownloadManifestResponse, error) {
	var out DownloadManifestResponse
	err := c.get(ctx, "/v1/groups/"+url.PathEscape(auth.GroupID)+"/artifacts/"+url.PathEscape(string(artifactKind))+"/"+url.PathEscape(artifactID)+"/manifest", auth, &out)
	return out, err
}

func (c *Client) DownloadObjectStream(
	ctx context.Context,
	auth ArtifactAuth,
	sha256 string,
) (io.ReadCloser, int64, error) {
	httpReq, err := http.NewRequestWithContext(
		ctx,
		http.MethodGet,
		c.baseURL+"/v1/groups/"+url.PathEscape(auth.GroupID)+"/artifacts/objects/"+url.PathEscape(sha256),
		nil,
	)
	if err != nil {
		return nil, 0, fmt.Errorf("create request: %w", err)
	}
	httpReq.Header.Set("X-ACBH-Host-ID", auth.HostID)
	httpReq.Header.Set("X-ACBH-Host-Token", auth.HostToken)

	resp, err := c.httpClient.Do(httpReq)
	if err != nil {
		return nil, 0, fmt.Errorf("coordinator request failed: %w", err)
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		defer resp.Body.Close()
		body, readErr := io.ReadAll(io.LimitReader(resp.Body, maxCoordinatorResponseBytes))
		if readErr != nil {
			return nil, 0, fmt.Errorf("read coordinator response: %w", readErr)
		}
		return nil, 0, responseError(resp.StatusCode, body)
	}

	return resp.Body, resp.ContentLength, nil
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

func (c *Client) get(ctx context.Context, path string, auth ArtifactAuth, out any) error {
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodGet, c.baseURL+path, nil)
	if err != nil {
		return fmt.Errorf("create request: %w", err)
	}
	httpReq.Header.Set("X-ACBH-Host-ID", auth.HostID)
	httpReq.Header.Set("X-ACBH-Host-Token", auth.HostToken)

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
