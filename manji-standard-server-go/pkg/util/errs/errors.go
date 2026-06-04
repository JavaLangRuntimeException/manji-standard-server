package errs

import (
	"errors"
	"fmt"
	"net/http"
)

type FieldError struct {
	Field   string         `json:"field"`
	Message string         `json:"message"`
	Meta    map[string]any `json:"meta,omitempty"`
}

type ErrorType string

const (
	ErrorTypeValidation   ErrorType = "VALIDATION_ERROR"
	ErrorTypeNotFound     ErrorType = "NOT_FOUND"
	ErrorTypeDuplicate    ErrorType = "DUPLICATE_ERROR"
	ErrorTypeUnauthorized ErrorType = "UNAUTHORIZED"
	ErrorTypeForbidden    ErrorType = "FORBIDDEN"
	ErrorTypeBadRequest   ErrorType = "BAD_REQUEST"
	ErrorTypeInternal     ErrorType = "INTERNAL_ERROR"
	ErrorTypeDatabase     ErrorType = "DATABASE_ERROR"
)

type DomainError struct {
	Type       ErrorType    `json:"type"`
	Message    string       `json:"message"`
	Errors     []FieldError `json:"-"`
	StatusCode int          `json:"-"`
}

func (e *DomainError) Error() string {
	return fmt.Sprintf("%s: %s", e.Type, e.Message)
}

func New(errType ErrorType, message string) *DomainError {
	e := &DomainError{
		Type:    errType,
		Message: message,
	}
	switch errType {
	case ErrorTypeValidation, ErrorTypeBadRequest:
		e.StatusCode = http.StatusBadRequest
	case ErrorTypeNotFound:
		e.StatusCode = http.StatusNotFound
	case ErrorTypeUnauthorized:
		e.StatusCode = http.StatusUnauthorized
	case ErrorTypeForbidden:
		e.StatusCode = http.StatusForbidden
	case ErrorTypeDuplicate:
		e.StatusCode = http.StatusConflict
	default:
		e.StatusCode = http.StatusInternalServerError
	}
	return e
}

func As(err error) (*DomainError, bool) {
	if err == nil {
		return nil, false
	}
	var de *DomainError
	if errors.As(err, &de) {
		return de, true
	}
	return nil, false
}
