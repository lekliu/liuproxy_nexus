package errors

import (
	"strings"

	"liuproxy_nexus/internal/xray_core/common/serial"
)

// Error is an error object with underlying error.
type Error struct {
	prefix  []interface{}
	message []interface{}
	inner   error
	//severity log.Severity
}

// Error implements error.Error().
func (err *Error) Error() string {
	builder := strings.Builder{}
	for _, prefix := range err.prefix {
		builder.WriteByte('[')
		builder.WriteString(serial.ToString(prefix))
		builder.WriteString("] ")
	}

	msg := serial.Concat(err.message...)
	builder.WriteString(msg)

	if err.inner != nil {
		builder.WriteString(" > ")
		builder.WriteString(err.inner.Error())
	}

	return builder.String()
}

func (err *Error) Base(e error) *Error {
	err.inner = e
	return err
}

// String returns the string representation of this error.
func (err *Error) String() string {
	return err.Error()
}

// New returns a new error object with message formed from given arguments.
func NewError(msg ...interface{}) *Error {
	return &Error{
		message: msg,
		//severity: log.Severity_Info,
	}
}
