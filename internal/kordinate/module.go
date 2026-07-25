// Package kordinate is the domain module for kordinate: MamaMoney's customer
// operations console, and the replacement for the sunsetting claire-admin.
//
// claire-admin was a Laravel app with ~150 routes over ten microservices, eleven
// flat roles checked inline at each route, and no model of the work itself. This
// keeps the feature surface and adds what it lacked:
//
//   - an explicit onboarding lifecycle (onboarding.go) with SLAs and an audit
//     trail on every transition, instead of onboarding-by-eyeball;
//   - one stitched customer view (timeline.go) across every microservice,
//     instead of five screens an agent reconciles by hand;
//   - AI document vetting (docvet.go) and burnt-in PII redaction (redact.go);
//   - a capability model (roles.go) that can be read in one table.
//
// Everything customer-specific sits behind a seam — upstream clients behind
// interfaces with fakes, brand as a literal, the role map as data — so a
// partner variant is a new thin kore composition rather than a fork.
package kordinate

import (
	"context"
	"embed"
	"errors"
	"fmt"
	"html/template"
	"io/fs"
	"log/slog"
	"net/http"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/krogio/kore"
	"github.com/krogio/kore/access"
	"github.com/krogio/kore/audit"
	"github.com/krogio/kore/store"
	"github.com/krogio/kore/user"

	"github.com/krogio/kordinate/internal/kordinate/upstream"
)

//go:embed all:templates
var templates embed.FS

//go:embed static/kordinate.js static/redact.js
var staticFS embed.FS

// Module is the kordinate domain module.
type Module struct {
	d   kore.Deps
	st  *Store
	up  upstream.Set
	tl  *TimelineBuilder
	vet *Vetter
	now func() time.Time
}

// New builds the module. The upstream mode is resolved from env: without
// UPSTREAM_MODE=live it uses the deterministic fakes, so a local run needs no
// VPN and no credentials.
func New(d kore.Deps) *Module {
	cfg := upstream.FromEnv(envGetter(d))

	var set upstream.Set
	if cfg.Live() {
		set = upstream.NewLive(cfg)
		slog.Info("kordinate: using live upstream services")
	} else {
		set = upstream.NewFake()
		slog.Warn("kordinate: using FAKE upstream data — set UPSTREAM_MODE=live for real services")
	}

	st := NewStore(d.DB)
	return &Module{
		d:   d,
		st:  st,
		up:  set,
		tl:  NewTimelineBuilder(set, st),
		vet: NewVetter(d.AI),
		now: time.Now,
	}
}

// envGetter reads upstream config through kore's settings resolver first (so an
// operator can point kordinate at a different environment from the settings UI
// without a redeploy) and falls back to process env.
func envGetter(d kore.Deps) func(string) string {
	return func(key string) string {
		if d.SettingsResolver != nil {
			if v := strings.TrimSpace(d.SettingsResolver.GetOr(settingKey(key), "")); v != "" {
				return v
			}
		}
		return strings.TrimSpace(os.Getenv(key))
	}
}

// settingKey namespaces an env var name into a kordinate setting key, so
// upstream endpoints don't collide with other products' settings.
func settingKey(env string) string {
	return "kordinate_" + strings.ToLower(env)
}

func (m *Module) Key() string  { return "kordinate" }
func (m *Module) Name() string { return "Customer Ops" }

func (m *Module) Migrations() []store.Migration { return Migrations }
func (m *Module) Templates() fs.FS              { return templates }

// Sections declares the nav and access-control tree. Prefixes are matched
// longest-first by kore, so the more specific children win over /kordinate.
func (m *Module) Sections() []access.Section {
	return []access.Section{
		{Key: "kordinate", Label: "Customer Ops", NavOnly: true, Icon: "users"},

		{Key: "kdn_customers", Label: "Customers", Parent: "kordinate",
			Prefixes: []string{"/kordinate"}, Icon: "users"},
		{Key: "kdn_onboarding", Label: "Onboarding", Parent: "kordinate",
			Prefixes: []string{"/kordinate/onboarding"}, Icon: "clipboard"},
		{Key: "kdn_documents", Label: "Documents", Parent: "kordinate",
			Prefixes: []string{"/kordinate/documents"}, Icon: "file"},
		{Key: "kdn_deposits", Label: "Deposits & EFT", Parent: "kordinate",
			Prefixes: []string{"/kordinate/deposits"}, Icon: "bank"},
		{Key: "kdn_vouchers", Label: "Vouchers", Parent: "kordinate",
			Prefixes: []string{"/kordinate/vouchers"}, Icon: "ticket"},
		{Key: "kdn_devices", Label: "Devices", Parent: "kordinate",
			Prefixes: []string{"/kordinate/devices"}, Icon: "shield", AdminOnly: true},
		{Key: "kdn_access", Label: "Access log", Parent: "kordinate",
			Prefixes: []string{"/kordinate/access-log"}, Icon: "audit", AdminOnly: true},
	}
}

func (m *Module) Funcs() template.FuncMap {
	return template.FuncMap{
		"zar":         FormatZAR,
		"kdnState":    func(s State) string { return s.Label() },
		"kdnDocLabel": DocLabel,
		"kdnPIILabel": func(k PIIKind) string { return k.Label() },
		"kdnAdvisory": func() string { return Advisory },
		"kdnRelTime":  relTime,
	}
}

func (m *Module) Routes(r kore.Router) {
	// Customer search + 360 view.
	r.Get("/kordinate", m.customerSearch)
	r.Get("/kordinate/customer", m.customerView)
	r.Get("/kordinate/customer/timeline", m.customerTimeline) // JSON, lazy-loaded
	r.Get("/kordinate/customer/balances", m.customerBalances) // JSON, fetched async
	r.Post("/kordinate/customer/note", user.RoleViewer, m.addNote)
	r.Post("/kordinate/customer/status", user.RoleEditor, m.setStatus)
	r.Post("/kordinate/customer/risk", user.RoleEditor, m.setRisk)
	r.Post("/kordinate/customer/reset-pin", user.RoleEditor, m.resetLoginPIN)
	r.GetRole("/kordinate/customer/new", user.RoleEditor, m.customerNewForm)
	r.Post("/kordinate/customer/create", user.RoleEditor, m.createCustomer)
	r.Post("/kordinate/customer/edit", user.RoleEditor, m.editCustomer)

	// Card operations.
	r.Post("/kordinate/card/block", user.RoleEditor, m.cardBlock)
	r.Post("/kordinate/card/unblock", user.RoleEditor, m.cardUnblock)
	r.Post("/kordinate/card/reset-pin", user.RoleEditor, m.cardResetPIN)
	r.Post("/kordinate/card/reallocate", user.RoleEditor, m.cardReallocate)
	r.Post("/kordinate/card/opt-in", user.RoleEditor, m.bankingOptIn)

	// Onboarding lifecycle: the work queue and case actions.
	r.Get("/kordinate/onboarding", m.onboardingQueue)
	r.Get("/kordinate/onboarding/case", m.caseView)
	r.Post("/kordinate/onboarding/open", user.RoleEditor, m.openCase)
	r.Post("/kordinate/onboarding/move", user.RoleEditor, m.moveCase)
	r.Post("/kordinate/onboarding/assign", user.RoleEditor, m.assignCase)

	// Documents: review, AI vetting, redaction.
	r.Get("/kordinate/documents", m.documentQueue)
	r.Get("/kordinate/documents/view", m.documentView)
	r.Get("/kordinate/documents/file", m.documentFile) // serves the redacted copy
	r.Post("/kordinate/documents/upload", user.RoleEditor, m.documentUpload)
	r.Post("/kordinate/documents/approve", user.RoleEditor, m.documentApprove)
	r.Post("/kordinate/documents/vet", user.RoleEditor, m.documentVet)
	r.Post("/kordinate/documents/redact", user.RoleEditor, m.documentRedact)
	r.Post("/kordinate/documents/reveal", user.RoleEditor, m.documentReveal)

	// Deposits, EFT and refunds.
	r.Get("/kordinate/deposits", m.depositQueue)
	r.Post("/kordinate/deposits/assign", user.RoleEditor, m.depositAssign)
	r.Post("/kordinate/deposits/refund", user.RoleEditor, m.depositRefund)
	r.Post("/kordinate/deposits/success", user.RoleEditor, m.depositSuccess)

	// Vouchers.
	r.Get("/kordinate/vouchers", m.voucherList)
	r.Post("/kordinate/vouchers/cancel", user.RoleEditor, m.voucherCancel)
	r.Post("/kordinate/vouchers/create", user.RoleEditor, m.voucherCreate)

	// Device blocker (fraud containment).
	r.GetRole("/kordinate/devices", user.RoleEditor, m.deviceLookup)
	r.Post("/kordinate/devices/status", user.RoleAdmin, m.deviceSetStatus)
	r.Post("/kordinate/devices/bulk-suspend", user.RoleAdmin, m.deviceBulkSuspend)

	// Access log: who looked at whose data.
	r.GetRole("/kordinate/access-log", user.RoleAdmin, m.accessLogView)
	// Who may view an unredacted identity document — the one permission no role
	// confers. Managed here because this is the screen that shows who has.
	r.Post("/kordinate/access-log/grant-reveal", user.RoleAdmin, m.grantReveal)
	r.Post("/kordinate/access-log/revoke-reveal", user.RoleAdmin, m.revokeReveal)

	// Static assets.
	r.Get("/kordinate/static/kordinate.js", m.assetJS("static/kordinate.js"))
	r.Get("/kordinate/static/redact.js", m.assetJS("static/redact.js"))
}

// ---------- request context helpers ----------

// deny renders a permission error. Phrased in terms of the missing permission
// so an agent can ask for the right grant instead of "access denied".
func (m *Module) deny(w http.ResponseWriter, r *http.Request, what string) {
	w.WriteHeader(http.StatusForbidden)
	m.d.Render(w, r, "kordinate_error.html", map[string]any{
		"Title":   "Not permitted",
		"Message": what,
	})
}

// denyEdit is the common case: a mutation attempted by a viewer. The route
// guards already refuse these, so reaching it means a role changed mid-session.
func (m *Module) denyEdit(w http.ResponseWriter, r *http.Request) {
	m.deny(w, r, "You need maintainer access to make this change.")
}

// fail renders an error page, translating an upstream failure into something an
// agent can act on: a missing customer and a dead service need different
// responses from the person reading the screen.
func (m *Module) fail(w http.ResponseWriter, r *http.Request, err error) {
	var ue *upstream.Error
	var re *requestError
	title, msg, code := "Something went wrong", err.Error(), http.StatusInternalServerError

	switch {
	case errors.As(err, &re):
		title, msg, code = "Check that again", re.msg, re.code
	case errors.As(err, &ue):
		switch {
		case ue.NotFound():
			title, code = "Not found", http.StatusNotFound
			msg = "That record doesn't exist in " + ue.Service + "."
		case ue.Unavailable():
			title, code = "Service unavailable", http.StatusBadGateway
			msg = ue.Service + " is not responding. The data shown may be incomplete — try again shortly."
		default:
			title, code = "Upstream error", http.StatusBadGateway
			msg = ue.Service + ": " + ue.Message
		}
	}
	slog.ErrorContext(r.Context(), "kordinate request failed", "path", r.URL.Path, "error", err)
	w.WriteHeader(code)
	m.d.Render(w, r, "kordinate_error.html", map[string]any{"Title": title, "Message": msg})
}

// logAccess records that a principal touched a customer's data. Best-effort by
// design at the call sites: failing to write an access log must not block an
// agent mid-call, but it is logged loudly so a silent gap can't develop.
func (m *Module) logAccess(r *http.Request, mmGuid, action, target, reason string) {
	ctx := r.Context()
	if err := m.st.LogAccess(ctx, account(r), mmGuid, principal(r), action, target, reason); err != nil {
		slog.ErrorContext(ctx, "kordinate: access log write failed",
			"principal", principal(r), "mm_guid", mmGuid, "action", action, "error", err)
	}
}

func (m *Module) assetJS(name string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		b, err := staticFS.ReadFile(name)
		if err != nil {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "application/javascript; charset=utf-8")
		w.Header().Set("Cache-Control", "public, max-age=300")
		_, _ = w.Write(b)
	}
}

// ---------- small shared helpers ----------

// audit records a mutating action in kore's audit trail.
//
// kore's Entry is keyed on a numeric EntityID, which doesn't fit here: the
// identifiers in this domain are customer GUIDs, document media ids and voucher
// codes. They go in Detail, and Entity carries the kordinate action name — so
// the trail stays searchable by what happened and to whom even though the id
// isn't numeric.
func (m *Module) audit(ctx context.Context, action, target, actor string) {
	if m.d.Audit == nil {
		return
	}
	m.d.Audit.Record(ctx, audit.Entry{
		Action:   action,
		Entity:   "kordinate",
		Detail:   target,
		UserName: actor,
	})
}

// relTime renders a timestamp as an age ("3 days ago"). Agents scan a timeline
// for recency far more than for exact clock times.
func relTime(t time.Time) string {
	if t.IsZero() {
		return ""
	}
	d := time.Since(t)
	switch {
	case d < time.Minute:
		return "just now"
	case d < time.Hour:
		return fmt.Sprintf("%dm ago", int(d.Minutes()))
	case d < 24*time.Hour:
		return fmt.Sprintf("%dh ago", int(d.Hours()))
	case d < 30*24*time.Hour:
		return fmt.Sprintf("%dd ago", int(d.Hours()/24))
	default:
		return t.Format("2 Jan 2006")
	}
}

// dateRange parses from/to query params, defaulting to the last 90 days —
// long enough to cover a support conversation about "last month", short enough
// not to pull a customer's entire history on every page view.
func dateRange(r *http.Request) (time.Time, time.Time) {
	to := time.Now()
	from := to.AddDate(0, 0, -90)
	if v := r.URL.Query().Get("from"); v != "" {
		if t, err := time.Parse("2006-01-02", v); err == nil {
			from = t
		}
	}
	if v := r.URL.Query().Get("to"); v != "" {
		if t, err := time.Parse("2006-01-02", v); err == nil {
			to = t.Add(24 * time.Hour)
		}
	}
	return from, to
}

func atoi64(s string) int64 {
	n, _ := strconv.ParseInt(strings.TrimSpace(s), 10, 64)
	return n
}
