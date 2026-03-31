package response

import (
	"errors"
	"log/slog"
	"net/http"
	"reflect"

	"github.com/gofiber/fiber/v2"
	"github.com/mst-corp/orbit/pkg/apperror"
)

// JSON sends a JSON response with the given status code.
func JSON(c *fiber.Ctx, status int, data interface{}) error {
	return c.Status(status).JSON(data)
}

// Error sends an error response. If the error is an AppError, uses its status and message.
// Otherwise returns a generic 500 error.
func Error(c *fiber.Ctx, err error) error {
	var appErr *apperror.AppError
	if errors.As(err, &appErr) {
		return c.Status(appErr.Status).JSON(appErr)
	}
	slog.Error("unhandled error", "error", err.Error())
	internal := apperror.Internal(err.Error())
	return c.Status(internal.Status).JSON(internal)
}

// Paginated sends a paginated JSON response.
// Ensures data is always a JSON array (never null) for frontend compatibility.
func Paginated(c *fiber.Ctx, data interface{}, cursor string, hasMore bool) error {
	// nil slices serialize as null in JSON; force empty array instead
	if data == nil || (reflect.TypeOf(data).Kind() == reflect.Slice && reflect.ValueOf(data).IsNil()) {
		data = []struct{}{}
	}
	return c.Status(fiber.StatusOK).JSON(fiber.Map{
		"data":     data,
		"cursor":   cursor,
		"has_more": hasMore,
	})
}

// FiberErrorHandler is a custom Fiber error handler that returns structured errors.
func FiberErrorHandler(c *fiber.Ctx, err error) error {
	var appErr *apperror.AppError
	if errors.As(err, &appErr) {
		return c.Status(appErr.Status).JSON(appErr)
	}

	var fiberErr *fiber.Error
	if errors.As(err, &fiberErr) {
		// Normalize message to prevent leaking internal Fiber details (paths, parser errors)
		safe := http.StatusText(fiberErr.Code)
		if safe == "" {
			safe = "Request error"
		}
		return c.Status(fiberErr.Code).JSON(apperror.AppError{
			Code:    "error",
			Message: safe,
			Status:  fiberErr.Code,
		})
	}

	slog.Error("unhandled error", "error", err.Error())
	internal := apperror.Internal(err.Error())
	return c.Status(internal.Status).JSON(internal)
}
