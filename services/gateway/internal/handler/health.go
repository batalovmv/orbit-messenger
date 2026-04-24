// Copyright (C) 2024 MST Corp. All rights reserved.
// SPDX-License-Identifier: GPL-3.0-or-later

package handler

import "github.com/gofiber/fiber/v2"

func HealthHandler(c *fiber.Ctx) error {
	return c.JSON(fiber.Map{"status": "ok", "service": "orbit-gateway"})
}
