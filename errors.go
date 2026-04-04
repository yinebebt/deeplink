package deeplink

import "net/http"

// Error represents an application error with an HTTP status code.
type Error struct {
	Code    int
	Message string
	Err     error
}

func (e *Error) Error() string {
	if e.Err != nil {
		return e.Message + ": " + e.Err.Error()
	}
	return e.Message
}

func (e *Error) Unwrap() error {
	return e.Err
}

var (
	ErrInvalidType = &Error{Code: http.StatusBadRequest, Message: "invalid link type"}
	ErrNotFound    = &Error{Code: http.StatusNotFound, Message: "link not found"}
)

// NewError wraps an error with an HTTP status code and message.
func NewError(err error, code int, message string) *Error {
	return &Error{Code: code, Message: message, Err: err}
}
