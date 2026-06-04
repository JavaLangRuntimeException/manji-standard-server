package errs

import (
	"errors"
	"fmt"
)

// RDBErrorWrapper はRDBリポジトリエラーをDomainErrorに変換するインターフェース
type RDBErrorWrapper interface {
	Wrap(resource string, err error) *DomainError
}

type rdbErrorWrapper struct {
	checker DBErrorChecker
}

func NewRDBErrorWrapper(checker DBErrorChecker) RDBErrorWrapper {
	return &rdbErrorWrapper{checker: checker}
}

func (w *rdbErrorWrapper) Wrap(resource string, err error) *DomainError {
	if err == nil {
		return nil
	}

	if de, ok := As(err); ok {
		return de
	}

	switch {
	case errors.Is(err, ErrNotFound):
		return New(ErrorTypeNotFound, fmt.Sprintf("%s not found", resource))
	case w.checker.IsNotNullViolationError(err):
		de := New(ErrorTypeBadRequest, fmt.Sprintf("NOT NULL constraint failed in %s", resource))
		if col := w.checker.ColumnName(err); col != "" {
			de.Errors = []FieldError{{Field: col, Message: "is required"}}
		}
		return de
	case w.checker.IsCheckViolationError(err):
		de := New(ErrorTypeBadRequest, fmt.Sprintf("CHECK constraint failed in %s", resource))
		if col := w.checker.ColumnName(err); col != "" {
			de.Errors = []FieldError{{Field: col, Message: "violates check constraint"}}
		}
		return de
	case w.checker.IsUniqueConstraintError(err):
		de := New(ErrorTypeDuplicate, fmt.Sprintf("%s already exists", resource))
		if col := w.checker.ColumnName(err); col != "" {
			de.Errors = []FieldError{{Field: col, Message: "already exists"}}
		}
		return de
	case w.checker.IsForeignKeyError(err):
		de := New(ErrorTypeBadRequest, fmt.Sprintf("FK constraint failed in %s", resource))
		if col := w.checker.ColumnName(err); col != "" {
			de.Errors = []FieldError{{Field: col, Message: "referenced record not found"}}
		}
		return de
	case w.checker.IsSerializationFailureError(err):
		return New(ErrorTypeDatabase, fmt.Sprintf("serialization failure in %s", resource))
	case w.checker.IsDeadlockDetectedError(err):
		return New(ErrorTypeDatabase, fmt.Sprintf("deadlock detected in %s", resource))
	default:
		return New(ErrorTypeInternal, fmt.Sprintf("unexpected error in %s", resource))
	}
}
