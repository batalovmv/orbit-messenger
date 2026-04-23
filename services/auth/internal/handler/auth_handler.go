package handler

import (
	"crypto/subtle"
	"log/slog"
	"os"
	"strings"
	"time"

	"github.com/gofiber/fiber/v2"
	"github.com/google/uuid"

	"github.com/mst-corp/orbit/pkg/apperror"
	"github.com/mst-corp/orbit/pkg/permissions"
	"github.com/mst-corp/orbit/pkg/response"
	"github.com/mst-corp/orbit/pkg/validator"
	"github.com/mst-corp/orbit/services/auth/internal/service"
)

func makeRefreshCookie(value string, maxAge int) *fiber.Cookie {
	secure := !strings.HasPrefix(os.Getenv("FRONTEND_URL"), "http://")
	return &fiber.Cookie{
		Name:     "refresh_token",
		Value:    value,
		HTTPOnly: true,
		Secure:   secure,
		SameSite: "Lax",
		Path:     "/",
		MaxAge:   maxAge,
	}
}

type AuthHandler struct {
	svc             *service.AuthService
	logger          *slog.Logger
	internalSecret  string
	bootstrapSecret string
}

// NewAuthHandler constructs an AuthHandler. bootstrapSecret gates the
// /auth/bootstrap endpoint: when empty, bootstrap is fully disabled; when set,
// callers must present a matching X-Bootstrap-Secret header.
func NewAuthHandler(svc *service.AuthService, logger *slog.Logger, internalSecret, bootstrapSecret string) *AuthHandler {
	return &AuthHandler{
		svc:             svc,
		logger:          logger,
		internalSecret:  internalSecret,
		bootstrapSecret: bootstrapSecret,
	}
}

func (h *AuthHandler) Register(app *fiber.App) {
	auth := app.Group("/auth")

	auth.Post("/bootstrap", h.Bootstrap)
	auth.Post("/register", h.RegisterUser)
	auth.Post("/login", h.Login)
	auth.Post("/logout", h.requireAuth, h.Logout)
	auth.Post("/refresh", h.Refresh)
	auth.Get("/me", h.requireAuth, h.GetMe)
	auth.Post("/reset-admin", h.ResetAdmin)
	auth.Get("/sessions", h.requireAuth, h.ListSessions)
	auth.Delete("/sessions/:id", h.requireAuth, h.RevokeSession)
	auth.Post("/2fa/setup", h.requireAuth, h.Setup2FA)
	auth.Post("/2fa/verify", h.requireAuth, h.Verify2FA)
	auth.Post("/2fa/disable", h.requireAuth, h.Disable2FA)
	auth.Post("/invite/validate", h.ValidateInvite)
	auth.Post("/invites", h.requireAuth, h.requireAdmin, h.CreateInvite)
	auth.Get("/invites", h.requireAuth, h.requireAdmin, h.ListInvites)
	auth.Delete("/invites/:id", h.requireAuth, h.requireAdmin, h.RevokeInvite)

	// User settings routes (proxied from gateway as /users/me/*)
	users := app.Group("/users/me", h.requireAuth)
	users.Get("/notification-priority", h.GetNotificationPriorityMode)
	users.Put("/notification-priority", h.UpdateNotificationPriorityMode)
}

// --- Middleware ---

func (h *AuthHandler) requireAuth(c *fiber.Ctx) error {
	// Trust X-User-ID only if the request carries a valid internal token
	// proving it was proxied by the gateway (not sent directly by a client).
	if internalToken := c.Get("X-Internal-Token"); internalToken != "" && h.internalSecret != "" &&
		subtle.ConstantTimeCompare([]byte(internalToken), []byte(h.internalSecret)) == 1 {
		userID := c.Get("X-User-ID")
		if userID != "" {
			c.Locals("user_id", userID)
			c.Locals("user_role", c.Get("X-User-Role", "member"))
			return c.Next()
		}
	}

	token := extractBearerToken(c)
	if token == "" {
		return response.Error(c, apperror.Unauthorized("Missing authorization"))
	}

	uid, role, err := h.svc.ValidateAccessToken(c.Context(), token)
	if err != nil {
		return response.Error(c, err)
	}

	c.Locals("user_id", uid.String())
	c.Locals("user_role", role)
	return c.Next()
}

func (h *AuthHandler) requireAdmin(c *fiber.Ctx) error {
	role, _ := c.Locals("user_role").(string)
	if !permissions.HasSysPermission(role, permissions.SysManageInvites) {
		return response.Error(c, apperror.Forbidden("Admin access required"))
	}
	return c.Next()
}

func getUserID(c *fiber.Ctx) (uuid.UUID, error) {
	idStr, ok := c.Locals("user_id").(string)
	if !ok || idStr == "" {
		return uuid.Nil, apperror.Unauthorized("Missing user context")
	}
	return uuid.Parse(idStr)
}

func extractBearerToken(c *fiber.Ctx) string {
	auth := c.Get("Authorization")
	if strings.HasPrefix(auth, "Bearer ") {
		return auth[7:]
	}
	return ""
}

// --- Handlers ---

func (h *AuthHandler) Bootstrap(c *fiber.Ctx) error {
	// Gate: bootstrap is disabled unless BOOTSTRAP_SECRET is configured on the
	// service. When configured, the caller must present a matching
	// X-Bootstrap-Secret header. This prevents a race where the first
	// unauthenticated external request on a fresh deployment mints a
	// permanent superadmin account.
	if h.bootstrapSecret == "" {
		return response.Error(c, apperror.Forbidden("Bootstrap disabled"))
	}
	provided := c.Get("X-Bootstrap-Secret")
	if provided == "" || subtle.ConstantTimeCompare([]byte(provided), []byte(h.bootstrapSecret)) != 1 {
		return response.Error(c, apperror.Forbidden("Bootstrap disabled"))
	}

	var req struct {
		Email       string `json:"email"`
		Password    string `json:"password"`
		DisplayName string `json:"display_name"`
	}
	if err := c.BodyParser(&req); err != nil {
		return response.Error(c, apperror.BadRequest("Invalid request body"))
	}

	if appErr := validator.RequireEmail(req.Email, "email"); appErr != nil {
		return response.Error(c, appErr)
	}
	if appErr := validator.RequireString(req.Password, "password", 8, 72); appErr != nil {
		return response.Error(c, appErr)
	}
	if appErr := validator.RequireString(req.DisplayName, "display_name", 1, 100); appErr != nil {
		return response.Error(c, appErr)
	}

	u, err := h.svc.Bootstrap(c.Context(), req.Email, req.Password, req.DisplayName)
	if err != nil {
		return response.Error(c, err)
	}

	return response.JSON(c, fiber.StatusCreated, u)
}

func (h *AuthHandler) RegisterUser(c *fiber.Ctx) error {
	var req struct {
		InviteCode  string `json:"invite_code"`
		Email       string `json:"email"`
		Password    string `json:"password"`
		DisplayName string `json:"display_name"`
	}
	if err := c.BodyParser(&req); err != nil {
		return response.Error(c, apperror.BadRequest("Invalid request body"))
	}

	if appErr := validator.RequireString(req.InviteCode, "invite_code", 1, 32); appErr != nil {
		return response.Error(c, appErr)
	}
	if appErr := validator.RequireEmail(req.Email, "email"); appErr != nil {
		return response.Error(c, appErr)
	}
	if appErr := validator.RequireString(req.Password, "password", 8, 72); appErr != nil {
		return response.Error(c, appErr)
	}
	if appErr := validator.RequireString(req.DisplayName, "display_name", 1, 100); appErr != nil {
		return response.Error(c, appErr)
	}

	u, err := h.svc.Register(c.Context(), req.InviteCode, req.Email, req.Password, req.DisplayName)
	if err != nil {
		return response.Error(c, err)
	}

	return response.JSON(c, fiber.StatusCreated, u)
}

func (h *AuthHandler) Login(c *fiber.Ctx) error {
	var req struct {
		Email    string `json:"email"`
		Password string `json:"password"`
		TOTPCode string `json:"totp_code"`
	}
	if err := c.BodyParser(&req); err != nil {
		return response.Error(c, apperror.BadRequest("Invalid request body"))
	}

	if appErr := validator.RequireEmail(req.Email, "email"); appErr != nil {
		return response.Error(c, appErr)
	}
	if appErr := validator.RequireString(req.Password, "password", 1, 72); appErr != nil {
		return response.Error(c, appErr)
	}

	ip := c.IP()
	ua := c.Get("User-Agent")

	pair, user, err := h.svc.Login(c.Context(), req.Email, req.Password, req.TOTPCode, ip, ua)
	if err != nil {
		return response.Error(c, err)
	}

	// Set refresh token as httpOnly cookie
	c.Cookie(makeRefreshCookie(pair.RefreshToken, int(30*24*time.Hour/time.Second)))

	return response.JSON(c, fiber.StatusOK, fiber.Map{
		"access_token": pair.AccessToken,
		"expires_in":   pair.ExpiresIn,
		"user":         user,
	})
}

func (h *AuthHandler) Logout(c *fiber.Ctx) error {
	token := extractBearerToken(c)
	if token == "" {
		return response.Error(c, apperror.BadRequest("Missing token"))
	}

	refreshToken := c.Cookies("refresh_token")
	if err := h.svc.Logout(c.Context(), token, refreshToken); err != nil {
		return response.Error(c, err)
	}

	// Clear refresh cookie
	c.Cookie(makeRefreshCookie("", -1))

	return response.JSON(c, fiber.StatusOK, fiber.Map{"message": "Logged out"})
}

func (h *AuthHandler) Refresh(c *fiber.Ctx) error {
	refreshToken := c.Cookies("refresh_token")
	if refreshToken == "" {
		// Also accept from body for non-browser clients
		var req struct {
			RefreshToken string `json:"refresh_token"`
		}
		if err := c.BodyParser(&req); err == nil && req.RefreshToken != "" {
			refreshToken = req.RefreshToken
		}
	}
	if refreshToken == "" {
		return response.Error(c, apperror.BadRequest("Missing refresh token"))
	}

	ip := c.IP()
	ua := c.Get("User-Agent")

	pair, user, err := h.svc.Refresh(c.Context(), refreshToken, ip, ua)
	if err != nil {
		return response.Error(c, err)
	}

	c.Cookie(makeRefreshCookie(pair.RefreshToken, int(30*24*time.Hour/time.Second)))

	return response.JSON(c, fiber.StatusOK, fiber.Map{
		"access_token": pair.AccessToken,
		"expires_in":   pair.ExpiresIn,
		"user":         user,
	})
}

func (h *AuthHandler) GetMe(c *fiber.Ctx) error {
	uid, err := getUserID(c)
	if err != nil {
		return response.Error(c, apperror.Unauthorized("Invalid user ID"))
	}

	u, err := h.svc.GetMe(c.Context(), uid)
	if err != nil {
		return response.Error(c, err)
	}

	return response.JSON(c, fiber.StatusOK, u)
}

func (h *AuthHandler) ResetAdmin(c *fiber.Ctx) error {
	var req struct {
		ResetKey    string `json:"reset_key"`
		Email       string `json:"email"`
		NewPassword string `json:"new_password"`
	}
	if err := c.BodyParser(&req); err != nil {
		return response.Error(c, apperror.BadRequest("Invalid request body"))
	}

	if appErr := validator.RequireString(req.ResetKey, "reset_key", 1, 256); appErr != nil {
		return response.Error(c, appErr)
	}
	if appErr := validator.RequireEmail(req.Email, "email"); appErr != nil {
		return response.Error(c, appErr)
	}
	if appErr := validator.RequireString(req.NewPassword, "new_password", 8, 72); appErr != nil {
		return response.Error(c, appErr)
	}

	if err := h.svc.ResetAdmin(c.Context(), req.ResetKey, req.Email, req.NewPassword); err != nil {
		return response.Error(c, err)
	}

	return response.JSON(c, fiber.StatusOK, fiber.Map{"message": "Admin password reset"})
}

func (h *AuthHandler) ListSessions(c *fiber.Ctx) error {
	uid, err := getUserID(c)
	if err != nil {
		return response.Error(c, apperror.Unauthorized("Invalid user ID"))
	}

	sessions, err := h.svc.ListSessions(c.Context(), uid)
	if err != nil {
		return response.Error(c, err)
	}

	return response.JSON(c, fiber.StatusOK, fiber.Map{"sessions": sessions})
}

func (h *AuthHandler) RevokeSession(c *fiber.Ctx) error {
	uid, err := getUserID(c)
	if err != nil {
		return response.Error(c, apperror.Unauthorized("Invalid user ID"))
	}

	sessionID, err := uuid.Parse(c.Params("id"))
	if err != nil {
		return response.Error(c, apperror.BadRequest("Invalid session ID"))
	}

	if err := h.svc.RevokeSession(c.Context(), sessionID, uid); err != nil {
		return response.Error(c, err)
	}

	return response.JSON(c, fiber.StatusOK, fiber.Map{"message": "Session revoked"})
}

func (h *AuthHandler) Setup2FA(c *fiber.Ctx) error {
	uid, err := getUserID(c)
	if err != nil {
		return response.Error(c, apperror.Unauthorized("Invalid user ID"))
	}

	secret, uri, err := h.svc.Setup2FA(c.Context(), uid)
	if err != nil {
		return response.Error(c, err)
	}

	return response.JSON(c, fiber.StatusOK, fiber.Map{
		"secret": secret,
		"uri":    uri,
	})
}

func (h *AuthHandler) Verify2FA(c *fiber.Ctx) error {
	uid, err := getUserID(c)
	if err != nil {
		return response.Error(c, apperror.Unauthorized("Invalid user ID"))
	}

	var req struct {
		Code string `json:"code"`
	}
	if err := c.BodyParser(&req); err != nil {
		return response.Error(c, apperror.BadRequest("Invalid request body"))
	}
	if appErr := validator.RequireString(req.Code, "code", 6, 6); appErr != nil {
		return response.Error(c, appErr)
	}

	if err := h.svc.Verify2FA(c.Context(), uid, req.Code); err != nil {
		return response.Error(c, err)
	}

	return response.JSON(c, fiber.StatusOK, fiber.Map{"message": "2FA enabled"})
}

func (h *AuthHandler) Disable2FA(c *fiber.Ctx) error {
	uid, err := getUserID(c)
	if err != nil {
		return response.Error(c, apperror.Unauthorized("Invalid user ID"))
	}

	var req struct {
		Password string `json:"password"`
	}
	if err := c.BodyParser(&req); err != nil {
		return response.Error(c, apperror.BadRequest("Invalid request body"))
	}
	if appErr := validator.RequireString(req.Password, "password", 1, 72); appErr != nil {
		return response.Error(c, appErr)
	}

	if err := h.svc.Disable2FA(c.Context(), uid, req.Password); err != nil {
		return response.Error(c, err)
	}

	return response.JSON(c, fiber.StatusOK, fiber.Map{"message": "2FA disabled"})
}

func (h *AuthHandler) ValidateInvite(c *fiber.Ctx) error {
	var req struct {
		Code string `json:"code"`
	}
	if err := c.BodyParser(&req); err != nil {
		return response.Error(c, apperror.BadRequest("Invalid request body"))
	}
	if appErr := validator.RequireString(req.Code, "code", 1, 64); appErr != nil {
		return response.Error(c, appErr)
	}

	inv, err := h.svc.ValidateInvite(c.Context(), req.Code)
	if err != nil {
		return response.Error(c, err)
	}

	result := fiber.Map{
		"valid":    true,
		"has_email": inv.Email != nil,
	}
	return response.JSON(c, fiber.StatusOK, result)
}

func (h *AuthHandler) CreateInvite(c *fiber.Ctx) error {
	uid, err := getUserID(c)
	if err != nil {
		return response.Error(c, apperror.Unauthorized("Invalid user ID"))
	}

	var req struct {
		Email     *string    `json:"email"`
		Role      string     `json:"role"`
		MaxUses   int        `json:"max_uses"`
		ExpiresAt *time.Time `json:"expires_at"`
	}
	if err := c.BodyParser(&req); err != nil {
		return response.Error(c, apperror.BadRequest("Invalid request body"))
	}

	if req.Role == "" {
		req.Role = "member"
	}
	if !permissions.ValidSystemRoles[req.Role] {
		return response.Error(c, apperror.BadRequest("Invalid role"))
	}
	actorRole, _ := c.Locals("user_role").(string)
	if !permissions.CanAssignRole(actorRole, req.Role) {
		return response.Error(c, apperror.Forbidden("You cannot create invites with this role"))
	}
	if req.MaxUses <= 0 {
		req.MaxUses = 1
	}

	inv, err := h.svc.CreateInvite(c.Context(), uid, req.Email, req.Role, req.MaxUses, req.ExpiresAt)
	if err != nil {
		return response.Error(c, err)
	}

	return response.JSON(c, fiber.StatusCreated, inv)
}

func (h *AuthHandler) ListInvites(c *fiber.Ctx) error {
	invites, err := h.svc.ListInvites(c.Context())
	if err != nil {
		return response.Error(c, err)
	}

	return response.JSON(c, fiber.StatusOK, fiber.Map{"invites": invites})
}

func (h *AuthHandler) RevokeInvite(c *fiber.Ctx) error {
	uid, err := getUserID(c)
	if err != nil {
		return response.Error(c, apperror.Unauthorized("Invalid user ID"))
	}

	inviteID, err := uuid.Parse(c.Params("id"))
	if err != nil {
		return response.Error(c, apperror.BadRequest("Invalid invite ID"))
	}

	if err := h.svc.RevokeInvite(c.Context(), inviteID, uid); err != nil {
		return response.Error(c, err)
	}

	return response.JSON(c, fiber.StatusOK, fiber.Map{"message": "Invite revoked"})
}

func (h *AuthHandler) GetNotificationPriorityMode(c *fiber.Ctx) error {
	uid, err := getUserID(c)
	if err != nil {
		return response.Error(c, apperror.Unauthorized("Invalid user ID"))
	}

	mode, err := h.svc.GetNotificationPriorityMode(c.Context(), uid)
	if err != nil {
		return response.Error(c, err)
	}

	return response.JSON(c, fiber.StatusOK, fiber.Map{"mode": mode})
}

func (h *AuthHandler) UpdateNotificationPriorityMode(c *fiber.Ctx) error {
	uid, err := getUserID(c)
	if err != nil {
		return response.Error(c, apperror.Unauthorized("Invalid user ID"))
	}

	var req struct {
		Mode string `json:"mode"`
	}
	if err := c.BodyParser(&req); err != nil {
		return response.Error(c, apperror.BadRequest("Invalid request body"))
	}

	validModes := map[string]bool{"smart": true, "all": true, "off": true}
	if !validModes[req.Mode] {
		return response.Error(c, apperror.BadRequest("mode must be one of: smart, all, off"))
	}

	if err := h.svc.UpdateNotificationPriorityMode(c.Context(), uid, req.Mode); err != nil {
		return response.Error(c, err)
	}

	return response.JSON(c, fiber.StatusOK, fiber.Map{"mode": req.Mode})
}
