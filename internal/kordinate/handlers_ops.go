package kordinate

import (
	"net/http"
	"strconv"
	"strings"

	"github.com/krogio/kordinate/internal/kordinate/upstream"
)

// handlers_ops.go covers the operational screens carried over from
// claire-admin: deposits and EFT reconciliation, vouchers, the device blocker,
// and the access log.

// ---------- Deposits and EFT ----------

func (m *Module) depositQueue(w http.ResponseWriter, r *http.Request) {

	from, to := dateRange(r)
	data := map[string]any{
		"CanAssign":      canEdit(r),
		"CanRefund":      canEdit(r),
		"CanMarkSuccess": canEdit(r),
		"Deposits":       []upstream.EFTNotification{},
		"Pending":        []upstream.EFTNotification{},
	}
	data["Filter"] = map[string]string{
		"Reference": strings.TrimSpace(r.URL.Query().Get("reference")),
		"Bank":      strings.TrimSpace(r.URL.Query().Get("bank")),
		"From":      from.Format("2006-01-02"),
		"To":        to.Format("2006-01-02"),
	}

	// Two views on one screen: deposits needing manual handling, and deposits
	// that never matched a customer. In claire-admin these were separate pages,
	// but they're the same job — an agent reconciling money that hasn't landed.
	pending, err := m.up.Emma.PendingManualNotifications(r.Context())
	if err != nil {
		data["PendingError"] = err.Error()
	} else {
		data["Pending"] = pending
		data["Deposits"] = pending
	}

	if ref := strings.TrimSpace(r.URL.Query().Get("reference")); ref != "" || r.URL.Query().Get("unmatched") == "1" {
		q := upstream.UnmatchedQuery{
			Reference: ref,
			Bank:      strings.TrimSpace(r.URL.Query().Get("bank")),
			From:      from,
			To:        to,
		}
		if v, err := strconv.ParseFloat(r.URL.Query().Get("amount_min"), 64); err == nil {
			q.AmountMin = v
		}
		if v, err := strconv.ParseFloat(r.URL.Query().Get("amount_max"), 64); err == nil {
			q.AmountMax = v
		}
		unmatched, err := m.up.Emma.SearchUnmatched(r.Context(), q)
		if err != nil {
			data["UnmatchedError"] = err.Error()
		} else {
			data["Unmatched"], data["Searched"] = unmatched, true
			data["Deposits"] = unmatched
		}
	}
	m.d.Render(w, r, "kordinate_deposits.html", data)
}

func (m *Module) depositAssign(w http.ResponseWriter, r *http.Request) {
	if !canEdit(r) {
		m.denyEdit(w, r)
		return
	}
	id := atoi64(r.FormValue("notification_id"))
	guid := strings.TrimSpace(r.FormValue("guid"))
	if id == 0 || guid == "" {
		m.fail(w, r, errBadRequest("A deposit and a customer are required."))
		return
	}
	if err := m.up.Emma.AssignDeposit(r.Context(), id, guid, principal(r)); err != nil {
		m.fail(w, r, err)
		return
	}
	m.audit(r.Context(), "kordinate.deposit.assign", guid, principal(r))
	http.Redirect(w, r, "/kordinate/deposits", http.StatusSeeOther)
}

func (m *Module) depositRefund(w http.ResponseWriter, r *http.Request) {
	if !canEdit(r) {
		m.denyEdit(w, r)
		return
	}
	id := atoi64(r.FormValue("notification_id"))
	reason := strings.TrimSpace(r.FormValue("reason"))
	// A refund moves real money out; it does not happen without a recorded why.
	if id == 0 || reason == "" {
		m.fail(w, r, errBadRequest("A refund needs a deposit and a reason."))
		return
	}
	if err := m.up.Emma.RefundDeposit(r.Context(), id, reason, principal(r)); err != nil {
		m.fail(w, r, err)
		return
	}
	m.audit(r.Context(), "kordinate.deposit.refund", strconv.FormatInt(id, 10), principal(r))
	http.Redirect(w, r, "/kordinate/deposits", http.StatusSeeOther)
}

func (m *Module) depositSuccess(w http.ResponseWriter, r *http.Request) {
	if !canEdit(r) {
		m.denyEdit(w, r)
		return
	}
	id := atoi64(r.FormValue("notification_id"))
	if id == 0 {
		m.fail(w, r, errBadRequest("A deposit is required."))
		return
	}
	if err := m.up.Emma.MarkSuccess(r.Context(), id, principal(r)); err != nil {
		m.fail(w, r, err)
		return
	}
	m.audit(r.Context(), "kordinate.deposit.success", strconv.FormatInt(id, 10), principal(r))
	http.Redirect(w, r, "/kordinate/deposits", http.StatusSeeOther)
}

// ---------- Vouchers ----------

func (m *Module) voucherList(w http.ResponseWriter, r *http.Request) {
	data := map[string]any{
		"CanManage": canEdit(r),
		// Always present: len/range on a missing key errors mid-render, which
		// aborts the layout and takes the kernel drawer with it.
		"Vouchers": []upstream.Voucher{},
	}
	data["Filter"] = map[string]string{
		"Code": strings.TrimSpace(r.URL.Query().Get("code")),
		"GUID": strings.TrimSpace(r.URL.Query().Get("guid")),
	}

	// Vouchers are looked up either by code (a customer on the phone reading it
	// out) or by customer — both are one-field searches, so support both.
	if code := strings.TrimSpace(r.URL.Query().Get("code")); code != "" {
		v, err := m.up.VMS.Voucher(r.Context(), code)
		if err != nil {
			data["Error"] = err.Error()
		} else {
			data["Vouchers"] = []upstream.Voucher{*v}
		}
		data["Code"], data["Searched"] = code, true
	} else if guid := strings.TrimSpace(r.URL.Query().Get("guid")); guid != "" {
		vs, err := m.up.VMS.VouchersForCustomer(r.Context(), guid)
		if err != nil {
			data["Error"] = err.Error()
		} else {
			data["Vouchers"] = vs
		}
		data["GUID"], data["Searched"] = guid, true
	}
	m.d.Render(w, r, "kordinate_vouchers.html", data)
}

func (m *Module) voucherCancel(w http.ResponseWriter, r *http.Request) {
	if !canEdit(r) {
		m.denyEdit(w, r)
		return
	}
	code := strings.TrimSpace(r.FormValue("code"))
	reason := strings.TrimSpace(r.FormValue("reason"))
	if code == "" || reason == "" {
		m.fail(w, r, errBadRequest("Cancelling a voucher needs its code and a reason."))
		return
	}
	if err := m.up.VMS.Cancel(r.Context(), code, reason, principal(r)); err != nil {
		m.fail(w, r, err)
		return
	}
	m.audit(r.Context(), "kordinate.voucher.cancel", code, principal(r))
	http.Redirect(w, r, "/kordinate/vouchers?code="+code, http.StatusSeeOther)
}

func (m *Module) voucherCreate(w http.ResponseWriter, r *http.Request) {
	if !canEdit(r) {
		m.denyEdit(w, r)
		return
	}
	amount, err := strconv.ParseFloat(strings.TrimSpace(r.FormValue("amount")), 64)
	if err != nil || amount <= 0 {
		m.fail(w, r, errBadRequest("A voucher needs a positive amount."))
		return
	}
	qty, err := strconv.Atoi(strings.TrimSpace(r.FormValue("quantity")))
	if err != nil || qty < 1 {
		qty = 1
	}

	vs, err := m.up.VMS.Create(r.Context(), upstream.VoucherCreate{
		Amount:   amount,
		Currency: orDefault(r.FormValue("currency"), "ZAR"),
		Quantity: qty,
		Product:  strings.TrimSpace(r.FormValue("product")),
		AgentID:  principal(r),
	})
	if err != nil {
		m.fail(w, r, err)
		return
	}
	m.audit(r.Context(), "kordinate.voucher.create", strconv.Itoa(len(vs))+" vouchers", principal(r))
	data := map[string]any{"CanManage": canEdit(r)}
	data["Vouchers"], data["Created"] = vs, true
	m.d.Render(w, r, "kordinate_vouchers.html", data)
}

// ---------- Device blocker ----------

func (m *Module) deviceLookup(w http.ResponseWriter, r *http.Request) {
	if !canManage(r) {
		m.deny(w, r, "This action needs admin access.")
		return
	}
	data := map[string]any{
		"CanBlock": canEdit(r), "CanBulkSuspend": canManage(r),
		"Linked":  []upstream.Customer{},
		"Devices": []upstream.Device{},
	}
	data["Filter"] = map[string]string{
		"DeviceID": strings.TrimSpace(r.URL.Query().Get("device_id")),
	}

	if id := strings.TrimSpace(r.URL.Query().Get("device_id")); id != "" {
		dev, err := m.up.Device.Device(r.Context(), id)
		if err != nil {
			data["Error"] = err.Error()
		} else {
			data["Device"] = dev
			// Resolve the linked customers to names: an agent about to suspend
			// a group needs to see who they are, not a list of GUIDs.
			data["Linked"] = m.resolveCustomers(r, dev.LinkedCustomers)
		}
		data["DeviceID"], data["Searched"] = id, true
	} else if guid := strings.TrimSpace(r.URL.Query().Get("guid")); guid != "" {
		devs, err := m.up.Device.DevicesForCustomer(r.Context(), guid)
		if err != nil {
			data["Error"] = err.Error()
		} else {
			data["Devices"] = devs
		}
		data["GUID"], data["Searched"] = guid, true
	}
	m.d.Render(w, r, "kordinate_devices.html", data)
}

// resolveCustomers turns GUIDs into displayable customers, skipping any that
// can't be resolved rather than failing the whole screen.
func (m *Module) resolveCustomers(r *http.Request, guids []string) []upstream.Customer {
	out := make([]upstream.Customer, 0, len(guids))
	for _, g := range guids {
		if cust, err := m.up.Customer.GetByGUID(r.Context(), g); err == nil && cust != nil {
			out = append(out, *cust)
		}
	}
	return out
}

func (m *Module) deviceSetStatus(w http.ResponseWriter, r *http.Request) {
	if !canManage(r) {
		m.deny(w, r, "This action needs admin access.")
		return
	}
	id := strings.TrimSpace(r.FormValue("device_id"))
	status := upstream.DeviceStatus(strings.ToUpper(strings.TrimSpace(r.FormValue("status"))))
	reason := strings.TrimSpace(r.FormValue("reason"))

	if id == "" || (status != upstream.DeviceActive && status != upstream.DeviceBlocked) {
		m.fail(w, r, errBadRequest("A device and a valid status are required."))
		return
	}
	if status == upstream.DeviceBlocked && reason == "" {
		m.fail(w, r, errBadRequest("Blocking a device needs a reason."))
		return
	}
	if err := m.up.Device.SetStatus(r.Context(), id, status, reason, principal(r)); err != nil {
		m.fail(w, r, err)
		return
	}
	m.audit(r.Context(), "kordinate.device."+strings.ToLower(string(status)), id, principal(r))
	http.Redirect(w, r, "/kordinate/devices?device_id="+id, http.StatusSeeOther)
}

// deviceBulkSuspend suspends every customer linked to a device.
//
// This is the highest blast-radius action in the product — one submit can lock
// dozens of customers out of their money. It therefore requires the separate
// CapBulkSuspend, a reason, and an explicit confirmation of the exact count the
// agent believes they are affecting: if the linked set has changed since the
// page was rendered, the request is refused rather than applied to a larger
// group than the agent saw.
func (m *Module) deviceBulkSuspend(w http.ResponseWriter, r *http.Request) {
	if !canManage(r) {
		m.deny(w, r, "This action needs admin access.")
		return
	}
	id := strings.TrimSpace(r.FormValue("device_id"))
	reason := strings.TrimSpace(r.FormValue("reason"))
	confirmed, cerr := strconv.Atoi(strings.TrimSpace(r.FormValue("confirm_count")))

	if id == "" || reason == "" {
		m.fail(w, r, errBadRequest("A bulk suspend needs a device and a reason."))
		return
	}
	if cerr != nil {
		m.fail(w, r, errBadRequest("Confirm how many customers you expect to suspend."))
		return
	}

	dev, err := m.up.Device.Device(r.Context(), id)
	if err != nil {
		m.fail(w, r, err)
		return
	}
	if len(dev.LinkedCustomers) != confirmed {
		m.fail(w, r, errBadRequest(
			"The number of linked customers has changed since this page loaded ("+
				strconv.Itoa(len(dev.LinkedCustomers))+" now, you confirmed "+
				strconv.Itoa(confirmed)+"). Reload and check before suspending."))
		return
	}
	if len(dev.LinkedCustomers) == 0 {
		m.fail(w, r, errBadRequest("No customers are linked to that device."))
		return
	}

	failures, err := m.up.Customer.BulkSuspend(r.Context(), dev.LinkedCustomers, reason)
	if err != nil {
		m.fail(w, r, err)
		return
	}
	if err := m.up.Device.PatchAndUpdateLinked(r.Context(), id, upstream.DeviceBlocked,
		dev.LinkedCustomers, reason, principal(r)); err != nil {
		m.fail(w, r, err)
		return
	}
	m.audit(r.Context(), "kordinate.device.bulk_suspend",
		id+" ("+strconv.Itoa(len(dev.LinkedCustomers))+" customers)", principal(r))

	data := map[string]any{"CanBlock": canEdit(r), "CanBulkSuspend": canManage(r)}
	data["Device"] = dev
	data["Linked"] = m.resolveCustomers(r, dev.LinkedCustomers)
	data["Suspended"] = len(dev.LinkedCustomers) - len(failures)
	data["Failures"] = failures
	data["Searched"] = true
	data["DeviceID"] = id
	data["Filter"] = map[string]string{"DeviceID": id}
	m.d.Render(w, r, "kordinate_devices.html", data)
}

// ---------- Access log ----------

func (m *Module) accessLogView(w http.ResponseWriter, r *http.Request) {
	if !canManage(r) {
		m.deny(w, r, "This action needs admin access.")
		return
	}
	guid := strings.TrimSpace(r.URL.Query().Get("guid"))
	from, to := dateRange(r)
	grants, err := m.st.RevealGrants(r.Context(), account(r))
	if err != nil {
		m.fail(w, r, err)
		return
	}
	data := map[string]any{
		"RevealGrants": grants,
		"Now":          m.now(),
	}
	data["GUID"] = guid
	data["Filter"] = map[string]string{
		"GUID":      guid,
		"Principal": strings.TrimSpace(r.URL.Query().Get("principal")),
		"Action":    strings.TrimSpace(r.URL.Query().Get("action")),
		"From":      from.Format("2006-01-02"),
		"To":        to.Format("2006-01-02"),
	}

	if guid != "" {
		entries, err := m.st.AccessLog(r.Context(), account(r), guid, 200)
		if err != nil {
			m.fail(w, r, err)
			return
		}
		// Surface the unredacted-reveal count separately: it is the number a
		// compliance reviewer is actually looking for on this screen.
		reveals := 0
		for _, e := range entries {
			if e.Action == AccessRevealUnredacted {
				reveals++
			}
		}
		data["Entries"], data["Searched"], data["Reveals"] = entries, true, reveals
	}
	m.d.Render(w, r, "kordinate_access_log.html", data)
}

// grantReveal grants a named user permission to view unredacted identity
// documents. Admin-only, and the reason is recorded: this is the permission an
// auditor will ask about, so "who granted it, when, and why" must be answerable
// without reading a deploy diff.
func (m *Module) grantReveal(w http.ResponseWriter, r *http.Request) {
	if !canManage(r) {
		m.deny(w, r, "Granting unredacted-document access needs admin access.")
		return
	}
	email := strings.ToLower(strings.TrimSpace(r.FormValue("email")))
	reason := strings.TrimSpace(r.FormValue("reason"))
	if email == "" || reason == "" {
		m.fail(w, r, errBadRequest("Granting this permission needs a user and a reason."))
		return
	}

	g := RevealGrant{Email: email, GrantedBy: principal(r), Reason: reason}
	// Default to a bounded grant. An investigation ends; a standing permission
	// to read every customer's ID does not, which is why it must be opted into
	// explicitly rather than being the default.
	days := 30
	if v, err := strconv.Atoi(strings.TrimSpace(r.FormValue("days"))); err == nil && v > 0 {
		days = v
	}
	if r.FormValue("no_expiry") != "1" {
		exp := m.now().UTC().AddDate(0, 0, days)
		g.ExpiresAt = &exp
	}

	if err := m.st.GrantReveal(r.Context(), account(r), g); err != nil {
		m.fail(w, r, err)
		return
	}
	m.audit(r.Context(), "kordinate.reveal.grant", email, principal(r))
	http.Redirect(w, r, "/kordinate/access-log", http.StatusSeeOther)
}

func (m *Module) revokeReveal(w http.ResponseWriter, r *http.Request) {
	if !canManage(r) {
		m.deny(w, r, "Revoking unredacted-document access needs admin access.")
		return
	}
	email := strings.ToLower(strings.TrimSpace(r.FormValue("email")))
	if email == "" {
		m.fail(w, r, errBadRequest("A user is required."))
		return
	}
	if err := m.st.RevokeReveal(r.Context(), account(r), email); err != nil {
		m.fail(w, r, err)
		return
	}
	m.audit(r.Context(), "kordinate.reveal.revoke", email, principal(r))
	http.Redirect(w, r, "/kordinate/access-log", http.StatusSeeOther)
}
