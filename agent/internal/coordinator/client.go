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
	"github.com/Ruichen-0079/ACBH/agent/internal/worldbackup"
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

type CreateGroupRequest struct {
	Name      string `json:"name"`
	OwnerName string `json:"ownerName"`
}

type CreateGroupResponse struct {
	GroupID       string `json:"groupId"`
	OwnerMemberID string `json:"ownerMemberId"`
	AccessKey     string `json:"accessKey"`
}

type JoinGroupResponse struct {
	MemberID string `json:"memberId"`
	Role     string `json:"role"`
}

type RegisterHostRequest struct {
	GroupID      string `json:"groupId"`
	AccessKey    string `json:"accessKey"`
	MemberID     string `json:"memberId"`
	DeviceName   string `json:"deviceName"`
	Platform     string `json:"platform"`
	AgentVersion string `json:"agentVersion"`
}

type RegisterHostResponse struct {
	HostID    string `json:"hostId"`
	HostToken string `json:"hostToken"`
}

type CreateInviteRequest struct {
	AccessKey        string `json:"accessKey,omitempty"`
	HostID           string `json:"hostId,omitempty"`
	HostToken        string `json:"hostToken,omitempty"`
	ExpiresInSeconds int    `json:"expiresInSeconds,omitempty"`
	OneTime          bool   `json:"oneTime,omitempty"`
}

type CreateInviteResponse struct {
	InviteID   string `json:"inviteId"`
	InviteCode string `json:"inviteCode"`
	GroupID    string `json:"groupId"`
	ExpiresAt  string `json:"expiresAt"`
	OneTime    bool   `json:"oneTime"`
}

type PublicInvite struct {
	InviteID  string  `json:"inviteId"`
	GroupID   string  `json:"groupId"`
	ExpiresAt string  `json:"expiresAt"`
	OneTime   bool    `json:"oneTime"`
	UsedAt    *string `json:"usedAt"`
	RevokedAt *string `json:"revokedAt"`
	CreatedAt string  `json:"createdAt"`
}

type ListInvitesRequest struct {
	AccessKey string `json:"accessKey,omitempty"`
	HostID    string `json:"hostId,omitempty"`
	HostToken string `json:"hostToken,omitempty"`
}

type ListInvitesResponse struct {
	Invites []PublicInvite `json:"invites"`
}

type RevokeInviteRequest struct {
	AccessKey string `json:"accessKey,omitempty"`
	HostID    string `json:"hostId,omitempty"`
	HostToken string `json:"hostToken,omitempty"`
	InviteID  string `json:"inviteId"`
}

type RevokeInviteResponse struct {
	OK       bool   `json:"ok"`
	InviteID string `json:"inviteId"`
}

type JoinInviteRequest struct {
	InviteCode   string `json:"inviteCode"`
	DisplayName  string `json:"displayName"`
	DeviceName   string `json:"deviceName"`
	Platform     string `json:"platform"`
	AgentVersion string `json:"agentVersion"`
}

type JoinInviteResponse struct {
	GroupID   string `json:"groupId"`
	MemberID  string `json:"memberId"`
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

type Capabilities struct {
	CoordinatorVersion    string   `json:"coordinatorVersion"`
	ProtocolVersion       int      `json:"protocolVersion"`
	MinimumClientProtocol int      `json:"minimumClientProtocol"`
	Capabilities          []string `json:"capabilities"`
	ServerTime            string   `json:"serverTime"`
	AuthenticationMode    string   `json:"authenticationMode"`
}

func (c Capabilities) Supports(name string) bool {
	for _, capability := range c.Capabilities {
		if capability == name {
			return true
		}
	}
	return false
}

type WhoAmIResponse struct {
	GroupID        string          `json:"groupId"`
	MemberID       string          `json:"memberId"`
	HostID         string          `json:"hostId"`
	Role           string          `json:"role"`
	CredentialKind string          `json:"credentialKind"`
	Lease          HostLeaseStatus `json:"lease"`
}

type HostLeaseStatus struct {
	GroupID              string `json:"groupId"`
	HostID               string `json:"hostId"`
	CurrentHostID        string `json:"currentHostId,omitempty"`
	CurrentHostIDMatches bool   `json:"currentHostIdMatches"`
	LeaseValid           bool   `json:"leaseValid"`
	LeaseExpiresAt       string `json:"leaseExpiresAt,omitempty"`
	LeaseRemaining       int64  `json:"leaseRemaining"`
	Generation           int    `json:"generation"`
	ServerTime           string `json:"serverTime"`
	HeartbeatActive      bool   `json:"heartbeatActive"`
}

type EnsureActiveLeaseResponse struct {
	OK      bool            `json:"ok"`
	Renewed bool            `json:"renewed"`
	Lease   HostLeaseStatus `json:"lease"`
	Message string          `json:"message"`
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

type WorldBackupPlanRequest struct {
	HostID           string                      `json:"hostId"`
	HostToken        string                      `json:"hostToken"`
	HostGeneration   int                         `json:"hostGeneration"`
	ParentSnapshotID string                      `json:"parentSnapshotId,omitempty"`
	Objects          []worldbackup.PlannedObject `json:"objects"`
}

type WorldBackupPlanResponse struct {
	OK             bool                        `json:"ok"`
	MissingObjects []worldbackup.PlannedObject `json:"missingObjects"`
	ExistingCount  int                         `json:"existingCount"`
}

type WorldBackupCommitRequest struct {
	HostID         string               `json:"hostId"`
	HostToken      string               `json:"hostToken"`
	HostGeneration int                  `json:"hostGeneration"`
	Manifest       worldbackup.Manifest `json:"manifest"`
}

type WorldBackupCommitResponse struct {
	OK         bool   `json:"ok"`
	SnapshotID string `json:"snapshotId"`
	Status     string `json:"status"`
}

type WorldBackupListResponse struct {
	Snapshots []WorldBackupMetadata `json:"snapshots"`
}

type WorldBackupMetadata struct {
	SnapshotID       string `json:"snapshotId"`
	GroupID          string `json:"groupId"`
	SourceHostID     string `json:"sourceHostId"`
	HostGeneration   int    `json:"hostGeneration"`
	CreatedAt        string `json:"createdAt"`
	Consistent       bool   `json:"consistent"`
	Pinned           bool   `json:"pinned"`
	LogicalSize      int64  `json:"logicalSize"`
	UploadedSize     int64  `json:"uploadedSize"`
	FileCount        int    `json:"fileCount"`
	ChangedFileCount int    `json:"changedFileCount"`
	DeletedFileCount int    `json:"deletedFileCount"`
}

type WorldBackupManifestResponse struct {
	Metadata WorldBackupMetadata  `json:"metadata"`
	Manifest worldbackup.Manifest `json:"manifest"`
}

type WorldBackupPinRequest struct {
	HostID    string `json:"hostId"`
	HostToken string `json:"hostToken"`
	Pinned    bool   `json:"pinned"`
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
	Code    string `json:"code"`
}

type APIError struct {
	StatusCode int
	Code       string
	Message    string
	Body       string
}

func (e *APIError) Error() string {
	if e.Code != "" {
		return fmt.Sprintf("coordinator rejected request (%d %s): %s", e.StatusCode, e.Code, e.Message)
	}
	if e.Message != "" {
		return fmt.Sprintf("coordinator rejected request (%d): %s", e.StatusCode, e.Message)
	}
	return fmt.Sprintf("coordinator rejected request (%d)", e.StatusCode)
}

func IsAPIErrorCode(err error, code string) bool {
	var apiErr *APIError
	return errors.As(err, &apiErr) && apiErr.Code == code
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

func (c *Client) Health(ctx context.Context) error {
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodGet, c.baseURL+"/health", nil)
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

	var out struct {
		OK bool `json:"ok"`
	}
	if err := json.Unmarshal(body, &out); err != nil {
		return fmt.Errorf("decode coordinator response: %w", err)
	}
	if !out.OK {
		return errors.New("coordinator health check did not return ok")
	}
	return nil
}

func (c *Client) GetCapabilities(ctx context.Context) (Capabilities, error) {
	var out Capabilities
	err := c.getNoAuth(ctx, "/v1/capabilities", &out)
	return out, err
}

func (c *Client) CreateGroup(ctx context.Context, req CreateGroupRequest) (CreateGroupResponse, error) {
	var out CreateGroupResponse
	err := c.post(ctx, "/v1/groups", req, &out)
	return out, err
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

func (c *Client) CreateInvite(ctx context.Context, groupID string, req CreateInviteRequest) (CreateInviteResponse, error) {
	var out CreateInviteResponse
	err := c.post(ctx, "/v1/groups/"+url.PathEscape(groupID)+"/invites", req, &out)
	return out, err
}

func (c *Client) ListInvites(ctx context.Context, groupID string, req ListInvitesRequest) (ListInvitesResponse, error) {
	var out ListInvitesResponse
	err := c.post(ctx, "/v1/groups/"+url.PathEscape(groupID)+"/invites/list", req, &out)
	return out, err
}

func (c *Client) RevokeInvite(ctx context.Context, groupID string, req RevokeInviteRequest) (RevokeInviteResponse, error) {
	var out RevokeInviteResponse
	err := c.post(ctx, "/v1/groups/"+url.PathEscape(groupID)+"/invites/revoke", req, &out)
	return out, err
}

func (c *Client) JoinInvite(ctx context.Context, req JoinInviteRequest) (JoinInviteResponse, error) {
	var out JoinInviteResponse
	err := c.post(ctx, "/v1/invites/join", req, &out)
	return out, err
}

func (c *Client) SendHeartbeat(ctx context.Context, req HeartbeatRequest) (HeartbeatResponse, error) {
	var out HeartbeatResponse
	err := c.post(ctx, "/v1/hosts/heartbeat", req, &out)
	return out, err
}

func (c *Client) WhoAmI(ctx context.Context, auth ArtifactAuth) (WhoAmIResponse, error) {
	var out WhoAmIResponse
	err := c.get(ctx, "/v1/groups/"+url.PathEscape(auth.GroupID)+"/whoami", auth, &out)
	return out, err
}

func (c *Client) GetLeaseStatus(ctx context.Context, auth ArtifactAuth) (HostLeaseStatus, error) {
	var out HostLeaseStatus
	err := c.get(ctx, "/v1/groups/"+url.PathEscape(auth.GroupID)+"/lease/status", auth, &out)
	return out, err
}

func (c *Client) EnsureActiveLease(ctx context.Context, auth ArtifactAuth, generation *int) (EnsureActiveLeaseResponse, error) {
	body := struct {
		GroupID    string `json:"groupId"`
		HostID     string `json:"hostId"`
		HostToken  string `json:"hostToken"`
		Generation *int   `json:"generation,omitempty"`
	}{GroupID: auth.GroupID, HostID: auth.HostID, HostToken: auth.HostToken, Generation: generation}
	var out EnsureActiveLeaseResponse
	err := c.post(ctx, "/v1/groups/"+url.PathEscape(auth.GroupID)+"/lease/ensure-active", body, &out)
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
	// UploadObjectStream does not take ownership of content. io.NopCloser prevents the
	// HTTP transport from closing the caller's reader (for example *os.File).
	httpReq, err := http.NewRequestWithContext(
		ctx,
		http.MethodPut,
		c.baseURL+"/v1/artifacts/objects/"+url.PathEscape(sha256),
		io.NopCloser(content),
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

func (c *Client) PlanWorldBackup(ctx context.Context, groupID string, req WorldBackupPlanRequest) (WorldBackupPlanResponse, error) {
	var out WorldBackupPlanResponse
	err := c.post(ctx, "/v1/groups/"+url.PathEscape(groupID)+"/world-backups/plan", req, &out)
	return out, err
}

// UploadWorldObjectStream streams a world CAS object to the Coordinator.
// The caller retains ownership of content and is responsible for closing any *os.File.
// This method never closes the provided reader.
func (c *Client) UploadWorldObjectStream(
	ctx context.Context,
	auth ArtifactAuth,
	sha256 string,
	content io.Reader,
	size int64,
) (UploadObjectResponse, error) {
	httpReq, err := http.NewRequestWithContext(
		ctx,
		http.MethodPut,
		c.baseURL+"/v1/groups/"+url.PathEscape(auth.GroupID)+"/world-objects/"+url.PathEscape(sha256),
		io.NopCloser(content),
	)
	if err != nil {
		return UploadObjectResponse{}, fmt.Errorf("create request: %w", err)
	}
	httpReq.Header.Set("Content-Type", "application/octet-stream")
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
	return out, nil
}

func (c *Client) CommitWorldBackup(ctx context.Context, groupID string, req WorldBackupCommitRequest) (WorldBackupCommitResponse, error) {
	var out WorldBackupCommitResponse
	err := c.post(ctx, "/v1/groups/"+url.PathEscape(groupID)+"/world-backups/commit", req, &out)
	return out, err
}

func (c *Client) GetLatestWorldBackup(ctx context.Context, auth ArtifactAuth, consistentOnly bool) (WorldBackupManifestResponse, error) {
	var out WorldBackupManifestResponse
	path := "/v1/groups/" + url.PathEscape(auth.GroupID) + "/world-backups/latest"
	if consistentOnly {
		path += "?consistentOnly=true"
	}
	err := c.get(ctx, path, auth, &out)
	return out, err
}

func (c *Client) ListWorldBackups(ctx context.Context, auth ArtifactAuth) (WorldBackupListResponse, error) {
	var out WorldBackupListResponse
	err := c.get(ctx, "/v1/groups/"+url.PathEscape(auth.GroupID)+"/world-backups", auth, &out)
	return out, err
}

func (c *Client) GetWorldBackup(ctx context.Context, auth ArtifactAuth, snapshotID string) (WorldBackupManifestResponse, error) {
	var out WorldBackupManifestResponse
	err := c.get(ctx, "/v1/groups/"+url.PathEscape(auth.GroupID)+"/world-backups/"+url.PathEscape(snapshotID), auth, &out)
	return out, err
}

func (c *Client) PinWorldBackup(ctx context.Context, auth ArtifactAuth, snapshotID string, pinned bool) error {
	req := WorldBackupPinRequest{HostID: auth.HostID, HostToken: auth.HostToken, Pinned: pinned}
	var out struct {
		OK bool `json:"ok"`
	}
	return c.post(ctx, "/v1/groups/"+url.PathEscape(auth.GroupID)+"/world-backups/"+url.PathEscape(snapshotID)+"/pin", req, &out)
}

func (c *Client) DeleteWorldBackup(ctx context.Context, auth ArtifactAuth, snapshotID string) error {
	httpReq, err := http.NewRequestWithContext(
		ctx,
		http.MethodDelete,
		c.baseURL+"/v1/groups/"+url.PathEscape(auth.GroupID)+"/world-backups/"+url.PathEscape(snapshotID),
		nil,
	)
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
	return nil
}

func (c *Client) DownloadWorldObjectStream(
	ctx context.Context,
	auth ArtifactAuth,
	sha256 string,
) (io.ReadCloser, int64, error) {
	httpReq, err := http.NewRequestWithContext(
		ctx,
		http.MethodGet,
		c.baseURL+"/v1/groups/"+url.PathEscape(auth.GroupID)+"/world-objects/"+url.PathEscape(sha256),
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

type GcBlocker struct {
	GroupID      string `json:"groupId"`
	ArtifactKind string `json:"artifactKind"`
	ArtifactID   string `json:"artifactId"`
	Reason       string `json:"reason"`
}

type GcResponse struct {
	DryRun               bool                `json:"dryRun"`
	Blocked              bool                `json:"blocked"`
	Blockers             []GcBlocker         `json:"blockers"`
	DeletedArtifacts     []GcDeletedArtifact `json:"deletedArtifacts"`
	DeletedObjectCount   int                 `json:"deletedObjectCount"`
	ProtectedArtifactIds []string            `json:"protectedArtifactIds"`
}

type TunnelSession struct {
	SessionID             string `json:"sessionId"`
	GroupID               string `json:"groupId"`
	HostID                string `json:"hostId"`
	PlayerID              string `json:"playerId"`
	Mode                  string `json:"mode"`
	Status                string `json:"status"`
	CurrentHostGeneration int    `json:"currentHostGeneration"`
	CreatedAt             string `json:"createdAt"`
	ExpiresAt             string `json:"expiresAt"`
}

func (c *Client) ListTunnelSessions(ctx context.Context, groupID string) ([]TunnelSession, error) {
	if groupID == "" {
		return nil, fmt.Errorf("groupID required")
	}
	url := fmt.Sprintf("%s/v1/groups/%s/tunnel-sessions", c.baseURL, groupID)
	req, err := http.NewRequestWithContext(ctx, "GET", url, nil)
	if err != nil {
		return nil, err
	}
	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		b, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("list tunnel sessions: %s: %s", resp.Status, string(b))
	}
	var list []TunnelSession
	if err := json.NewDecoder(resp.Body).Decode(&list); err != nil {
		return nil, err
	}
	return list, nil
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

func (c *Client) getNoAuth(ctx context.Context, path string, out any) error {
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
		return &APIError{StatusCode: statusCode, Code: apiErr.Code, Message: apiErr.Message, Body: string(body)}
	}

	text := strings.TrimSpace(string(body))
	if text == "" {
		return &APIError{StatusCode: statusCode}
	}

	return &APIError{StatusCode: statusCode, Message: text, Body: text}
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
