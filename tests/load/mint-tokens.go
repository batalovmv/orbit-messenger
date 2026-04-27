// Mints 150 JWT access tokens for the load test users seeded by
// seed-loadtest-users.sql, plus the matching session rows in postgres
// (auth's ValidateAccessToken cross-checks the jti against user_sessions).
//
// Why this exists: gateway+auth share a 5/min/IP rate limit on /auth/login,
// so 150 sequential setup logins from the k6 container would take 30 min.
// Since we own JWT_SECRET on the test box, mint tokens locally and skip the
// hot path entirely. Production load tests against Saturn would use a
// distributed IP source instead.
//
// Run from repo root:
//
//	cd tests/load && go run mint-tokens.go > tokens.json
//
// Reads .env for JWT_SECRET + DATABASE_URL.
package main

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
)

type Token struct {
	Email string `json:"email"`
	Token string `json:"token"`
}

func loadDotEnv(path string) map[string]string {
	out := map[string]string{}
	b, err := os.ReadFile(path)
	if err != nil {
		return out
	}
	for _, line := range strings.Split(string(b), "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		if eq := strings.Index(line, "="); eq > 0 {
			k := strings.TrimSpace(line[:eq])
			v := strings.TrimSpace(line[eq+1:])
			v = strings.Trim(v, "\"'")
			out[k] = v
		}
	}
	return out
}

func main() {
	envPath := os.Getenv("ENV_FILE")
	if envPath == "" {
		envPath = "../../.env"
	}
	env := loadDotEnv(envPath)
	jwtSecret := env["JWT_SECRET"]
	if jwtSecret == "" {
		fmt.Fprintln(os.Stderr, "JWT_SECRET missing in .env")
		os.Exit(1)
	}
	dsn := os.Getenv("DATABASE_URL")
	if dsn == "" {
		// Default for local stack: 127.0.0.1:5434, password from .env, user/db orbit.
		pw := env["POSTGRES_PASSWORD"]
		dsn = fmt.Sprintf("postgres://orbit:%s@127.0.0.1:5434/orbit?sslmode=disable", pw)
	}

	ctx := context.Background()
	pool, err := pgxpool.New(ctx, dsn)
	if err != nil {
		fmt.Fprintln(os.Stderr, "pool:", err)
		os.Exit(1)
	}
	defer pool.Close()

	out := []Token{}
	now := time.Now()
	exp := now.Add(2 * time.Hour) // longer than the load test will run

	for i := 0; i < 150; i++ {
		email := fmt.Sprintf("loadtest_%d@orbit.local", i)
		var userID uuid.UUID
		var role string
		err := pool.QueryRow(ctx,
			`SELECT id, role FROM users WHERE email = $1`, email,
		).Scan(&userID, &role)
		if err != nil {
			fmt.Fprintf(os.Stderr, "user %s: %v\n", email, err)
			continue
		}

		// Session row keyed by id = jti claim. Reruns wipe-and-reinsert via
		// per-user UA tag so we don't accumulate dead rows across runs.
		_, _ = pool.Exec(ctx,
			`DELETE FROM sessions WHERE user_id = $1 AND user_agent = 'k6-loadtest'`,
			userID,
		)
		sessID := uuid.New()
		_, err = pool.Exec(ctx,
			`INSERT INTO sessions (id, user_id, token_hash, user_agent, expires_at)
			 VALUES ($1, $2, $3, 'k6-loadtest', $4)`,
			sessID, userID, fmt.Sprintf("loadtest-token-hash-%d", i), exp,
		)
		if err != nil {
			fmt.Fprintf(os.Stderr, "session for %s: %v\n", email, err)
			continue
		}

		claims := jwt.MapClaims{
			"sub":  userID.String(),
			"role": role,
			"iat":  now.Unix(),
			"exp":  exp.Unix(),
			"jti":  sessID.String(),
		}
		t := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
		signed, err := t.SignedString([]byte(jwtSecret))
		if err != nil {
			fmt.Fprintf(os.Stderr, "sign %s: %v\n", email, err)
			continue
		}
		out = append(out, Token{Email: email, Token: signed})
	}

	enc := json.NewEncoder(os.Stdout)
	enc.SetIndent("", "  ")
	if err := enc.Encode(out); err != nil {
		fmt.Fprintln(os.Stderr, "encode:", err)
		os.Exit(1)
	}
	fmt.Fprintf(os.Stderr, "minted %d tokens\n", len(out))
}
