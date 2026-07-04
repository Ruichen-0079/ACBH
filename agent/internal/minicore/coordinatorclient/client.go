package coordinatorclient

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/Ruichen-0079/ACBH/agent/internal/minicore/coreerrors"
)

const maxResponseBytes = 32 * 1024 * 1024

type Client struct {
	baseURL string
	http    *http.Client
}

type Health struct {
	OK                    bool     `json:"ok"`
	Version               string   `json:"version,omitempty"`
	CoordinatorVersion    string   `json:"coordinatorVersion,omitempty"`
	ProtocolVersion       int      `json:"protocolVersion,omitempty"`
	MinimumClientProtocol int      `json:"minimumClientProtocol,omitempty"`
	Capabilities          []string `json:"capabilities,omitempty"`
	AuthenticationMode    string   `json:"authenticationMode,omitempty"`
}

type Capabilities struct {
	CoordinatorVersion    string   `json:"coordinatorVersion"`
	ProtocolVersion       int      `json:"protocolVersion"`
	MinimumClientProtocol int      `json:"minimumClientProtocol"`
	Capabilities          []string `json:"capabilities"`
	ServerTime            string   `json:"serverTime"`
	AuthenticationMode    string   `json:"authenticationMode"`
}

type HostConnection struct {
	Host    string `json:"host"`
	Port    int    `json:"port"`
	Network string `json:"network"`
}

type HeartbeatRequest struct {
	GroupID    string          `json:"groupId"`
	HostID     string          `json:"hostId"`
	HostToken  string          `json:"hostToken"`
	Status     string          `json:"status"`
	Connection *HostConnection `json:"connection,omitempty"`
}

type HeartbeatResponse struct {
	OK     bool   `json:"ok"`
	HostID string `json:"hostId"`
	Status string `json:"status"`
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

type RouteProbe struct {
	Method       string               `json:"method"`
	Path         string               `json:"path"`
	URL          string               `json:"url"`
	HTTPStatus   int                  `json:"httpStatus"`
	ErrorCode    coreerrors.ErrorCode `json:"errorCode,omitempty"`
	RouteMissing bool                 `json:"routeMissing"`
	ResponseBody string               `json:"responseBody,omitempty"`
}

type ProbeResult struct {
	CoordinatorURL         string        `json:"coordinatorUrl"`
	ActualRequestURL       string        `json:"actualRequestUrl"`
	Health                 *Health       `json:"health,omitempty"`
	Capabilities           *Capabilities `json:"capabilities,omitempty"`
	ProtocolOK             bool          `json:"protocolOk"`
	CapabilitiesOK         bool          `json:"capabilitiesOk"`
	CapabilityRouteMissing bool          `json:"capabilityRouteMissing"`
	RouteProbes            []RouteProbe  `json:"routeProbes"`
}

func New(baseURL string) (*Client, *coreerrors.Error) {
	return NewWithHTTPClient(baseURL, nil)
}

func NewWithHTTPClient(baseURL string, httpClient *http.Client) (*Client, *coreerrors.Error) {
	baseURL = strings.TrimRight(strings.TrimSpace(baseURL), "/")
	parsed, err := url.Parse(baseURL)
	if err != nil || parsed.Scheme == "" || parsed.Host == "" {
		return nil, coreerrors.New(coreerrors.ConfigInvalid, "invalid coordinatorUrl", coreerrors.Details{CoordinatorURL: baseURL}, "Use a full URL such as http://121.40.101.224:6121.")
	}
	if httpClient == nil {
		httpClient = &http.Client{Timeout: 15 * time.Second}
	}
	return &Client{baseURL: baseURL, http: httpClient}, nil
}

func (c *Client) Health(ctx context.Context) (Health, *coreerrors.Error) {
	var out Health
	err := c.doJSON(ctx, http.MethodGet, "/health", nil, nil, &out)
	return out, err
}

func (c *Client) Capabilities(ctx context.Context) (Capabilities, *coreerrors.Error) {
	var out Capabilities
	err := c.doJSON(ctx, http.MethodGet, "/v1/capabilities", nil, nil, &out)
	return out, err
}

func (c *Client) WhoAmI(ctx context.Context, groupID string, hostID string, hostToken string) (map[string]any, *coreerrors.Error) {
	headers := authHeaders(hostID, hostToken)
	var out map[string]any
	err := c.doJSON(ctx, http.MethodGet, "/v1/groups/"+url.PathEscape(groupID)+"/whoami", nil, headers, &out)
	return out, err
}

func (c *Client) LeaseStatus(ctx context.Context, groupID string, hostID string, hostToken string) (map[string]any, *coreerrors.Error) {
	headers := authHeaders(hostID, hostToken)
	var out map[string]any
	err := c.doJSON(ctx, http.MethodGet, "/v1/groups/"+url.PathEscape(groupID)+"/lease/status", nil, headers, &out)
	return out, err
}

func (c *Client) GetLeaseStatus(ctx context.Context, groupID string, hostID string, hostToken string) (HostLeaseStatus, *coreerrors.Error) {
	headers := authHeaders(hostID, hostToken)
	var out HostLeaseStatus
	err := c.doJSON(ctx, http.MethodGet, "/v1/groups/"+url.PathEscape(groupID)+"/lease/status", nil, headers, &out)
	return out, err
}

func (c *Client) EnsureActiveLease(ctx context.Context, groupID string, hostID string, hostToken string) (EnsureActiveLeaseResponse, *coreerrors.Error) {
	body := struct {
		GroupID   string `json:"groupId"`
		HostID    string `json:"hostId"`
		HostToken string `json:"hostToken"`
	}{GroupID: groupID, HostID: hostID, HostToken: hostToken}
	var out EnsureActiveLeaseResponse
	err := c.doJSON(ctx, http.MethodPost, "/v1/groups/"+url.PathEscape(groupID)+"/lease/ensure-active", body, nil, &out)
	return out, err
}

func (c *Client) SendHeartbeat(ctx context.Context, req HeartbeatRequest) (HeartbeatResponse, *coreerrors.Error) {
	var out HeartbeatResponse
	err := c.doJSON(ctx, http.MethodPost, "/v1/hosts/heartbeat", req, nil, &out)
	return out, err
}

func (c *Client) Probe(ctx context.Context) (ProbeResult, *coreerrors.Error) {
	result := ProbeResult{CoordinatorURL: c.baseURL, ActualRequestURL: c.baseURL + "/health"}
	health, healthErr := c.Health(ctx)
	if healthErr != nil {
		return result, healthErr
	}
	result.Health = &health
	caps, capsErr := c.Capabilities(ctx)
	if capsErr != nil {
		return result, capsErr
	}
	result.Capabilities = &caps
	result.ProtocolOK = caps.ProtocolVersion == 2
	result.CapabilitiesOK = hasRequiredCapabilities(caps.Capabilities)
	if !result.ProtocolOK {
		return result, coreerrors.New(coreerrors.CoordinatorProtocolMismatch, "coordinator protocolVersion is not 2", coreerrors.Details{CoordinatorURL: c.baseURL}, "Upgrade or point to a protocolVersion=2 coordinator.")
	}
	if !result.CapabilitiesOK {
		return result, coreerrors.New(coreerrors.CoordinatorCapabilityMissing, "coordinator capabilities are incomplete", coreerrors.Details{CoordinatorURL: c.baseURL}, "Use a coordinator with alpha6/hotfix2 capabilities.")
	}
	result.RouteProbes = c.routeProbes(ctx)
	for _, probe := range result.RouteProbes {
		if probe.RouteMissing {
			result.CapabilityRouteMissing = true
			break
		}
	}
	return result, nil
}

func (c *Client) routeProbes(ctx context.Context) []RouteProbe {
	requests := []struct {
		method string
		path   string
		body   any
	}{
		{http.MethodGet, "/v1/groups/grp_test/whoami", nil},
		{http.MethodGet, "/v1/groups/grp_test/lease/status", nil},
		{http.MethodPost, "/v1/groups/grp_test/lease/ensure-active", map[string]any{}},
		{http.MethodGet, "/v1/groups/grp_test/members", nil},
	}
	probes := make([]RouteProbe, 0, len(requests))
	for _, item := range requests {
		status, body, err := c.doRaw(ctx, item.method, item.path, item.body, nil)
		probe := RouteProbe{Method: item.method, Path: item.path, URL: c.baseURL + item.path, HTTPStatus: status, ResponseBody: body}
		if err != nil {
			probe.ErrorCode = err.ErrorCode
		} else {
			probe.ErrorCode = classifyStatus(status, body)
		}
		probe.RouteMissing = status == http.StatusNotFound
		probes = append(probes, probe)
	}
	return probes
}

func (c *Client) doJSON(ctx context.Context, method string, path string, body any, headers map[string]string, out any) *coreerrors.Error {
	status, responseBody, err := c.doRaw(ctx, method, path, body, headers)
	if err != nil {
		return err
	}
	if status < 200 || status >= 300 {
		return responseError(method, c.baseURL+path, status, responseBody)
	}
	if err := json.Unmarshal([]byte(responseBody), out); err != nil {
		return coreerrors.New(coreerrors.CoordinatorUnreachable, "decode coordinator response failed", coreerrors.Details{URL: c.baseURL + path, Method: method, HTTPStatus: status, ResponseBody: responseBody}, err.Error())
	}
	return nil
}

func (c *Client) doRaw(ctx context.Context, method string, path string, body any, headers map[string]string) (int, string, *coreerrors.Error) {
	var reader io.Reader
	if body != nil {
		data, err := json.Marshal(body)
		if err != nil {
			return 0, "", coreerrors.New(coreerrors.InvalidRequest, "encode request failed", coreerrors.Details{URL: c.baseURL + path, Method: method}, err.Error())
		}
		reader = bytes.NewReader(data)
	}
	req, err := http.NewRequestWithContext(ctx, method, c.baseURL+path, reader)
	if err != nil {
		return 0, "", coreerrors.New(coreerrors.InvalidRequest, "create request failed", coreerrors.Details{URL: c.baseURL + path, Method: method}, err.Error())
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	for key, value := range headers {
		req.Header.Set(key, value)
	}
	resp, err := c.http.Do(req)
	if err != nil {
		return 0, "", transportError(err, method, c.baseURL+path)
	}
	defer resp.Body.Close()
	data, readErr := io.ReadAll(io.LimitReader(resp.Body, maxResponseBytes))
	if readErr != nil {
		return resp.StatusCode, "", coreerrors.New(coreerrors.CoordinatorUnreachable, "read coordinator response failed", coreerrors.Details{URL: c.baseURL + path, Method: method, HTTPStatus: resp.StatusCode}, readErr.Error())
	}
	return resp.StatusCode, string(data), nil
}

func responseError(method string, rawURL string, status int, body string) *coreerrors.Error {
	code := classifyStatus(status, body)
	message := "coordinator request failed"
	switch code {
	case coreerrors.CoordinatorRouteMissing:
		message = "VPS Coordinator 接口探测失败。请查看实际请求 URL、HTTP 状态和响应体。"
	case coreerrors.AuthMissing:
		message = "host authentication is required"
	case coreerrors.LeaseExpired:
		message = "当前设备的 VPS 会话已过期，需要重新验证。"
	case coreerrors.NotCurrentHost:
		message = "当前设备不是此私人实例的活动设备。"
	case coreerrors.InvalidRequest:
		message = "coordinator rejected invalid request"
	case coreerrors.ProxyInterferenceSuspected:
		message = "proxy interference suspected"
	}
	return coreerrors.New(code, message, coreerrors.Details{URL: rawURL, Method: method, HTTPStatus: status, ResponseBody: body}, "Check the URL, status and responseBody in details.")
}

func classifyStatus(status int, body string) coreerrors.ErrorCode {
	lower := strings.ToLower(body)
	switch {
	case status == http.StatusNotFound && (strings.Contains(lower, "route_not_found") || strings.Contains(lower, "coordinator_capability_route_missing") || strings.Contains(lower, "not found")):
		return coreerrors.CoordinatorRouteMissing
	case status == http.StatusUnauthorized && strings.Contains(lower, "host_auth_required"):
		return coreerrors.AuthMissing
	case status == http.StatusForbidden && strings.Contains(lower, "host_lease_expired"):
		return coreerrors.LeaseExpired
	case status == http.StatusForbidden && strings.Contains(lower, "not_current_host"):
		return coreerrors.NotCurrentHost
	case status == http.StatusBadRequest:
		return coreerrors.InvalidRequest
	case status == http.StatusBadGateway && strings.Contains(lower, "proxy-connection"):
		return coreerrors.ProxyInterferenceSuspected
	case status == http.StatusUnauthorized:
		return coreerrors.AuthMissing
	case status == http.StatusForbidden:
		return coreerrors.AuthInvalid
	default:
		return coreerrors.CoordinatorUnreachable
	}
}

func transportError(err error, method string, rawURL string) *coreerrors.Error {
	var netErr net.Error
	code := coreerrors.CoordinatorUnreachable
	if errors.As(err, &netErr) && netErr.Timeout() {
		code = coreerrors.OperationTimeout
	}
	return coreerrors.New(code, "coordinator request failed", coreerrors.Details{URL: rawURL, Method: method}, err.Error())
}

func authHeaders(hostID string, hostToken string) map[string]string {
	return map[string]string{
		"X-ACBH-Host-ID":    hostID,
		"X-ACBH-Host-Token": hostToken,
	}
}

func hasRequiredCapabilities(capabilities []string) bool {
	required := []string{"lease_renew_v1", "world_backup_v1", "group_whoami_v1"}
	set := map[string]bool{}
	for _, capability := range capabilities {
		set[capability] = true
	}
	for _, capability := range required {
		if !set[capability] {
			return false
		}
	}
	return true
}

func (c *Client) String() string {
	return fmt.Sprintf("coordinator %s", c.baseURL)
}
