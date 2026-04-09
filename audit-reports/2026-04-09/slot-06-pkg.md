# Audit Report: pkg/ - Shared Infrastructure
**Slot:** 06  
**Date:** 2026-04-09  
**Auditor:** Senior Security and Code Auditor (Claude)  
**Scope:** pkg/ - apperror, response, config, validator, permissions, migrator  
**Commit:** f6aaf05

---

## Scope

    pkg/apperror/apperror.go
    pkg/response/response.go
    pkg/config/config.go
    pkg/config/parse_test.go
    pkg/validator/validator.go
    pkg/permissions/permissions.go
    pkg/permissions/permissions_test.go
    pkg/permissions/system.go
    pkg/permissions/system_test.go
    pkg/migrator/migrator.go

---

## Audit Checklist

- [x] apperror.Internal() - does it leak msg to client?
- [x] response.Error() - unwrap path, unhandled error fallback
- [x] response.FiberErrorHandler - fiber error normalization, leak check
- [x] response.Paginated - cursor format, nil-slice guard
- [x] config.MustEnv - panic behavior, whitespace edge case
- [x] config.EnvOr / EnvIntOr / EnvDurationOr - fallback correctness, parse-error handling
- [x] config.DatabaseDSN - password separation, Saturn backslash handling, rawPassword
- [x] config.parsePostgresURL - manual parse correctness, edge cases
- [x] config.RedactURL - empty, malformed, no-scheme, keyword-value DSN
- [x] config.NatsURL - port normalization edge cases
- [x] validator.RequireString - empty, whitespace-only, zero-width unicode, len vs rune count
- [x] validator.RequireEmail - RFC compliance, display-name format, empty
- [x] validator.RequireUUID - regex correctness, case sensitivity
- [x] validator.IsValidEmail - net/mail behavior for edge cases
- [x] permissions.EffectivePermissions - role mapping, chatType param usage
- [x] permissions.CanPerform - bitmask logic
- [x] permissions.int64 overflow safety
- [x] permissions.system - rolePermissions map, unknown role fallback
- [x] permissions.HasSysPermission - unknown role returns 0
- [x] permissions.CanAssignRole / CanModifyUser - role validation
- [x] migrator.Run - file ordering, idempotency, transaction safety
- [x] migrator.applyOne - rollback on failure
- [x] migrator.checksum - stored but not verified on re-run
- [x] migrator - legacy DB bootstrap logic
- [x] crypto/subtle - constant-time compares for secrets
- [x] bcrypt cost - hardcoded vs configurable
- [x] Test coverage gaps in pkg/

---

## Progress Log

1. Read CLAUDE.md - project conventions, security rules, pkg usage patterns
2. Globbed all pkg/**/*.go - 10 files found
3. Read all source and test files in pkg/
4. Traced response.Error -> apperror.Internal leak path
5. Analyzed EffectivePermissions chatType parameter - critical bug found
6. Analyzed permissions tests - two test/implementation mismatches found
7. Analyzed validator whitespace/unicode edge cases
8. Analyzed config.RedactURL with keyword-value DSN
9. Analyzed migrator checksum logic, ordering, transaction safety
10. Verified crypto/subtle usage in all services
11. Confirmed bcrypt cost=12 hardcoded (correct per TZ)
12. Catalogued test coverage gaps

---

## Findings

---

### FINDING-01

**File:** `pkg/permissions/permissions.go:45-62`  
**Category:** HIGH - Logic Error / Security Bypass  
**Title:** chatType parameter silently ignored - channel members receive full permissions

**Description:**  
EffectivePermissions(role, chatType string, memberPerms, defaultPerms int64) accepts chatType as
a parameter but never reads it. For a member in a channel, the function falls through to the
default branch and returns memberPerms (if set) or defaultPerms. A channel member with
memberPerms = AllPermissions (255) gets all permissions - CanSendMessages, CanDeleteMessages,
CanBanUsers, etc. - despite channels semantically being read-only for members.

**Evidence:**  
The test TestEffectivePermissions_MemberChannel_AlwaysZero (permissions_test.go:64-70) calls:

    EffectivePermissions("member", "channel", AllPermissions, AllPermissions)
    // expects: 0
    // actual:  255  (AllPermissions != PermissionsUnset(-1))

chatType is never referenced anywhere in the switch body. This test FAILS at runtime.

**Verified:** Yes - EffectivePermissions code contains no reference to chatType.

**Impact:**  
- Channel members can send messages, delete messages, ban users if their per-member or
  default permissions bits are set.
- Any service calling CanPerform with channel chatType and non-sentinel memberPerms gets true.
- All four services (auth, messaging, calls, gateway) use CanPerform via this package.

**Fix:**  
Add channel handling before the default branch in EffectivePermissions:

    default: // member
        if chatType == "channel" {
            return 0  // channel members have no write permissions
        }
        if memberPerms != PermissionsUnset {
            return memberPerms
        }
        return defaultPerms

---

### FINDING-02

**File:** `pkg/permissions/permissions_test.go:16-21`  
**Category:** HIGH - Test/Implementation Mismatch (masks logic bug)  
**Title:** TestEffectivePermissions_Admin_DefaultPerms will fail at runtime

**Description:**  
The test passes memberPerms=0 and expects DefaultAdminPermissions (255). But PermissionsUnset = -1
is the sentinel for not customized. Since 0 != -1, the code treats 0 as explicitly set to no
permissions and returns 0, not DefaultAdminPermissions.

**Evidence:**  

    // permissions_test.go:17
    got := EffectivePermissions("admin", "group", 0, 100)
    if got != DefaultAdminPermissions { ... }   // expects 255

    // permissions.go:49-52
    case "admin":
        if memberPerms != PermissionsUnset {   // 0 != -1 -> true
            return memberPerms                 // returns 0
        }
        return DefaultAdminPermissions         // never reached

This test FAILS at runtime.

**Verified:** Yes - PermissionsUnset = -1; memberPerms=0 hits the first branch and returns 0.

**Impact:**  
- Test suite gives false signal of correctness.
- Callers creating an admin with memberPerms=0 expecting default admin perms get zero permissions.
- The sentinel pattern (only -1 means unset) is correct but the test contradicts it.

**Fix:**  
Fix the test to pass PermissionsUnset to represent no custom override:

    got := EffectivePermissions("admin", "group", PermissionsUnset, 100)
    // now correctly returns DefaultAdminPermissions

---

### FINDING-03

**File:** `pkg/validator/validator.go:40-52`  
**Category:** MEDIUM - Input Validation Gap  
**Title:** RequireString accepts whitespace-only strings and zero-width unicode as valid

**Description:**  
RequireString checks if val equals empty string for rejection, then counts runes.
A string of spaces, tabs, or zero-width Unicode (U+200B ZERO WIDTH SPACE, U+FEFF BOM,
U+200C ZWNJ) passes the empty check and satisfies minLen >= 1.
RequireString of a spaces-only string with minLen=1 returns nil (valid).

**Evidence:**  
No strings.TrimSpace or unicode-category filtering is present in validator.go.
A string of 3 spaces has runes=3, passes minLen=1, and returns nil (valid).

**Verified:** Yes.

**Impact:**  
- User display names, chat group names can be set to visually empty strings.
- Downstream storage accepts invisible-character values; UI renders blank-looking entries.

**Fix:**  

    func RequireString(val, field string, minLen, maxLen int) *apperror.AppError {
        val = strings.TrimSpace(val)
        if val == "" {
            return apperror.BadRequest(field + " is required")
        }
        runes := utf8.RuneCountInString(val)
        // ... rest unchanged

---

### FINDING-04

**File:** `pkg/validator/validator.go:11`  
**Category:** MEDIUM - Correctness  
**Title:** UUID regex is lowercase-only - valid uppercase UUIDs from clients are rejected

**Description:**  
The UUID regex is case-sensitive lowercase only:
    ^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$
RFC 4122 permits uppercase hex in UUIDs. A UUID like 550E8400-E29B-41D4-A716-446655440000
passes Go standard uuid.Parse (used elsewhere in services) but fails IsValidUUID.

**Evidence:**  
    var uuidRegex = regexp.MustCompile(...)  // lowercase-only, no (?i) flag, no ToLower

**Verified:** Yes.

**Impact:**  
- Clients (Windows GUIDs, some mobile SDKs) sending uppercase UUIDs receive spurious 400 errors.
- Medium severity - incorrect rejection, not a security bypass.

**Fix:**  

    func IsValidUUID(s string) bool {
        return uuidRegex.MatchString(strings.ToLower(s))
    }

---

### FINDING-05

**File:** `pkg/config/config.go:27-32`  
**Category:** MEDIUM - Security / Configuration  
**Title:** MustEnv and EnvOr accept whitespace-only values - misconfigured secrets silently accepted

**Description:**  
MustEnv checks if v equals empty string only. A value of spaces-only is treated as properly set.
For security-critical secrets (JWT_SECRET, INTERNAL_SECRET), a whitespace deployment error is
silently accepted and the whitespace string is used as the secret.

**Evidence:**  

    v := os.Getenv(key)
    if v == "" {     // spaces-only passes this check
        panic(...)
    }
    return v

subtle.ConstantTimeCompare of two identical whitespace strings returns 1.
Any attacker sending X-Internal-Token with the same whitespace passes the auth check.

**Verified:** Yes - no trimming anywhere in config.go.

**Impact:**  
- Whitespace JWT_SECRET: JWTs signed and verified with a trivially guessable secret.
- Whitespace INTERNAL_SECRET: internal token validation bypassed by any request with
  the same whitespace string in X-Internal-Token.
- Risk requires a deployment error but the function should guard against this class of mistake.

**Fix:**  

    func MustEnv(key string) string {
        v := strings.TrimSpace(os.Getenv(key))
        if v == "" {
            panic(fmt.Sprintf("required environment variable %s is not set or empty", key))
        }
        return v
    }

---

### FINDING-06

**File:** `pkg/config/config.go:15-24`  
**Category:** MEDIUM - Incorrect Behavior / Future Risk  
**Title:** RedactURL silently passes keyword-value DSN strings through unchanged

**Description:**  
DatabaseDSN() returns keyword-value DSNs like:
    host=myhost port=5432 user=myuser dbname=mydb sslmode=disable

When RedactURL(dbDSN) is called for logging, Go url.Parse does not error on this string
(treats it as a relative URL) and u.Redacted() returns it unchanged - no userinfo to strip.
Currently safe as the DSN contains no password, but RedactURL provides false confidence.

**Evidence:**  
Called in services/messaging/cmd/main.go:35 and services/calls/cmd/main.go:31.
Go url.Parse is lenient: keyword-value strings parse without error, making the stars fallback unreachable.

**Verified:** Yes.

**Impact:**  
- Currently low - DSN is password-free by design.
- If DatabaseDSN() is changed to embed credentials, RedactURL silently passes them to logs.

**Fix:**  
Document that RedactURL only works on URL-format strings. Add a comment at call sites noting
that dbDSN is guaranteed password-free. Or add a separate RedactDSN helper.

---

### FINDING-07

**File:** `pkg/migrator/migrator.go:83-86`  
**Category:** MEDIUM - Data Integrity  
**Title:** Migrator loads stored checksums but never verifies them against current file contents

**Description:**  
loadApplied fetches filename -> checksum pairs from schema_migrations. In the apply loop,
the check is only existence: if _, ok := applied[name]; ok { continue }. The stored checksum
value is never compared against the current file checksum. A migration file edited after
initial application is silently skipped with no warning.

**Evidence:**  

    applied, _ := loadApplied(...)   // map[filename]checksum loaded
    for _, f := range files {
        name := filepath.Base(f)
        if _, ok := applied[name]; ok {   // existence check only
            continue                       // stored checksum never read
        }
        checksum := sha256Hex(content)    // computed only for new INSERT

**Verified:** Yes - applied[name] checksum value is never read in the loop.

**Impact:**  
- Silent schema drift: migration SQL changed on disk without being re-executed.
- Hard-to-debug production issues when expected schema changes are missing.

**Fix:**  

    if storedChecksum, ok := applied[name]; ok {
        content, _ := os.ReadFile(f)
        if sha256Hex(content) != storedChecksum {
            slog.Warn("migrator: checksum mismatch for already-applied migration",
                "file", name, "stored", storedChecksum)
        }
        continue
    }

---

### FINDING-08

**File:** `pkg/migrator/migrator.go:61-75`  
**Category:** MEDIUM - Logic / Reliability  
**Title:** Legacy DB bootstrap heuristic based on users table existence is fragile

**Description:**  
When schema_migrations does not exist, the migrator checks if the users table exists.
If it does, all migrations are marked applied without execution. A shared PostgreSQL instance
or a misconfigured DATABASE_URL pointing to any DB with a users table causes all Orbit
migrations to be silently skipped.

**Evidence:**  

    if !tableExisted {
        legacy, _ := tableExists(ctx, pool, "users")
        if legacy {
            markAllApplied(...)  // ALL migrations skipped, no error
            return nil
        }
    }

**Verified:** Yes.

**Impact:**  
- Silent failure: no schema created, no error returned, service starts and fails at first DB call.
- Low probability but high impact in a misconfiguration scenario.

**Fix:**  
Gate on an explicit env var: ORBIT_LEGACY_DB_BOOTSTRAP=true. Or check for an Orbit-specific
marker table or column rather than the generic users table.

---

## Summary

| ID | Severity | Package | Title |
|----|----------|---------|-------|
| FINDING-01 | HIGH | pkg/permissions | chatType ignored - channel members receive full permissions |
| FINDING-02 | HIGH | pkg/permissions | Test/impl mismatch: admin memberPerms=0 returns 0 not defaults |
| FINDING-03 | MEDIUM | pkg/validator | RequireString accepts whitespace-only and zero-width unicode |
| FINDING-04 | MEDIUM | pkg/validator | UUID regex lowercase-only - uppercase UUIDs rejected |
| FINDING-05 | MEDIUM | pkg/config | MustEnv/EnvOr accept whitespace-only values as valid |
| FINDING-06 | MEDIUM | pkg/config | RedactURL on keyword-value DSN passes through unchanged |
| FINDING-07 | MEDIUM | pkg/migrator | Stored checksums never verified against current file contents |
| FINDING-08 | MEDIUM | pkg/migrator | Legacy-DB bootstrap heuristic fragile (users table check) |

**Confirmed safe:**
- apperror.Internal() - correctly discards msg, always returns Internal server error (apperror.go:41)
- response.Error() - unhandled non-AppError calls apperror.Internal() which discards msg before JSON (response.go:26-27)
- response.FiberErrorHandler - fiber.Error path normalizes to http.StatusText(code), no detail exposed (response.go:54-56)
- response.Paginated - nil slice guard correct; cursor passed through as-is
- DatabaseDSN - password correctly separated from DSN; Saturn backslash variants tried in auth/main.go:55-66
- parsePostgresURL - splits on last @ handling literal @ in passwords; URL-decodes with url.PathUnescape
- config.NatsURL - port normalization correctly avoids corrupting :8080 to :42228 via suffix check
- permissions.HasSysPermission - unknown roles return 0 from map lookup (Go zero-value for missing key)
- permissions.CanAssignRole - validates newRole against ValidSystemRoles before proceeding
- permissions.CanModifyUser - checks actorRank==0 || targetRank==0 to reject unknown roles
- permissions int64 bitmask - AllPermissions=255, AllSysPermissions=2047; well within int64 range
- crypto/subtle.ConstantTimeCompare - used consistently for X-Internal-Token, X-Bootstrap-Secret, admin reset key
- bcrypt cost 12 - hardcoded in auth_service.go, matches TZ security requirement of cost >= 12

---

## Low Bucket

- pkg/validator: no test file - validator.go has no _test.go. Edge cases have no unit-level coverage.
- pkg/response: no test file - FiberErrorHandler normalization and nil-slice Paginated behavior untested.
- pkg/migrator: no test file - ordering, legacy bootstrap, and rollback on SQL failure are untested.
- pkg/apperror: no test file - Internal() msg-suppression has no test asserting msg is not in output struct.
- config.EnvIntOr parse error silent - ENV_VAR=abc silently returns fallback with no log warning.
- config.EnvDurationOr parse error silent - same pattern as EnvIntOr.
- validator.IsValidEmail - net/mail accepts quoted local-parts with spaces (RFC 5321 valid but rejected by most mail systems).
- permissions.DefaultGroupPermissions - CanInviteViaLink excluded while CanAddMembers included; asymmetry warrants a comment.

---

## Status: COMPLETED
