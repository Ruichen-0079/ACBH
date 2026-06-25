package desktop

import (
	"context"
	"encoding/json"
	"errors"
	"reflect"
	"strings"
	"time"
)

const DesktopProtocolVersion = 2

type OperationOutcome string

const (
	OutcomeSuccess             OperationOutcome = "success"
	OutcomeSuccessWithWarnings OperationOutcome = "success_with_warnings"
	OutcomePartialFailure      OperationOutcome = "partial_failure"
	OutcomeFailure             OperationOutcome = "failure"
	OutcomeCancelled           OperationOutcome = "cancelled"
	OutcomeTimedOut            OperationOutcome = "timed_out"
)

type Envelope struct {
	SchemaVersion int              `json:"schemaVersion"`
	OK            bool             `json:"ok"`
	Outcome       OperationOutcome `json:"outcome"`
	ErrorCode     string           `json:"errorCode"`
	Message       string           `json:"message"`
	Warnings      []string         `json:"warnings"`
	Data          any              `json:"data,omitempty"`
	TraceID       string           `json:"traceId"`
	StartedAt     time.Time        `json:"startedAt"`
	CompletedAt   time.Time        `json:"completedAt"`
}

func SuccessEnvelope(traceID string, startedAt time.Time, data any) Envelope {
	warnings := extractStringSliceField(data, "Warnings")
	outcome := OutcomeSuccess
	if len(warnings) > 0 {
		outcome = OutcomeSuccessWithWarnings
	}
	return Envelope{
		SchemaVersion: DesktopProtocolVersion,
		OK:            true,
		Outcome:       outcome,
		Message:       extractStringField(data, "Message"),
		Warnings:      warnings,
		Data:          data,
		TraceID:       traceID,
		StartedAt:     startedAt,
		CompletedAt:   time.Now().UTC(),
	}
}

func FailureEnvelope(traceID string, startedAt time.Time, outcome OperationOutcome, errorCode string, err error) Envelope {
	if outcome == "" {
		outcome = OutcomeFailure
	}
	message := ""
	if err != nil {
		message = err.Error()
	}
	return Envelope{
		SchemaVersion: DesktopProtocolVersion,
		OK:            false,
		Outcome:       outcome,
		ErrorCode:     errorCode,
		Message:       message,
		Warnings:      []string{},
		TraceID:       traceID,
		StartedAt:     startedAt,
		CompletedAt:   time.Now().UTC(),
	}
}

func envelopeFromResult(traceID string, startedAt time.Time, data any, err error) Envelope {
	if err != nil {
		return FailureEnvelope(traceID, startedAt, OutcomeFailure, inferErrorCode(err), err)
	}
	ok, hasOK := extractBoolField(data, "OK")
	if hasOK && !ok {
		code := extractStringField(data, "ErrorCode")
		if code == "" {
			code = "operation_failed"
		}
		msg := extractStringField(data, "Message")
		if msg == "" {
			msg = "operation failed"
		}
		return Envelope{
			SchemaVersion: DesktopProtocolVersion,
			OK:            false,
			Outcome:       OutcomeFailure,
			ErrorCode:     code,
			Message:       msg,
			Warnings:      extractStringSliceField(data, "Warnings"),
			Data:          data,
			TraceID:       traceID,
			StartedAt:     startedAt,
			CompletedAt:   time.Now().UTC(),
		}
	}
	return SuccessEnvelope(traceID, startedAt, data)
}

func inferErrorCode(err error) string {
	if err == nil {
		return ""
	}
	text := strings.ToLower(err.Error())
	switch {
	case errors.Is(err, context.Canceled), strings.Contains(text, "canceled"), strings.Contains(text, "cancelled"):
		return "cancelled"
	case strings.Contains(text, "deadline exceeded"), strings.Contains(text, "timed out"), strings.Contains(text, "timeout"):
		return "timed_out"
	case strings.Contains(text, "unsupported_capability"):
		return "unsupported_capability"
	case strings.Contains(text, "identity_mismatch"):
		return "identity_mismatch"
	case strings.Contains(text, "lease_expired"):
		return "lease_expired"
	default:
		return "operation_failed"
	}
}

func extractStringField(data any, name string) string {
	if data == nil {
		return ""
	}
	v := reflect.Indirect(reflect.ValueOf(data))
	if !v.IsValid() || v.Kind() != reflect.Struct {
		return ""
	}
	f := v.FieldByName(name)
	if f.IsValid() && f.Kind() == reflect.String {
		return f.String()
	}
	return ""
}

func extractBoolField(data any, name string) (bool, bool) {
	if data == nil {
		return false, false
	}
	v := reflect.Indirect(reflect.ValueOf(data))
	if !v.IsValid() || v.Kind() != reflect.Struct {
		return false, false
	}
	f := v.FieldByName(name)
	if f.IsValid() && f.Kind() == reflect.Bool {
		return f.Bool(), true
	}
	return false, false
}

func extractStringSliceField(data any, name string) []string {
	if data == nil {
		return []string{}
	}
	v := reflect.Indirect(reflect.ValueOf(data))
	if !v.IsValid() || v.Kind() != reflect.Struct {
		return []string{}
	}
	f := v.FieldByName(name)
	if !f.IsValid() || f.Kind() != reflect.Slice || f.Type().Elem().Kind() != reflect.String {
		return []string{}
	}
	out := make([]string, 0, f.Len())
	for i := 0; i < f.Len(); i++ {
		out = append(out, f.Index(i).String())
	}
	return out
}

func marshalEnvelopeDebug(env Envelope) []byte {
	data, err := json.Marshal(env)
	if err != nil {
		return []byte(`{"schemaVersion":2,"ok":false,"outcome":"failure","errorCode":"debug_marshal_failed"}`)
	}
	return data
}
