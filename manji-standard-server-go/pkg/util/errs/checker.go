package errs

import "errors"

// ErrNotFound はRowsAffected == 0のときにリポジトリ実装が返すsentinel
var ErrNotFound = errors.New("repository: entity not found")

// DBErrorChecker はDB固有のエラーを判定するインターフェース。
// 具体実装はinfra層に置き、DB依存をpkg層に持ち込まない。
type DBErrorChecker interface {
	IsUniqueConstraintError(err error) bool
	IsForeignKeyError(err error) bool
	IsNotNullViolationError(err error) bool
	IsCheckViolationError(err error) bool
	IsSerializationFailureError(err error) bool
	IsDeadlockDetectedError(err error) bool
	ConstraintName(err error) string
	ColumnName(err error) string
}
