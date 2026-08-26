package apperr

import (
	"errors"
	"testing"
)

func TestSentinelCodesAndMessages(t *testing.T) {
	t.Parallel()
	tests := []struct {
		err     error
		code    string
		message string
	}{
		{ErrInvalid, "invalid_input", "请求参数无效"},
		{ErrUnauthorized, "unauthorized", "请先登录"},
		{ErrForbidden, "forbidden", "没有执行该操作的权限"},
		{ErrSelfReview, "forbidden", "没有执行该操作的权限"},
		{ErrNotFound, "not_found", "目标记录不存在"},
		{ErrConflict, "conflict", "资源状态已发生变化"},
		{ErrQuotaOccupied, "conflict", "资源状态已发生变化"},
		{ErrVersion, "conflict", "资源状态已发生变化"},
		{ErrExpired, "expired", "凭据已过期"},
		{ErrUnavailable, "unavailable", "服务暂时不可用"},
		{ErrInvalidState, "invalid_state", "当前状态不允许该操作"},
		{errors.New("unknown"), "internal_error", "服务处理请求时发生错误"},
	}
	for _, test := range tests {
		if got := Code(test.err); got != test.code {
			t.Errorf("Code(%v)=%q, want %q", test.err, got, test.code)
		}
		if got := PublicMessage(test.err); got != test.message {
			t.Errorf("PublicMessage(%v)=%q, want %q", test.err, got, test.message)
		}
	}
}

func TestTypedErrorPreservesCauseAndPublicFields(t *testing.T) {
	t.Parallel()
	base := New("duplicate_source", "同一来源已经登记", ErrConflict)
	if !errors.Is(base, ErrConflict) {
		t.Fatalf("typed error lost conflict cause: %v", base)
	}
	if Code(base) != "duplicate_source" {
		t.Fatalf("typed code=%q", Code(base))
	}
	if PublicMessage(base) != "同一来源已经登记" {
		t.Fatalf("typed message=%q", PublicMessage(base))
	}
	withFields := WithFields(base, map[string]string{"fingerprint": "abc", "source_id": "10"})
	var target *Error
	if !errors.As(withFields, &target) {
		t.Fatalf("expected typed Error, got %T", withFields)
	}
	if target.Fields["fingerprint"] != "abc" || target.Fields["source_id"] != "10" {
		t.Fatalf("fields not preserved: %#v", target.Fields)
	}
	if !errors.Is(withFields, ErrConflict) {
		t.Fatal("WithFields lost original cause")
	}
}

func TestWithFieldsMergesWithoutMutatingOriginal(t *testing.T) {
	t.Parallel()
	original := &Error{Code: "invalid", Message: "invalid", Cause: ErrInvalid,
		Fields: map[string]string{"existing": "one", "overridden": "old"}}
	updated := WithFields(original, map[string]string{"added": "two", "overridden": "new"})
	var target *Error
	errors.As(updated, &target)
	if target.Fields["existing"] != "one" || target.Fields["added"] != "two" || target.Fields["overridden"] != "new" {
		t.Fatalf("unexpected merged fields: %#v", target.Fields)
	}
	if original.Fields["overridden"] != "old" {
		t.Fatalf("original fields mutated: %#v", original.Fields)
	}
	plain := errors.New("plain")
	updated = WithFields(plain, map[string]string{"operation": "test"})
	if Code(updated) != "internal_error" || !errors.Is(updated, plain) {
		t.Fatalf("plain error wrapping incorrect: %v", updated)
	}
}

func TestErrorStringIncludesCauseOnlyWhenPresent(t *testing.T) {
	t.Parallel()
	without := &Error{Code: "simple", Message: "simple message"}
	if got := without.Error(); got != "simple message" {
		t.Fatalf("Error()=%q", got)
	}
	with := &Error{Code: "wrapped", Message: "wrapped message", Cause: ErrConflict}
	if got := with.Error(); got != "wrapped message: conflict" {
		t.Fatalf("Error()=%q", got)
	}
}
