// Package service — use cases daemon (slice 1: Team Builder).
package service

import (
	"errors"
	"fmt"
)

// AppError — структурированная ошибка API (error model: docs/contracts/error-model.md).
type AppError struct {
	Code    string
	Status  int
	Message string
	Details map[string]any
}

func (e *AppError) Error() string { return e.Message }

func NewValidation(msg string, details ...any) *AppError {
	e := &AppError{Code: "validation_failed", Status: 400, Message: msg}
	if len(details) > 0 {
		e.Details = map[string]any{"errors": details[0]}
	}
	return e
}

// FieldError — одна проблема валидации.
type FieldError struct {
	Field  string `json:"field"`
	Reason string `json:"reason"`
}

func NewNotFound(what string) *AppError {
	return &AppError{Code: "not_found", Status: 404, Message: what + " not found"}
}

func NewConflict(msg string) *AppError {
	return &AppError{Code: "conflict", Status: 409, Message: msg}
}

func NewInternal(cause error) *AppError {
	return &AppError{Code: "internal", Status: 500, Message: "internal server error",
		Details: map[string]any{"cause": cause.Error()}}
}

var (
	ErrTeamNotFound     = errors.New("team not found")
	ErrSegmentNotFound  = errors.New("segment not found")
	ErrRoleNotFound     = errors.New("role not found")
	ErrRelativeNotFound = errors.New("relative not found")
)

func wrapNotFound(sentinel error, what string) error {
	if errors.Is(sentinel, errors.New("not found")) || sentinel == errors.New("not found") {
		return NewNotFound(what)
	}
	return sentinel
}

func notFound(what string) error { return NewNotFound(what) }

func conflict(msg string) error { return NewConflict(msg) }

func validationf(format string, args ...any) *AppError {
	return &AppError{Code: "validation_failed", Status: 400, Message: fmt.Sprintf(format, args...)}
}
