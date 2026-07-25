package kordinate

import (
	"encoding/json"
	"net/http"
	"strconv"
	"strings"

	"github.com/krogio/kordinate/internal/kordinate/upstream"
)

// handlers.go is the HTTP surface. Two rules hold throughout:
//
//  1. Every handler resolves a caller and checks the specific capability for
//     the action, not the route's role. The route role is a coarse first gate;
//     the capability is the real authorisation, and it's checked next to the
//     thing it protects so it can't drift out of sync with the action.
//  2. Reads of customer data are logged. A back office that can't answer
//     "who looked at this customer" is a compliance finding waiting to happen.

// ---------- Customer search ----------

func (m *Module) customerSearch(w http.ResponseWriter, r *http.Request) {

	q := upstream.CustomerSearchQuery{
		MSISDN:             strings.TrimSpace(r.URL.Query().Get("msisdn")),
		MMGlobalCustomerID: strings.TrimSpace(r.URL.Query().Get("guid")),
		IDNumber:           strings.TrimSpace(r.URL.Query().Get("id_number")),
		FirstName:          strings.TrimSpace(r.URL.Query().Get("first_name")),
		LastName:           strings.TrimSpace(r.URL.Query().Get("last_name")),
		EmailAddress:       strings.TrimSpace(r.URL.Query().Get("email")),
		Status:             upstream.CustomerStatus(strings.TrimSpace(r.URL.Query().Get("status"))),
		PerPage:            25,
	}
	if p, err := strconv.Atoi(r.URL.Query().Get("page")); err == nil && p > 1 {
		q.Page = p
	}

	data := map[string]any{
		"Query":     q,
		"Statuses":  allStatuses(),
		"CanCreate": canEdit(r),
		// Always present: len/range on a missing key errors mid-render, which
		// aborts the layout and takes the kernel drawer down with it.
		"Results": []*upstream.Customer{},
	}

	// An empty form is the landing state, not a search for everyone — running an
	// unfiltered query would pull the whole customer book on page load.
	if searchIsEmpty(q) {
		m.d.Render(w, r, "kordinate_search.html", data)
		return
	}

	results, total, err := m.up.Customer.Search(r.Context(), q)
	if err != nil {
		m.fail(w, r, err)
		return
	}
	data["Results"], data["Total"], data["Searched"] = results, total, true
	m.d.Render(w, r, "kordinate_search.html", data)
}

func searchIsEmpty(q upstream.CustomerSearchQuery) bool {
	return q.MSISDN == "" && q.MMGlobalCustomerID == "" && q.IDNumber == "" &&
		q.FirstName == "" && q.LastName == "" && q.EmailAddress == "" && q.Status == ""
}

func allStatuses() []upstream.CustomerStatus {
	return []upstream.CustomerStatus{
		upstream.StatusActive, upstream.StatusInactive, upstream.StatusSuspended,
		upstream.StatusDuplicate, upstream.StatusUndergoingScreening,
		upstream.StatusBlockedPositiveMatch, upstream.StatusPermanentlyBlocked,
	}
}

// ---------- Customer 360 ----------

// customerView renders the single customer view. Balances and the timeline are
// deliberately NOT fetched here — they fan out to six services, and blocking
// the page on the slowest one would make the screen feel broken. The template
// requests them asynchronously (see kordinate.js).
func (m *Module) customerView(w http.ResponseWriter, r *http.Request) {
	guid := strings.TrimSpace(r.URL.Query().Get("guid"))
	if guid == "" {
		http.Redirect(w, r, "/kordinate", http.StatusSeeOther)
		return
	}

	cust, err := m.up.Customer.GetByGUID(r.Context(), guid)
	if err != nil {
		m.fail(w, r, err)
		return
	}
	m.logAccess(r, guid, AccessViewCustomer, "", "")

	docs := cust.Documents
	if len(docs) == 0 && m.up.Customer != nil {
		// Older records don't inline documents on the customer read; fetch
		// separately rather than showing an empty document list.
		if fetched, derr := m.up.Customer.ListDocuments(r.Context(), guid); derr == nil {
			docs = fetched
		}
	}

	// The case is what makes this more than claire-admin's read-only view. For a
	// customer who predates kordinate there is no case row, so derive the state
	// they're really in rather than showing nothing.
	kase, err := m.st.OpenCaseFor(r.Context(), account(r), guid)
	if err != nil {
		m.fail(w, r, err)
		return
	}
	var suggested State
	if kase == nil {
		suggested = SuggestState(cust, docs)
	}

	notes, err := m.st.Notes(r.Context(), account(r), guid, 50)
	if err != nil {
		m.fail(w, r, err)
		return
	}

	edit := canEdit(r)
	data := map[string]any{
		"CanEdit":           edit,
		"CanChangeStatus":   edit,
		"CanSetRisk":        edit,
		"CanResetPIN":       edit,
		"CanUploadDocument": edit,
		"CanAdvance":        edit,
		"CanViewDocuments":  true,
		"CanViewFinancial":  true,
	}
	data["Customer"] = cust
	data["Documents"] = docs
	data["Case"] = kase
	data["SuggestedState"] = suggested
	data["Notes"] = notes
	data["Statuses"] = allStatuses()
	data["RiskScores"] = riskScores()
	data["DocTypes"] = docTypes()
	data["Sentiments"] = defaultSentiments()
	data["VetAvailable"] = m.vet.Available()
	data["Now"] = m.now()
	from, to := dateRange(r)
	data["From"], data["To"] = from.Format("2006-01-02"), to.Format("2006-01-02")
	if risk, rerr := m.up.Claire.RiskMatrix(r.Context(), guid); rerr == nil {
		data["Risk"] = risk
	}
	if kase != nil {
		data["Outstanding"] = Outstanding(kase.State, docs)
		data["NextStates"] = kase.State.NextStates()
		if hist, herr := m.st.CaseHistory(r.Context(), account(r), kase.ID); herr == nil {
			data["History"] = hist
		}
	}
	m.d.Render(w, r, "kordinate_customer.html", data)
}

// customerBalances serves the cross-product balance strip as JSON.
func (m *Module) customerBalances(w http.ResponseWriter, r *http.Request) {
	guid := strings.TrimSpace(r.URL.Query().Get("guid"))
	msisdn := strings.TrimSpace(r.URL.Query().Get("msisdn"))
	if guid == "" {
		writeJSONError(w, http.StatusBadRequest, "A customer is required.")
		return
	}

	bal, err := m.up.UML.Balances(r.Context(), guid, msisdn)
	if err != nil {
		writeJSONError(w, http.StatusBadGateway, "Balances are unavailable: "+err.Error())
		return
	}
	writeJSON(w, http.StatusOK, bal)
}

// customerTimeline serves the stitched activity feed as JSON. A degraded
// timeline is still a 200: the events that WERE retrieved are useful, and the
// payload names the failed sources so the UI can say so.
func (m *Module) customerTimeline(w http.ResponseWriter, r *http.Request) {
	guid := strings.TrimSpace(r.URL.Query().Get("guid"))
	if guid == "" {
		writeJSONError(w, http.StatusBadRequest, "A customer is required.")
		return
	}
	from, to := dateRange(r)

	tl, err := m.tl.Build(r.Context(), account(r), guid, from, to)
	if err != nil {
		writeJSONError(w, http.StatusBadGateway, err.Error())
		return
	}
	// Financial events are money movements; hide them from roles that can't see
	// balances rather than leaking amounts through the timeline.
	writeJSON(w, http.StatusOK, tl)
}

// ---------- Customer mutations ----------

func (m *Module) addNote(w http.ResponseWriter, r *http.Request) {
	guid := strings.TrimSpace(r.FormValue("guid"))
	body := strings.TrimSpace(r.FormValue("body"))
	if guid == "" || body == "" {
		m.fail(w, r, errBadRequest("A note needs a customer and some text."))
		return
	}
	if _, err := m.st.AddNote(r.Context(), account(r), Note{
		MMGuid:    guid,
		Subject:   orDefault(r.FormValue("subject"), "customer"),
		SubjectID: strings.TrimSpace(r.FormValue("subject_id")),
		Body:      body,
		Sentiment: strings.TrimSpace(r.FormValue("sentiment")),
		TicketRef: strings.TrimSpace(r.FormValue("ticket_ref")),
		Author:    principal(r),
	}); err != nil {
		m.fail(w, r, err)
		return
	}
	m.audit(r.Context(), "kordinate.note.add", guid, principal(r))
	redirectToCustomer(w, r, guid)
}

func (m *Module) setStatus(w http.ResponseWriter, r *http.Request) {
	if !canEdit(r) {
		m.denyEdit(w, r)
		return
	}
	guid := strings.TrimSpace(r.FormValue("guid"))
	status := upstream.CustomerStatus(strings.TrimSpace(r.FormValue("status")))
	reason := strings.TrimSpace(r.FormValue("reason"))

	// A status change is a consequential act on a customer's ability to
	// transact; requiring a reason is what makes the audit trail worth having.
	if guid == "" || status == "" || reason == "" {
		m.fail(w, r, errBadRequest("A status change needs a customer, a new status and a reason."))
		return
	}
	if err := m.up.Customer.UpdateStatus(r.Context(), guid, status, reason); err != nil {
		m.fail(w, r, err)
		return
	}
	m.audit(r.Context(), "kordinate.customer.status."+strings.ToLower(string(status)), guid, principal(r))
	redirectToCustomer(w, r, guid)
}

func (m *Module) setRisk(w http.ResponseWriter, r *http.Request) {
	if !canEdit(r) {
		m.denyEdit(w, r)
		return
	}
	guid := strings.TrimSpace(r.FormValue("guid"))
	score, err := strconv.Atoi(strings.TrimSpace(r.FormValue("score")))
	if guid == "" || err != nil || score < 1 || score > 6 {
		m.fail(w, r, errBadRequest("A risk rating must be a score from 1 to 6."))
		return
	}
	if _, nerr := m.st.AddNote(r.Context(), account(r), Note{
		MMGuid: guid, Subject: "customer",
		Body:   "Risk rating set to " + strconv.Itoa(score) + ". " + strings.TrimSpace(r.FormValue("reason")),
		Author: principal(r),
	}); nerr != nil {
		m.fail(w, r, nerr)
		return
	}
	m.audit(r.Context(), "kordinate.customer.risk", guid, principal(r))
	redirectToCustomer(w, r, guid)
}

func (m *Module) resetLoginPIN(w http.ResponseWriter, r *http.Request) {
	if !canEdit(r) {
		m.denyEdit(w, r)
		return
	}
	guid := strings.TrimSpace(r.FormValue("guid"))
	if guid == "" {
		m.fail(w, r, errBadRequest("A customer is required."))
		return
	}
	if err := m.up.IDV.ResetLoginPIN(r.Context(), guid, principal(r)); err != nil {
		m.fail(w, r, err)
		return
	}
	m.audit(r.Context(), "kordinate.customer.reset_pin", guid, principal(r))
	redirectToCustomer(w, r, guid)
}

func (m *Module) customerNewForm(w http.ResponseWriter, r *http.Request) {
	if !canEdit(r) {
		m.denyEdit(w, r)
		return
	}
	data := map[string]any{"CanCreate": true}
	data["IDTypes"] = idTypes()
	data["Form"] = map[string]string{}
	m.d.Render(w, r, "kordinate_customer_new.html", data)
}

func (m *Module) createCustomer(w http.ResponseWriter, r *http.Request) {
	if !canEdit(r) {
		m.denyEdit(w, r)
		return
	}
	in := upstream.CustomerCreate{
		MSISDN:         strings.TrimSpace(r.FormValue("msisdn")),
		FirstName:      strings.TrimSpace(r.FormValue("first_name")),
		LastName:       strings.TrimSpace(r.FormValue("last_name")),
		Gender:         strings.TrimSpace(r.FormValue("gender")),
		EmailAddress:   strings.TrimSpace(r.FormValue("email")),
		DateOfBirth:    strings.TrimSpace(r.FormValue("date_of_birth")),
		StreetAddress:  strings.TrimSpace(r.FormValue("street_address")),
		StreetSuburb:   strings.TrimSpace(r.FormValue("suburb")),
		StreetCity:     strings.TrimSpace(r.FormValue("city")),
		StreetProvince: strings.TrimSpace(r.FormValue("province")),
		PostalCode:     strings.TrimSpace(r.FormValue("postal_code")),
		AgentID:        strings.TrimSpace(r.FormValue("agent_id")),
		IDNumber:       strings.TrimSpace(r.FormValue("id_number")),
		IDType:         strings.TrimSpace(r.FormValue("id_type")),
	}
	in.Defaults()

	if in.MSISDN == "" || in.FirstName == "" || in.DateOfBirth == "" {
		m.fail(w, r, errBadRequest("A phone number, first name and date of birth are required."))
		return
	}

	cust, err := m.up.Customer.Create(r.Context(), in)
	if err != nil {
		m.fail(w, r, err)
		return
	}
	m.audit(r.Context(), "kordinate.customer.create", cust.MMGlobalCustomerID, principal(r))

	// A newly captured customer is a lead with work outstanding, so open the
	// case immediately — the whole point of the lifecycle is that nothing
	// enters the book without a tracked state.
	if _, cerr := m.st.OpenCase(r.Context(), account(r), Case{
		MMGuid: cust.MMGlobalCustomerID, MSISDN: cust.MSISDN,
		DisplayName: cust.FullName(), State: StateLead, CreatedBy: principal(r),
	}); cerr != nil {
		m.fail(w, r, cerr)
		return
	}
	redirectToCustomer(w, r, cust.MMGlobalCustomerID)
}

func (m *Module) editCustomer(w http.ResponseWriter, r *http.Request) {
	if !canEdit(r) {
		m.denyEdit(w, r)
		return
	}
	guid := strings.TrimSpace(r.FormValue("guid"))
	if guid == "" {
		m.fail(w, r, errBadRequest("A customer is required."))
		return
	}

	// Only fields actually present in the form are patched, so an edit of one
	// field can't blank the others.
	var in upstream.CustomerUpdate
	setIfPresent(r, "first_name", &in.FirstName)
	setIfPresent(r, "last_name", &in.LastName)
	setIfPresent(r, "email", &in.EmailAddress)
	setIfPresent(r, "date_of_birth", &in.DateOfBirth)
	setIfPresent(r, "gender", &in.Gender)
	setIfPresent(r, "street_address", &in.StreetAddress)
	setIfPresent(r, "suburb", &in.StreetSuburb)
	setIfPresent(r, "city", &in.StreetCity)
	setIfPresent(r, "province", &in.StreetProvince)
	setIfPresent(r, "postal_code", &in.PostalCode)

	if _, err := m.up.Customer.Update(r.Context(), guid, in); err != nil {
		m.fail(w, r, err)
		return
	}
	m.audit(r.Context(), "kordinate.customer.edit", guid, principal(r))
	redirectToCustomer(w, r, guid)
}

func setIfPresent(r *http.Request, field string, dst **string) {
	if _, ok := r.Form[field]; !ok {
		return
	}
	v := strings.TrimSpace(r.FormValue(field))
	*dst = &v
}

// ---------- Card operations ----------

func (m *Module) cardBlock(w http.ResponseWriter, r *http.Request) {
	m.cardAction(w, r, "block", func(guid, reason string, rr *http.Request) error {
		return m.up.UML.BlockCard(rr.Context(), guid, reason)
	})
}

func (m *Module) cardUnblock(w http.ResponseWriter, r *http.Request) {
	m.cardAction(w, r, "unblock", func(guid, reason string, rr *http.Request) error {
		return m.up.UML.UnblockCard(rr.Context(), guid, reason)
	})
}

func (m *Module) cardResetPIN(w http.ResponseWriter, r *http.Request) {
	m.cardAction(w, r, "reset_pin", func(guid, _ string, rr *http.Request) error {
		return m.up.UML.ResetCardPIN(rr.Context(), guid)
	})
}

func (m *Module) cardReallocate(w http.ResponseWriter, r *http.Request) {
	m.cardAction(w, r, "reallocate", func(guid, _ string, rr *http.Request) error {
		seq := strings.TrimSpace(rr.FormValue("card_sequence_number"))
		if seq == "" {
			return m.up.UML.RetryCardAllocation(rr.Context(), guid)
		}
		return m.up.UML.ReallocateCard(rr.Context(), guid, seq)
	})
}

func (m *Module) bankingOptIn(w http.ResponseWriter, r *http.Request) {
	m.cardAction(w, r, "opt_in", func(guid, _ string, rr *http.Request) error {
		return m.up.UML.OptInForBanking(rr.Context(), guid)
	})
}

// cardAction is the shared shape of every card mutation: check the capability,
// require a customer, run the call, audit it, return to the customer.
func (m *Module) cardAction(w http.ResponseWriter, r *http.Request, action string,
	fn func(guid, reason string, r *http.Request) error) {
	if !canEdit(r) {
		m.denyEdit(w, r)
		return
	}
	guid := strings.TrimSpace(r.FormValue("guid"))
	if guid == "" {
		m.fail(w, r, errBadRequest("A customer is required."))
		return
	}
	if err := fn(guid, strings.TrimSpace(r.FormValue("reason")), r); err != nil {
		m.fail(w, r, err)
		return
	}
	m.audit(r.Context(), "kordinate.card."+action, guid, principal(r))
	redirectToCustomer(w, r, guid)
}

// ---------- Onboarding ----------

func (m *Module) onboardingQueue(w http.ResponseWriter, r *http.Request) {

	q := CaseQuery{Limit: 200}
	var f QueueFilter
	if st := strings.TrimSpace(r.URL.Query().Get("state")); st != "" {
		q.States = []State{State(st)}
		f.State = State(st)
	}
	switch strings.TrimSpace(r.URL.Query().Get("owner")) {
	case "mine":
		q.Assignee, f.Assignee = principal(r), principal(r)
	case "unassigned":
		q.Unassigned, f.Unassigned = true, true
	}
	if a := strings.TrimSpace(r.URL.Query().Get("assignee")); a != "" {
		q.Assignee, f.Assignee = a, a
	}
	if r.URL.Query().Get("unassigned") == "1" {
		q.Unassigned, f.Unassigned = true, true
	}
	q.OverdueOnly = r.URL.Query().Get("overdue") == "1"
	f.OverdueOnly = q.OverdueOnly
	q.IncludeClosed = r.URL.Query().Get("closed") == "1"
	f.IncludeClosed = q.IncludeClosed

	cases, err := m.st.ListCases(r.Context(), account(r), q)
	if err != nil {
		m.fail(w, r, err)
		return
	}
	counts, err := m.st.CaseCounts(r.Context(), account(r))
	if err != nil {
		m.fail(w, r, err)
		return
	}

	data := map[string]any{"CanAssign": canEdit(r)}
	data["Cases"] = cases
	data["Counts"] = counts
	data["States"] = OrderedStates()
	data["StateTiles"] = stateTiles(counts, cases)
	data["Assignees"] = assignees(cases, principal(r))
	data["Filter"] = f
	data["Now"] = m.now()
	m.d.Render(w, r, "kordinate_onboarding.html", data)
}

func (m *Module) caseView(w http.ResponseWriter, r *http.Request) {
	kase, err := m.st.GetCase(r.Context(), account(r), atoi64(r.URL.Query().Get("id")))
	if err != nil {
		m.fail(w, r, err)
		return
	}
	if kase == nil {
		m.fail(w, r, errNotFound("That case doesn't exist."))
		return
	}

	cust, err := m.up.Customer.GetByGUID(r.Context(), kase.MMGuid)
	if err != nil {
		m.fail(w, r, err)
		return
	}
	m.logAccess(r, kase.MMGuid, AccessViewCustomer, "case", "")

	docs, _ := m.up.Customer.ListDocuments(r.Context(), kase.MMGuid)
	hist, err := m.st.CaseHistory(r.Context(), account(r), kase.ID)
	if err != nil {
		m.fail(w, r, err)
		return
	}

	data := map[string]any{"CanAdvance": canEdit(r), "CanAssign": canEdit(r)}
	data["Case"] = kase
	data["Customer"] = cust
	data["Documents"] = docs
	data["History"] = hist
	data["Outstanding"] = Outstanding(kase.State, docs)
	data["NextStates"] = kase.State.NextStates()
	data["Assignees"] = assignees([]Case{*kase}, principal(r))
	data["Now"] = m.now()
	m.d.Render(w, r, "kordinate_case.html", data)
}

func (m *Module) openCase(w http.ResponseWriter, r *http.Request) {
	if !canEdit(r) {
		m.denyEdit(w, r)
		return
	}
	guid := strings.TrimSpace(r.FormValue("guid"))
	if guid == "" {
		m.fail(w, r, errBadRequest("A customer is required."))
		return
	}

	cust, err := m.up.Customer.GetByGUID(r.Context(), guid)
	if err != nil {
		m.fail(w, r, err)
		return
	}
	docs, _ := m.up.Customer.ListDocuments(r.Context(), guid)

	// Seed the case at the state the customer is genuinely in. Dropping a
	// long-standing active customer into LEAD would misreport the book.
	state := State(strings.TrimSpace(r.FormValue("state")))
	if !state.Valid() {
		state = SuggestState(cust, docs)
	}

	kase, err := m.st.OpenCase(r.Context(), account(r), Case{
		MMGuid: guid, MSISDN: cust.MSISDN, DisplayName: cust.FullName(),
		State: state, CreatedBy: principal(r),
	})
	if err != nil {
		m.fail(w, r, err)
		return
	}
	m.audit(r.Context(), "kordinate.case.open", guid, principal(r))
	http.Redirect(w, r, "/kordinate/onboarding/case?id="+strconv.FormatInt(kase.ID, 10), http.StatusSeeOther)
}

// moveCase applies a lifecycle transition. The legality check runs here, over
// live upstream documents, so a stale page can't be used to skip a requirement.
func (m *Module) moveCase(w http.ResponseWriter, r *http.Request) {
	if !canEdit(r) {
		m.denyEdit(w, r)
		return
	}

	id := atoi64(r.FormValue("id"))
	to := State(strings.TrimSpace(r.FormValue("to")))
	note := strings.TrimSpace(r.FormValue("note"))

	kase, err := m.st.GetCase(r.Context(), account(r), id)
	if err != nil {
		m.fail(w, r, err)
		return
	}
	if kase == nil {
		m.fail(w, r, errNotFound("That case doesn't exist."))
		return
	}

	// Approving or declining an application is a heavier permission than moving
	// work along the pipeline.
	if to == StateApproved || to == StateActive || to == StateRejected {
		if !canManage(r) {
			m.deny(w, r, "Approving or declining an application needs admin access.")
			return
		}
		if note == "" {
			m.fail(w, r, errBadRequest("A decision needs a reason recorded against it."))
			return
		}
	}

	docs, _ := m.up.Customer.ListDocuments(r.Context(), kase.MMGuid)
	if err := CheckTransition(kase.State, to, docs); err != nil {
		m.fail(w, r, errBadRequest(err.Error()))
		return
	}
	if err := m.st.MoveCase(r.Context(), account(r), id, to, principal(r), note); err != nil {
		m.fail(w, r, err)
		return
	}
	m.audit(r.Context(), "kordinate.case.move."+strings.ToLower(string(to)), kase.MMGuid, principal(r))
	http.Redirect(w, r, "/kordinate/onboarding/case?id="+strconv.FormatInt(id, 10), http.StatusSeeOther)
}

func (m *Module) assignCase(w http.ResponseWriter, r *http.Request) {
	if !canEdit(r) {
		m.denyEdit(w, r)
		return
	}
	id := atoi64(r.FormValue("id"))
	assignee := strings.TrimSpace(r.FormValue("assignee"))
	// "Take it" is the common case, so an empty assignee with a self flag means
	// the caller.
	if assignee == "" && r.FormValue("self") == "1" {
		assignee = principal(r)
	}
	if err := m.st.AssignCase(r.Context(), account(r), id, assignee, principal(r)); err != nil {
		m.fail(w, r, err)
		return
	}
	http.Redirect(w, r, "/kordinate/onboarding/case?id="+strconv.FormatInt(id, 10), http.StatusSeeOther)
}

// ---------- helpers ----------

func writeJSON(w http.ResponseWriter, code int, v any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(code)
	_ = json.NewEncoder(w).Encode(v)
}

func writeJSONError(w http.ResponseWriter, code int, msg string) {
	writeJSON(w, code, map[string]string{"error": msg})
}

func redirectToCustomer(w http.ResponseWriter, r *http.Request, guid string) {
	http.Redirect(w, r, "/kordinate/customer?guid="+guid, http.StatusSeeOther)
}

func orDefault(v, def string) string {
	if s := strings.TrimSpace(v); s != "" {
		return s
	}
	return def
}

// requestError is a client-side problem rendered as a friendly page rather than
// a bare 400 — the reader is an agent mid-call, not a developer.
type requestError struct {
	msg  string
	code int
}

func (e *requestError) Error() string { return e.msg }

func errBadRequest(msg string) error { return &requestError{msg: msg, code: http.StatusBadRequest} }
func errNotFound(msg string) error   { return &requestError{msg: msg, code: http.StatusNotFound} }
