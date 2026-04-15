package apperror

import "fmt"

// AppError represents a structured API error.
type AppError struct {
	Code    string `json:"error"`
	Message string `json:"message"`
	Status  int    `json:"status"`
}

func (e *AppError) Error() string {
	return fmt.Sprintf("%s: %s", e.Code, e.Message)
}

func BadRequest(msg string) *AppError {
	return &AppError{Code: "bad_request", Message: msg, Status: 400}
}

func Unauthorized(msg string) *AppError {
	return &AppError{Code: "unauthorized", Message: msg, Status: 401}
}

func Forbidden(msg string) *AppError {
	return &AppError{Code: "forbidden", Message: msg, Status: 403}
}

func NotFound(msg string) *AppError {
	return &AppError{Code: "not_found", Message: msg, Status: 404}
}

func Conflict(msg string) *AppError {
	return &AppError{Code: "conflict", Message: msg, Status: 409}
}

func TooManyRequests(msg string) *AppError {
	return &AppError{Code: "rate_limited", Message: msg, Status: 429}
}

func Internal(msg string) *AppError {
	return &AppError{Code: "internal_error", Message: "Internal server error", Status: 500}
}

// NotImplemented signals that an endpoint is a known no-op (e.g. AI semantic
// search before embeddings are wired up). The caller's message is returned
// so the UI can explain why the feature is disabled.
func NotImplemented(msg string) *AppError {
	return &AppError{Code: "not_implemented", Message: msg, Status: 501}
}

// ServiceUnavailable signals that an upstream dependency (AI provider,
// external API) is not currently reachable or not configured. Used by the
// AI service when ANTHROPIC_API_KEY / OPENAI_API_KEY is missing so the
// frontend can show a deterministic "AI offline" banner without the service
// crashing on startup.
func ServiceUnavailable(msg string) *AppError {
	return &AppError{Code: "service_unavailable", Message: msg, Status: 503}
}
