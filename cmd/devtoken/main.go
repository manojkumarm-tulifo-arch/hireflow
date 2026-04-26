// devtoken issues a development JWT signed with JWT_ACCESS_SECRET.
// Local-only — never deploy.
//
// Usage:
//   go run ./cmd/devtoken
//   go run ./cmd/devtoken --tenant <uuid> --recruiter <uuid> --ttl 24h
//   JWT_ACCESS_SECRET=... go run ./cmd/devtoken
package main

import (
	"flag"
	"fmt"
	"os"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"github.com/google/uuid"
)

func main() {
	tenant := flag.String("tenant", "", "tenant UUID (default: random)")
	recruiter := flag.String("recruiter", "", "recruiter UUID (default: random)")
	ttl := flag.Duration("ttl", 24*time.Hour, "token lifetime")
	issuer := flag.String("issuer", "hireflow", "iss claim")
	secret := flag.String("secret", "", "HS256 secret (defaults to $JWT_ACCESS_SECRET)")
	flag.Parse()

	s := *secret
	if s == "" {
		s = os.Getenv("JWT_ACCESS_SECRET")
	}
	if s == "" {
		fmt.Fprintln(os.Stderr, "error: --secret or $JWT_ACCESS_SECRET is required")
		os.Exit(1)
	}

	tenantID := *tenant
	if tenantID == "" {
		tenantID = uuid.New().String()
	}
	recruiterID := *recruiter
	if recruiterID == "" {
		recruiterID = uuid.New().String()
	}

	now := time.Now()
	claims := jwt.MapClaims{
		"tenant_id":    tenantID,
		"recruiter_id": recruiterID,
		"iss":          *issuer,
		"sub":          recruiterID,
		"iat":          now.Unix(),
		"nbf":          now.Unix(),
		"exp":          now.Add(*ttl).Unix(),
	}
	tok := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
	signed, err := tok.SignedString([]byte(s))
	if err != nil {
		fmt.Fprintf(os.Stderr, "sign: %v\n", err)
		os.Exit(1)
	}

	fmt.Fprintf(os.Stderr, "tenant_id    %s\n", tenantID)
	fmt.Fprintf(os.Stderr, "recruiter_id %s\n", recruiterID)
	fmt.Fprintf(os.Stderr, "expires      %s\n\n", now.Add(*ttl).Format(time.RFC3339))
	fmt.Println(signed)
}
