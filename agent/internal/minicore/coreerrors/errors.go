package coreerrors

import "fmt"

type ErrorCode string

const (
	ConfigParseError             ErrorCode = "config_parse_error"
	ConfigMissing                ErrorCode = "config_missing"
	ConfigInvalid                ErrorCode = "config_invalid"
	CoordinatorUnreachable       ErrorCode = "coordinator_unreachable"
	CoordinatorProtocolMismatch  ErrorCode = "coordinator_protocol_mismatch"
	CoordinatorCapabilityMissing ErrorCode = "coordinator_capability_missing"
	CoordinatorRouteMissing      ErrorCode = "coordinator_route_missing"
	AuthMissing                  ErrorCode = "auth_missing"
	AuthInvalid                  ErrorCode = "auth_invalid"
	LeaseExpired                 ErrorCode = "lease_expired"
	InvalidRequest               ErrorCode = "invalid_request"
	ProxyInterferenceSuspected   ErrorCode = "proxy_interference_suspected"
	OperationTimeout             ErrorCode = "operation_timeout"
)

type Details struct {
	URL            string `json:"url,omitempty"`
	Method         string `json:"method,omitempty"`
	HTTPStatus     int    `json:"httpStatus,omitempty"`
	ResponseBody   string `json:"responseBody,omitempty"`
	ConfigPath     string `json:"configPath,omitempty"`
	CoordinatorURL string `json:"coordinatorUrl,omitempty"`
	Path           string `json:"path,omitempty"`
	Line           int    `json:"line,omitempty"`
	Column         int    `json:"column,omitempty"`
}

type Error struct {
	ErrorCode  ErrorCode `json:"errorCode"`
	Message    string    `json:"message"`
	Details    Details   `json:"details,omitempty"`
	Suggestion string    `json:"suggestion,omitempty"`
}

func (e *Error) Error() string {
	if e == nil {
		return ""
	}
	if e.Message == "" {
		return string(e.ErrorCode)
	}
	return fmt.Sprintf("%s: %s", e.ErrorCode, e.Message)
}

func New(code ErrorCode, message string, details Details, suggestion string) *Error {
	return &Error{ErrorCode: code, Message: message, Details: details, Suggestion: suggestion}
}
