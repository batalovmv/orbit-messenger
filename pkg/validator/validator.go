package validator

import (
	"net/mail"
	"regexp"
	"strconv"
	"strings"
	"unicode/utf8"

	"github.com/mst-corp/orbit/pkg/apperror"
)

var uuidRegex = regexp.MustCompile(`(?i)^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$`)

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
	trimmed := strings.TrimSpace(strings.ReplaceAll(val, "\u200B", ""))
	if trimmed == "" {
		return apperror.BadRequest(field + " is required")
	}
	runes := utf8.RuneCountInString(trimmed)
	if runes < minLen {
		return apperror.BadRequest(field + " is too short")
	}
	if maxLen > 0 && runes > maxLen {
		return apperror.BadRequest(field + " is too long")
	}
	return nil
}

// OptionalString validates a string length only if non-empty after trimming.
// Use for optional fields that can be cleared (e.g. bio, description).
func OptionalString(val, field string, maxLen int) *apperror.AppError {
	trimmed := strings.TrimSpace(strings.ReplaceAll(val, "\u200B", ""))
	if trimmed == "" {
		return nil
	}
	runes := utf8.RuneCountInString(trimmed)
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

// RequireDigits validates that a string contains exactly n ASCII digit characters.
// Returns nil if val is empty (field is optional). Use RequireString first if required.
func RequireDigits(val, field string, n int) *apperror.AppError {
	if val == "" {
		return nil
	}
	if len(val) != n {
		return apperror.BadRequest(field + " must be exactly " + strconv.Itoa(n) + " digits")
	}
	for _, ch := range val {
		if ch < '0' || ch > '9' {
			return apperror.BadRequest(field + " must contain only digits")
		}
	}
	return nil
}

// SanitizeFilename strips path separators and directory traversal sequences,
// then truncates to maxLen runes. Returns the sanitized name and an error if
// the result is empty after sanitization.
func SanitizeFilename(name string, maxLen int) (string, *apperror.AppError) {
	// Normalize backslashes to forward slashes
	name = strings.ReplaceAll(name, "\\", "/")
	// Take only the last path component to strip directory traversal
	if idx := strings.LastIndex(name, "/"); idx >= 0 {
		name = name[idx+1:]
	}
	// Remove null bytes and control characters (codepoints < 32 and DEL 127)
	var b strings.Builder
	for _, r := range name {
		if r >= 32 && r != 127 {
			b.WriteRune(r)
		}
	}
	name = strings.TrimSpace(b.String())
	if name == "" {
		return "", apperror.BadRequest("filename is required")
	}
	runes := []rune(name)
	if maxLen > 0 && len(runes) > maxLen {
		name = string(runes[:maxLen])
	}
	return name, nil
}
