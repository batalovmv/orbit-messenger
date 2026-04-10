package botapi

import (
	"context"

	"github.com/gofiber/fiber/v2"
	"github.com/mst-corp/orbit/pkg/apperror"
	"github.com/mst-corp/orbit/pkg/response"
	"github.com/mst-corp/orbit/services/bots/internal/model"
)

type TokenValidator interface {
	ValidateToken(ctx context.Context, rawToken string) (*model.Bot, error)
}

func TokenAuthMiddleware(svc TokenValidator) fiber.Handler {
	return func(c *fiber.Ctx) error {
		token := c.Params("token")
		if token == "" {
			return response.Error(c, apperror.Unauthorized("Missing bot token"))
		}

		bot, err := svc.ValidateToken(c.Context(), token)
		if err != nil {
			return response.Error(c, apperror.Unauthorized("Invalid bot token"))
		}
		if !bot.IsActive {
			return response.Error(c, apperror.Forbidden("Bot is deactivated"))
		}

		c.Locals("bot", bot)
		c.Locals("bot_user_id", bot.UserID)
		return c.Next()
	}
}
