package hobbyagent

import "errors"

const (
	CodeConfigLockedWhileRunning = "CONFIG_LOCKED_WHILE_RUNNING"
	CodeLocalPortInUse           = "LOCAL_PORT_IN_USE"
	CodeLocalMCNotListening      = "LOCAL_MC_PORT_NOT_LISTENING"
	CodePublicPortInUse          = "PUBLIC_PORT_IN_USE"
)

type CodedError struct {
	Code    string
	Message string
	Cause   error
}

func (e *CodedError) Error() string { return e.Message }
func (e *CodedError) Unwrap() error { return e.Cause }

func errorCode(err error) string {
	var coded *CodedError
	if errors.As(err, &coded) {
		return coded.Code
	}
	return ""
}

func ErrorCode(err error) string { return errorCode(err) }
