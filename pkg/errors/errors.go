package errors

import (
	"errors"
	"fmt"
)

// ServiceError represents an error with additional context
// Simplified version aligned with TRex pattern
type ServiceError struct {
	Code    string
	Message string
	Reason  string
	Err     error
}

// Error implements the error interface
func (e *ServiceError) Error() string {
	if e.Err != nil {
		return fmt.Sprintf("%s: %s (%s)", e.Code, e.Message, e.Err.Error())
	}
	return fmt.Sprintf("%s: %s", e.Code, e.Message)
}

// Unwrap implements the errors.Unwrap interface
func (e *ServiceError) Unwrap() error {
	return e.Err
}

// New creates a new ServiceError
func New(code, message, reason string) *ServiceError {
	return &ServiceError{
		Code:    code,
		Message: message,
		Reason:  reason,
	}
}

// Wrap wraps an error with additional context
func Wrap(err error, code, message string) *ServiceError {
	return &ServiceError{
		Code:    code,
		Message: message,
		Err:     err,
	}
}

// Common error constructors aligned with TRex

// NotFound creates a not found error
func NotFound(message string) *ServiceError {
	return New("NOT_FOUND", message, "Resource not found")
}

// BadRequest creates a bad request error
func BadRequest(message string) *ServiceError {
	return New("BAD_REQUEST", message, "Invalid request")
}

// InternalServerError creates an internal server error
func InternalServerError(message string) *ServiceError {
	return New("INTERNAL_ERROR", message, "Internal error occurred")
}

// Conflict creates a conflict error
func Conflict(message string) *ServiceError {
	return New("CONFLICT", message, "Resource conflict")
}

// Validation creates a validation error
func Validation(message string) *ServiceError {
	return New("VALIDATION_ERROR", message, "Validation failed")
}

// IsNotFound checks if an error is a not found error
func IsNotFound(err error) bool {
	var svcErr *ServiceError
	if errors.As(err, &svcErr) {
		return svcErr.Code == "NOT_FOUND"
	}
	return false
}

// IsConflict checks if an error is a conflict error
func IsConflict(err error) bool {
	var svcErr *ServiceError
	if errors.As(err, &svcErr) {
		return svcErr.Code == "CONFLICT"
	}
	return false
}
