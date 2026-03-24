package handler

import "github.com/gofiber/fiber/v2"

func HealthHandler(c *fiber.Ctx) error {
	return c.JSON(fiber.Map{"status": "ok", "service": "orbit-gateway"})
}
