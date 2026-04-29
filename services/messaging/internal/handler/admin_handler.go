package handler

import (
	"bufio"
	"context"
	"encoding/csv"
	"encoding/json"
	"fmt"
	"log/slog"
	"time"

	"github.com/gofiber/fiber/v2"
	"github.com/google/uuid"

	"github.com/mst-corp/orbit/pkg/apperror"
	"github.com/mst-corp/orbit/pkg/permissions"
	"github.com/mst-corp/orbit/pkg/response"
	"github.com/mst-corp/orbit/services/messaging/internal/model"
	"github.com/mst-corp/orbit/services/messaging/internal/service"
	"github.com/mst-corp/orbit/services/messaging/internal/store"
)

// exportStreamTimeout caps any single NDJSON export at 10 minutes. The
// stream writer callback runs in a goroutine spawned by fasthttp AFTER
// ExportUser/ExportChat return, so we cannot reuse fiber's request
// context — by then `c.Context()` returns nil and any DB pool acquire
// panics with a nil pointer dereference. Use an independent background
// context with this hard cap instead.
const exportStreamTimeout = 10 * time.Minute

type AdminHandler struct {
	svc *service.AdminService
}

func NewAdminHandler(svc *service.AdminService) *AdminHandler {
	return &AdminHandler{svc: svc}
}

func (h *AdminHandler) Register(app fiber.Router) {
	admin := app.Group("/admin")
	admin.Get("/chats", h.ListAllChats)
	admin.Get("/chats/:id/export", h.ExportChat)
	admin.Get("/users", h.ListAllUsers)
	admin.Post("/users/:id/deactivate", h.DeactivateUser)
	admin.Post("/users/:id/reactivate", h.ReactivateUser)
	admin.Patch("/users/:id/role", h.ChangeUserRole)
	admin.Get("/users/:id/export", h.ExportUser)
	admin.Get("/audit-log", h.GetAuditLog)
	admin.Get("/audit-log/export", h.ExportAuditLog)
	// Welcome flow (mig 069). Both endpoints gated by SysManageSettings inside
	// the service layer; the handler only deals with parsing + auth context.
	admin.Put("/chats/:id/default-status", h.SetChatDefaultStatus)
	admin.Post("/default-chats/backfill", h.BackfillDefaultMemberships)
}

func (h *AdminHandler) ListAllChats(c *fiber.Ctx) error {
	actorID, err := getUserID(c)
	if err != nil {
		return response.Error(c, err)
	}

	cursor := c.Query("cursor")
	limit := c.QueryInt("limit", 50)

	chats, nextCursor, hasMore, err := h.svc.ListAllChats(
		c.Context(), actorID, getUserRole(c), cursor, limit,
		c.IP(), c.Get("User-Agent"),
	)
	if err != nil {
		return response.Error(c, err)
	}

	return response.Paginated(c, chats, nextCursor, hasMore)
}

func (h *AdminHandler) ListAllUsers(c *fiber.Ctx) error {
	actorID, err := getUserID(c)
	if err != nil {
		return response.Error(c, err)
	}

	cursor := c.Query("cursor")
	limit := c.QueryInt("limit", 50)

	users, nextCursor, hasMore, err := h.svc.ListAllUsers(
		c.Context(), actorID, getUserRole(c), cursor, limit,
		c.IP(), c.Get("User-Agent"),
	)
	if err != nil {
		return response.Error(c, err)
	}

	return response.Paginated(c, users, nextCursor, hasMore)
}

func (h *AdminHandler) DeactivateUser(c *fiber.Ctx) error {
	actorID, err := getUserID(c)
	if err != nil {
		return response.Error(c, err)
	}

	targetID, err := uuid.Parse(c.Params("id"))
	if err != nil {
		return response.Error(c, apperror.BadRequest("Invalid user ID"))
	}

	var req struct {
		Reason string `json:"reason"`
	}
	c.BodyParser(&req) //nolint: optional body

	if err := h.svc.DeactivateUser(
		c.Context(), actorID, targetID, getUserRole(c), req.Reason,
		c.IP(), c.Get("User-Agent"),
	); err != nil {
		return response.Error(c, err)
	}

	return response.JSON(c, 200, fiber.Map{"status": "deactivated"})
}

func (h *AdminHandler) ReactivateUser(c *fiber.Ctx) error {
	actorID, err := getUserID(c)
	if err != nil {
		return response.Error(c, err)
	}

	targetID, err := uuid.Parse(c.Params("id"))
	if err != nil {
		return response.Error(c, apperror.BadRequest("Invalid user ID"))
	}

	if err := h.svc.ReactivateUser(
		c.Context(), actorID, targetID, getUserRole(c),
		c.IP(), c.Get("User-Agent"),
	); err != nil {
		return response.Error(c, err)
	}

	return response.JSON(c, 200, fiber.Map{"status": "reactivated"})
}

func (h *AdminHandler) ChangeUserRole(c *fiber.Ctx) error {
	actorID, err := getUserID(c)
	if err != nil {
		return response.Error(c, err)
	}

	targetID, err := uuid.Parse(c.Params("id"))
	if err != nil {
		return response.Error(c, apperror.BadRequest("Invalid user ID"))
	}

	var req struct {
		Role string `json:"role"`
	}
	if err := c.BodyParser(&req); err != nil {
		return response.Error(c, apperror.BadRequest("Invalid request body"))
	}

	if err := h.svc.ChangeUserRole(
		c.Context(), actorID, targetID, getUserRole(c), req.Role,
		c.IP(), c.Get("User-Agent"),
	); err != nil {
		return response.Error(c, err)
	}

	return response.JSON(c, 200, fiber.Map{"status": "role_changed", "new_role": req.Role})
}

func (h *AdminHandler) GetAuditLog(c *fiber.Ctx) error {
	actorID, err := getUserID(c)
	if err != nil {
		return response.Error(c, err)
	}

	filter, err := parseAuditFilter(c, false)
	if err != nil {
		return response.Error(c, err)
	}

	entries, nextCursor, hasMore, err := h.svc.GetAuditLog(
		c.Context(), actorID, getUserRole(c), filter,
		c.IP(), c.Get("User-Agent"),
	)
	if err != nil {
		return response.Error(c, err)
	}

	return response.Paginated(c, entries, nextCursor, hasMore)
}

// parseAuditFilter extracts AuditFilter values from the fiber query string.
// Shared by /audit-log (paginated) and /audit-log/export (streaming) so the
// two endpoints stay in lockstep.
//
// Whitelisting `action` and `target_type` against the in-code registries is
// defense-in-depth: the store binds parameters, but rejecting unknown values
// at the boundary keeps audit log details predictable for compliance review.
//
// When forExport=true, Cursor/Limit are not parsed (the export ignores them).
func parseAuditFilter(c *fiber.Ctx, forExport bool) (store.AuditFilter, error) {
	filter := store.AuditFilter{}
	if !forExport {
		filter.Cursor = c.Query("cursor")
		filter.Limit = c.QueryInt("limit", 50)
	}

	if actorParam := c.Query("actor_id"); actorParam != "" {
		id, err := uuid.Parse(actorParam)
		if err != nil {
			return filter, apperror.BadRequest("invalid actor_id")
		}
		filter.ActorID = &id
	}
	if action := c.Query("action"); action != "" {
		if !auditValueAllowed(action, model.AuditActions()) {
			return filter, apperror.BadRequest("unknown action")
		}
		filter.Action = &action
	}
	if targetType := c.Query("target_type"); targetType != "" {
		if !auditValueAllowed(targetType, model.AuditTargetTypes()) {
			return filter, apperror.BadRequest("unknown target_type")
		}
		filter.TargetType = &targetType
	}
	if targetID := c.Query("target_id"); targetID != "" {
		if len(targetID) > 200 {
			return filter, apperror.BadRequest("target_id too long")
		}
		filter.TargetID = &targetID
	}
	if since := c.Query("since"); since != "" {
		t, err := parseAuditTime(since)
		if err != nil {
			return filter, apperror.BadRequest("invalid since")
		}
		filter.Since = &t
	}
	if until := c.Query("until"); until != "" {
		t, err := parseAuditTime(until)
		if err != nil {
			return filter, apperror.BadRequest("invalid until")
		}
		filter.Until = &t
	}
	// Free-text search across action / target / actor name / details. Cap at
	// 200 chars — long inputs are almost certainly mistakes (paste of a JWT
	// token, etc.) and would slow the ILIKE scan with no hit.
	if q := c.Query("q"); q != "" {
		if len(q) > 200 {
			q = q[:200]
		}
		filter.Q = q
	}
	return filter, nil
}

func auditValueAllowed(v string, allowed []string) bool {
	for _, a := range allowed {
		if v == a {
			return true
		}
	}
	return false
}

// parseAuditTime accepts either RFC3339 (the historical format) or the
// browser-native HTML date-input format (YYYY-MM-DD), which the admin UI
// produces from <input type="date">. The bare-date form is interpreted as
// UTC midnight of that day.
func parseAuditTime(s string) (time.Time, error) {
	if t, err := time.Parse(time.RFC3339, s); err == nil {
		return t, nil
	}
	if t, err := time.Parse("2006-01-02", s); err == nil {
		return t, nil
	}
	return time.Time{}, fmt.Errorf("invalid time %q", s)
}

// auditExportTimeout caps the stream end-to-end. 60s SET LOCAL inside the
// store is enforced by Postgres; this is the outer wall in case Postgres
// streams slowly into a stalled HTTP client.
const auditExportTimeout = 5 * time.Minute

// auditCSVHeader matches the column order written by streamAuditCSV — kept
// in sync by hand. Adding a column means updating both.
var auditCSVHeader = []string{
	"id", "created_at", "actor_id", "actor_name",
	"action", "target_type", "target_id",
	"ip_address", "user_agent", "details",
}

// ExportAuditLog streams the audit log as CSV. Same filters as GetAuditLog;
// cursor/limit ignored (server hard-cap applies).
//
// Order of operations is load-bearing: every check that MUST produce a
// real 4xx/5xx (permissions, filter validation, actor-role pivot guard,
// audit-first row write) runs BEFORE c.Set() and SetBodyStreamWriter. Once
// the stream writer is registered, fasthttp commits the 200 status before
// the callback fires — at that point the only error channel left is a
// "#error" CSV row inside a 200 response, which is indistinguishable from
// a successful empty export to a script that pipes the response.
func (h *AdminHandler) ExportAuditLog(c *fiber.Ctx) error {
	actorID, err := getUserID(c)
	if err != nil {
		return response.Error(c, err)
	}
	actorRole := getUserRole(c)
	filter, err := parseAuditFilter(c, true)
	if err != nil {
		return response.Error(c, err)
	}

	// Preflight runs all gates + audit-first synchronously on the request
	// context. If it fails, response.Error returns a structured 4xx/5xx
	// (and crucially, the audit-first row was either written or rejected
	// before any export work begins — fail-closed).
	if err := h.svc.PreflightAuditExport(c.Context(), actorID, actorRole, filter, c.IP(), c.Get("User-Agent")); err != nil {
		return response.Error(c, err)
	}

	stamp := time.Now().UTC().Format("20060102-150405")
	filename := fmt.Sprintf("audit-log-%s.csv", stamp)
	c.Set("Content-Type", "text/csv; charset=utf-8")
	c.Set("Content-Disposition", `attachment; filename="`+filename+`"`)
	c.Set("X-Content-Type-Options", "nosniff")

	// SetBodyStreamWriter runs after the handler returns — c.Context() is
	// gone by then, so we use a fresh background ctx with a hard cap.
	streamCtx, cancel := context.WithTimeout(context.Background(), auditExportTimeout)
	c.Context().SetBodyStreamWriter(func(w *bufio.Writer) {
		defer cancel()
		cw := csv.NewWriter(w)
		if err := cw.Write(auditCSVHeader); err != nil {
			writeCSVErrorRow(cw)
			cw.Flush()
			_ = w.Flush()
			return
		}
		cw.Flush()
		_ = w.Flush()

		emit := func(e model.AuditEntry) error {
			row := []string{
				fmt.Sprintf("%d", e.ID),
				e.CreatedAt.UTC().Format(time.RFC3339),
				e.ActorID.String(),
				csvSafe(e.ActorName),
				e.Action,
				e.TargetType,
				csvSafe(derefString(e.TargetID)),
				csvSafe(derefString(e.IPAddress)),
				csvSafe(derefString(e.UserAgent)),
				csvSafe(string(e.Details)),
			}
			if err := cw.Write(row); err != nil {
				return err
			}
			cw.Flush()
			if err := cw.Error(); err != nil {
				return err
			}
			return w.Flush()
		}
		if _, err := h.svc.StreamAuditExport(streamCtx, filter, emit); err != nil {
			// Internal err details (SQL fragments, statement_timeout payloads,
			// connection-string crumbs) MUST NOT leak into a CSV that ends up
			// on the analyst's machine. Log full error server-side; emit a
			// generic marker for the consumer.
			slog.Error("audit-log export stream failed", "error", err, "actor_id", actorID)
			writeCSVErrorRow(cw)
		}
		cw.Flush()
		_ = w.Flush()
	})
	return nil
}

// writeCSVErrorRow emits a fixed marker so a downstream consumer can tell
// "the export aborted" from "the export was empty". Deliberately carries no
// error detail (see slog at the call site).
func writeCSVErrorRow(cw *csv.Writer) {
	_ = cw.Write([]string{"#error", "export aborted; see server logs"})
}

func derefString(p *string) string {
	if p == nil {
		return ""
	}
	return *p
}

// csvSafe defangs CSV-formula injection: cells starting with =, +, -, @, or
// a leading TAB/CR are treated as formulas by Excel/LibreOffice/Google
// Sheets when the file is opened. audit_log fields like user_agent and the
// JSON details column carry attacker-controllable values, so any analyst
// double-clicking an export risks formula execution. Defang by prefixing
// dangerous cells with a single quote — the de-facto industry workaround
// (also recommended by OWASP "CSV Injection"). encoding/csv handles the
// outer quoting; the leading apostrophe stays inside the quoted cell and
// is read by spreadsheet apps as a literal string-prefix marker.
func csvSafe(s string) string {
	if s == "" {
		return s
	}
	switch s[0] {
	case '=', '+', '-', '@', '\t', '\r':
		return "'" + s
	}
	return s
}

func (h *AdminHandler) ExportChat(c *fiber.Ctx) error {
	actorID, err := getUserID(c)
	if err != nil {
		return response.Error(c, err)
	}
	actorRole := getUserRole(c)
	ip := c.IP()
	ua := c.Get("User-Agent")
	if !permissions.HasSysPermission(actorRole, permissions.SysExportData) {
		return response.Error(c, apperror.Forbidden("Insufficient permissions"))
	}
	chatID := c.Params("id")
	streamCtx, cancel := context.WithTimeout(context.Background(), exportStreamTimeout)
	c.Set("Content-Type", "application/x-ndjson")
	c.Set("Content-Disposition", "attachment; filename=\"chat-"+chatID+".ndjson\"")
	c.Context().SetBodyStreamWriter(func(w *bufio.Writer) {
		defer cancel()
		if exportErr := h.svc.ExportChatMessages(streamCtx, actorID, actorRole, chatID,
			ip, ua,
			func(row []byte) error {
				if _, writeErr := w.Write(append(row, '\n')); writeErr != nil {
					return writeErr
				}
				return w.Flush()
			}); exportErr != nil {
			errRow, _ := json.Marshal(map[string]string{"error": exportErr.Error()})
			_, _ = w.Write(append(errRow, '\n'))
			_ = w.Flush()
		}
	})
	return nil
}

func (h *AdminHandler) ExportUser(c *fiber.Ctx) error {
	actorID, err := getUserID(c)
	if err != nil {
		return response.Error(c, err)
	}
	actorRole := getUserRole(c)
	ip := c.IP()
	ua := c.Get("User-Agent")
	if !permissions.HasSysPermission(actorRole, permissions.SysExportData) {
		return response.Error(c, apperror.Forbidden("Insufficient permissions"))
	}
	userID := c.Params("id")
	streamCtx, cancel := context.WithTimeout(context.Background(), exportStreamTimeout)
	c.Set("Content-Type", "application/x-ndjson")
	c.Set("Content-Disposition", "attachment; filename=\"user-"+userID+".ndjson\"")
	c.Context().SetBodyStreamWriter(func(w *bufio.Writer) {
		defer cancel()
		if exportErr := h.svc.ExportUserData(streamCtx, actorID, actorRole, userID,
			ip, ua,
			func(row []byte) error {
				if _, writeErr := w.Write(append(row, '\n')); writeErr != nil {
					return writeErr
				}
				return w.Flush()
			}); exportErr != nil {
			errRow, _ := json.Marshal(map[string]string{"error": exportErr.Error()})
			_, _ = w.Write(append(errRow, '\n'))
			_ = w.Flush()
		}
	})
	return nil
}

// SetChatDefaultStatus toggles is_default_for_new_users on a chat. Body:
//
//	{ "is_default": bool, "default_join_order": int }
//
// Service layer enforces SysManageSettings (admin/superadmin) and validates
// join order range; we only parse + relay here.
func (h *AdminHandler) SetChatDefaultStatus(c *fiber.Ctx) error {
	actorID, err := getUserID(c)
	if err != nil {
		return response.Error(c, err)
	}
	chatID, err := uuid.Parse(c.Params("id"))
	if err != nil {
		return response.Error(c, apperror.BadRequest("Invalid chat ID"))
	}
	var req struct {
		IsDefault        bool `json:"is_default"`
		DefaultJoinOrder int  `json:"default_join_order"`
	}
	if err := c.BodyParser(&req); err != nil {
		return response.Error(c, apperror.BadRequest("Invalid body"))
	}
	if err := h.svc.SetChatDefaultStatus(c.Context(), actorID, getUserRole(c),
		chatID, req.IsDefault, req.DefaultJoinOrder, c.IP(), c.Get("User-Agent")); err != nil {
		return response.Error(c, err)
	}
	return response.JSON(c, fiber.StatusOK, fiber.Map{
		"chat_id":            chatID.String(),
		"is_default":         req.IsDefault,
		"default_join_order": req.DefaultJoinOrder,
	})
}

// BackfillDefaultMemberships joins every existing user to every chat marked
// is_default_for_new_users=true. Manual admin action — never wired to the
// flag-flip itself. Returns the count of newly-inserted memberships so the
// AdminPanel can show "Joined N memberships." after the confirmation modal.
func (h *AdminHandler) BackfillDefaultMemberships(c *fiber.Ctx) error {
	actorID, err := getUserID(c)
	if err != nil {
		return response.Error(c, err)
	}
	count, err := h.svc.BackfillDefaultMemberships(c.Context(), actorID, getUserRole(c),
		c.IP(), c.Get("User-Agent"))
	if err != nil {
		return response.Error(c, err)
	}
	return response.JSON(c, fiber.StatusOK, fiber.Map{"inserted": count})
}
