package apperr

import (
	"errors"
	"fmt"
)

var (
	ErrInvalid       = errors.New("invalid input")
	ErrUnauthorized  = errors.New("unauthorized")
	ErrForbidden     = errors.New("forbidden")
	ErrNotFound      = errors.New("not found")
	ErrConflict      = errors.New("conflict")
	ErrExpired       = errors.New("expired")
	ErrUnavailable   = errors.New("unavailable")
	ErrVersion       = errors.New("version conflict")
	ErrInvalidState  = errors.New("invalid state transition")
	ErrSelfReview    = errors.New("submitter cannot review own evidence")
	ErrQuotaOccupied = errors.New("review slot already occupied")
)

type Error struct {
	Code    string
	Message string
	Cause   error
	Fields  map[string]string
}

func (e *Error) Error() string {
	if e.Cause == nil {
		return e.Message
	}
	return fmt.Sprintf("%s: %v", e.Message, e.Cause)
}

func (e *Error) Unwrap() error { return e.Cause }

func New(code, message string, cause error) error {
	return &Error{Code: code, Message: message, Cause: cause}
}

func WithFields(err error, fields map[string]string) error {
	var target *Error
	if errors.As(err, &target) {
		copyFields := make(map[string]string, len(target.Fields)+len(fields))
		for key, value := range target.Fields {
			copyFields[key] = value
		}
		for key, value := range fields {
			copyFields[key] = value
		}
		return &Error{Code: target.Code, Message: target.Message, Cause: target.Cause, Fields: copyFields}
	}
	return &Error{Code: "internal_error", Message: "operation failed", Cause: err, Fields: fields}
}

func Code(err error) string {
	var target *Error
	if errors.As(err, &target) && target.Code != "" {
		return target.Code
	}
	switch {
	case errors.Is(err, ErrInvalid):
		return "invalid_input"
	case errors.Is(err, ErrUnauthorized):
		return "unauthorized"
	case errors.Is(err, ErrForbidden), errors.Is(err, ErrSelfReview):
		return "forbidden"
	case errors.Is(err, ErrNotFound):
		return "not_found"
	case errors.Is(err, ErrConflict), errors.Is(err, ErrQuotaOccupied), errors.Is(err, ErrVersion):
		return "conflict"
	case errors.Is(err, ErrExpired):
		return "expired"
	case errors.Is(err, ErrUnavailable):
		return "unavailable"
	case errors.Is(err, ErrInvalidState):
		return "invalid_state"
	default:
		return "internal_error"
	}
}

func PublicMessage(err error) string {
	var target *Error
	if errors.As(err, &target) && target.Message != "" {
		return target.Message
	}
	switch Code(err) {
	case "invalid_input":
		return "请求参数无效"
	case "unauthorized":
		return "请先登录"
	case "forbidden":
		return "没有执行该操作的权限"
	case "not_found":
		return "目标记录不存在"
	case "conflict":
		return "资源状态已发生变化"
	case "expired":
		return "凭据已过期"
	case "invalid_state":
		return "当前状态不允许该操作"
	case "unavailable":
		return "服务暂时不可用"
	default:
		return "服务处理请求时发生错误"
	}
}
