package validator

import (
	"net/mail"
	"regexp"
	"unicode/utf8"

	"github.com/mst-corp/orbit/pkg/apperror"
)

var uuidRegex = regexp.MustCompile(`^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$`)

// IsValidEmail checks if the string is a valid email address.
// Rejects RFC 5322 display-name format like "Name <user@example.com>".
func IsValidEmail(email string) bool {
	addr, err := mail.ParseAddress(email)
	if err != nil {
		return false
	}
	return addr.Address == email
}

// IsValidUUID checks if the string is a valid UUID v4 format.
func IsValidUUID(s string) bool {
	return uuidRegex.MatchString(s)
}

// RequireEmail validates an email field, returning an AppError if invalid.
func RequireEmail(email, field string) *apperror.AppError {
	if email == "" {
		return apperror.BadRequest(field + " is required")
	}
	if !IsValidEmail(email) {
		return apperror.BadRequest(field + " is not a valid email")
	}
	return nil
}

// RequireString validates a non-empty string with min/max length.
func RequireString(val, field string, minLen, maxLen int) *apperror.AppError {
	if val == "" {
		return apperror.BadRequest(field + " is required")
	}
	runes := utf8.RuneCountInString(val)
	if runes < minLen {
		return apperror.BadRequest(field + " is too short")
	}
	if maxLen > 0 && runes > maxLen {
		return apperror.BadRequest(field + " is too long")
	}
	return nil
}

// RequireUUID validates a UUID string.
func RequireUUID(val, field string) *apperror.AppError {
	if val == "" {
		return apperror.BadRequest(field + " is required")
	}
	if !IsValidUUID(val) {
		return apperror.BadRequest(field + " is not a valid UUID")
	}
	return nil
}
