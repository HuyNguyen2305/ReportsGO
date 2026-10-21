// Package dto holds the request and response shapes of the HTTP API.
package dto

// ErrorResponse is the envelope of every error returned by the API.
type ErrorResponse struct {
	Error ErrorBody `json:"error"`
}

// ErrorBody describes one error.
type ErrorBody struct {
	Code      string        `json:"code"`
	Message   string        `json:"message"`
	RequestID string        `json:"request_id,omitempty"`
	Details   []ErrorDetail `json:"details,omitempty"`
}

// ErrorDetail points at one invalid field.
type ErrorDetail struct {
	Field   string `json:"field"`
	Rule    string `json:"rule,omitempty"`
	Message string `json:"message"`
}
