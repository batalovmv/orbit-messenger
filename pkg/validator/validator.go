package validator

import (
	"net/mail"
	"regexp"

	"github.com/mst-corp/orbit/pkg/apperror"
)

var uuidRegex = regexp.MustCompile(`^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$`)

// IsValidEmail checks if the string is a valid email address.
func IsValidEmail(email string) bool {
	_, err := mail.ParseAddress(email)
	return err == nil
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
	if len(val) < minLen {
		return apperror.BadRequest(field + " is too short")
	}
	if maxLen > 0 && len(val) > maxLen {
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
