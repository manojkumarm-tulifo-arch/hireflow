package valueobjects

import (
	"errors"
	"regexp"
	"strings"
)

// ErrInvalidTenantSlug is returned for slugs that don't meet the format.
var ErrInvalidTenantSlug = errors.New("invalid tenant slug")

// Slugs are URL-friendly: lowercase letters, digits, hyphens; 3-32 chars.
var slugRe = regexp.MustCompile(`^[a-z0-9](?:[a-z0-9\-]{1,30}[a-z0-9])?$`)

// TenantSlug is the human-friendly identifier used at signup time to pick
// which tenant a new user joins. The Tenant aggregate (when it exists) will
// own slug uniqueness; for now slugs are pre-seeded via migration.
type TenantSlug struct {
	value string
}

// NewTenantSlug validates and constructs a slug.
func NewTenantSlug(s string) (TenantSlug, error) {
	n := strings.ToLower(strings.TrimSpace(s))
	if !slugRe.MatchString(n) {
		return TenantSlug{}, ErrInvalidTenantSlug
	}
	return TenantSlug{value: n}, nil
}

func (t TenantSlug) String() string { return t.value }
