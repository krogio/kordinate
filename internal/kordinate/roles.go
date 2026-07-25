package kordinate

import (
	"context"
	"database/sql"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/krogio/kore/user"
	"github.com/krogio/kore/web/reqctx"
)

// roles.go is kordinate's authorisation, layered on kore's three axes:
// licence → section access → role.
//
// Almost everything here is expressible in those three: a route's minimum role
// gates the mutation, and an access.Section rule gates which groups reach a
// feature at all (managed in the access admin UI, keyed on stable group IDs).
// Section rules are how, for example, sales and marketing are kept off
// /kordinate/documents — one rule an operator can change, not a constant.
//
// ONE permission genuinely does not fit that model, and it is the reason this
// file exists at all:
//
//	Revealing an unredacted identity document.
//
// It cannot be a role, because admin is a statement about configuration rights,
// not a standing authorisation to read every customer's ID number, address and
// photograph. It cannot be a section rule, because the redacted and unredacted
// views are the same route. It is a per-user grant, persisted, administrable,
// and logged on every use.
//
// claire-admin's eleven flat roles map onto kore groups + the route role via
// MapLegacyRole, so an existing user list migrates without hand-mapping every
// account.

// CanEdit reports whether the caller may mutate domain data. Mirrors konform's
// canAdmin shape: read the role off the request, don't cache a permission set.
func canEdit(r *http.Request) bool {
	if u := reqctx.User(r.Context()); u != nil {
		return u.Role.CanEdit()
	}
	return false
}

// canManage reports whether the caller may perform admin-only actions.
func canManage(r *http.Request) bool {
	if u := reqctx.User(r.Context()); u != nil {
		return u.Role.CanManage()
	}
	return false
}

// principal is the signed-in user's email, used as the actor on audit and
// access-log writes. Empty for an unauthenticated request, which the route
// guards already prevent reaching a handler.
func principal(r *http.Request) string {
	if u := reqctx.User(r.Context()); u != nil {
		return u.Email
	}
	return ""
}

// account is the request's tenant account, which scopes every store query.
func account(r *http.Request) string { return reqctx.Account(r.Context()) }

// ---------- the unredacted-document grant ----------

// RevealGrant is permission for one user to view unredacted identity documents.
//
// Granted per user rather than per group: it is the most sensitive read in the
// product, the population who legitimately need it is small and named, and a
// group grant would silently extend to anyone later added to that group.
type RevealGrant struct {
	Email     string
	GrantedBy string
	Reason    string
	GrantedAt time.Time
	// ExpiresAt bounds the grant. A standing permission to read every
	// customer's ID is what an audit finding looks like; an investigation has
	// an end date, so the grant should too. Nil means no expiry, which is
	// allowed but should be rare.
	ExpiresAt *time.Time
}

// Active reports whether the grant is currently in force.
func (g *RevealGrant) Active(now time.Time) bool {
	return g != nil && (g.ExpiresAt == nil || now.Before(*g.ExpiresAt))
}

// CanReveal reports whether a principal may view unredacted documents.
//
// Deliberately NOT satisfied by any role, including admin. A failed lookup
// denies: this permission must fail closed, because the cost of wrongly
// allowing it is exposing a customer's identity document.
func (s *Store) CanReveal(ctx context.Context, acct, email string) bool {
	if email == "" {
		return false
	}
	g, err := s.RevealGrant(ctx, acct, email)
	if err != nil || g == nil {
		return false
	}
	return g.Active(s.now())
}

// RevealGrant loads a principal's grant, or nil when they hold none.
func (s *Store) RevealGrant(ctx context.Context, acct, email string) (*RevealGrant, error) {
	row := s.db.QueryRowContext(ctx, `SELECT email, granted_by, reason, granted_at, expires_at
		FROM kdn_reveal_grants WHERE account=? AND email=?`, acct, strings.ToLower(email))

	var g RevealGrant
	if err := row.Scan(&g.Email, &g.GrantedBy, &g.Reason, &g.GrantedAt, &g.ExpiresAt); err != nil {
		if err == sql.ErrNoRows {
			return nil, nil
		}
		return nil, fmt.Errorf("reveal grant: %w", err)
	}
	return &g, nil
}

// RevealGrants lists every grant, for the admin screen. This list IS the answer
// to "who can see unredacted customer documents" — the question claire-admin
// could only answer by grepping route middleware.
func (s *Store) RevealGrants(ctx context.Context, acct string) ([]RevealGrant, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT email, granted_by, reason, granted_at, expires_at
		FROM kdn_reveal_grants WHERE account=? ORDER BY email`, acct)
	if err != nil {
		return nil, fmt.Errorf("reveal grants: %w", err)
	}
	defer rows.Close()

	var out []RevealGrant
	for rows.Next() {
		var g RevealGrant
		if err := rows.Scan(&g.Email, &g.GrantedBy, &g.Reason, &g.GrantedAt, &g.ExpiresAt); err != nil {
			return nil, fmt.Errorf("reveal grants: %w", err)
		}
		out = append(out, g)
	}
	return out, rows.Err()
}

// GrantReveal grants (or re-grants) the unredacted-document permission.
func (s *Store) GrantReveal(ctx context.Context, acct string, g RevealGrant) error {
	_, err := s.db.ExecContext(ctx, `INSERT INTO kdn_reveal_grants
		(account, email, granted_by, reason, granted_at, expires_at) VALUES (?,?,?,?,?,?)
		ON DUPLICATE KEY UPDATE granted_by=VALUES(granted_by), reason=VALUES(reason),
		 granted_at=VALUES(granted_at), expires_at=VALUES(expires_at)`,
		acct, strings.ToLower(g.Email), g.GrantedBy, g.Reason, s.now().UTC(), g.ExpiresAt)
	if err != nil {
		return fmt.Errorf("grant reveal to %s: %w", g.Email, err)
	}
	return nil
}

// RevokeReveal removes the grant.
func (s *Store) RevokeReveal(ctx context.Context, acct, email string) error {
	_, err := s.db.ExecContext(ctx, `DELETE FROM kdn_reveal_grants WHERE account=? AND email=?`,
		acct, strings.ToLower(email))
	if err != nil {
		return fmt.Errorf("revoke reveal from %s: %w", email, err)
	}
	return nil
}

// ---------- claire-admin role migration ----------

// legacyRoles maps each claire-admin role to the kore group it becomes and the
// role it implies. Sourced from claire-admin's README permission table and the
// middleware on its routes.
//
// Groups are created by name once at migration time; from then on kore's access
// rules key on the group's stable ID, so renaming a group in the admin UI can't
// silently change anyone's permissions.
var legacyRoles = map[string]struct {
	Group string
	Role  user.Role
}{
	"super_administrator":             {"CE Supervisor", user.RoleAdmin},
	"claire_administrator":            {"CE Team", user.RoleEditor},
	"mega_admin":                      {"Admin", user.RoleAdmin},
	"sales_marketing":                 {"Sales & Marketing", user.RoleViewer},
	"marketing":                       {"Marketing", user.RoleViewer},
	"activations_team":                {"Activations", user.RoleEditor},
	"activations_supervisor_aml_team": {"Activations & AML", user.RoleAdmin},
	"junior_finance":                  {"Junior Finance", user.RoleEditor},
	"senior_finance":                  {"Senior Finance", user.RoleAdmin},
	"card_specialist":                 {"Card Specialist", user.RoleEditor},
	"card_supervisor":                 {"Card Supervisor", user.RoleAdmin},
}

// MapLegacyRole translates a claire-admin role name to the kordinate group and
// kore role it becomes.
//
// An unknown role maps to the most restrictive combination rather than
// erroring: a migration must not grant access it can't explain, and must not
// fail the whole import over one stale row. The bool reports whether the role
// was recognised, so the caller can log what it downgraded.
func MapLegacyRole(r string) (group string, role user.Role, known bool) {
	if m, ok := legacyRoles[strings.ToLower(strings.TrimSpace(r))]; ok {
		return m.Group, m.Role, true
	}
	return "Marketing", user.RoleViewer, false
}
