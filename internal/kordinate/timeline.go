package kordinate

import (
	"context"
	"fmt"
	"log/slog"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/krogio/kordinate/internal/kordinate/upstream"
)

// timeline.go is the customer 360 feed.
//
// claire-admin made an agent open five screens — transactions, wallet, EFT
// deposits, documents, notes — and reconstruct the story in their head while a
// customer waited on the line. This stitches every event about one customer,
// from every microservice plus kordinate's own tables, into one chronological
// list with source attribution.
//
// Two design points matter more than the mapping code:
//
//  1. The fan-out is concurrent. Six serial HTTP calls to services that each
//     sit behind a VPN is measured in seconds, and a call-centre agent will not
//     wait.
//  2. Partial failure is the NORMAL case, not the exception. One dead service
//     must never blank the feed, so every source's error is captured against
//     its name and the rest of the story still renders. An agent told "wallet
//     history unavailable" can work; an agent shown a silently incomplete
//     history will give a customer the wrong answer.

// EventKind classifies a timeline entry. The UI groups and filters on it.
type EventKind string

const (
	KindOrder          EventKind = "order"
	KindWalletTxn      EventKind = "wallet_txn"
	KindEFT            EventKind = "eft"
	KindDocument       EventKind = "document"
	KindStatusChange   EventKind = "status_change"
	KindNote           EventKind = "note"
	KindCaseTransition EventKind = "case_transition"
	KindDevice         EventKind = "device"
	KindVoucher        EventKind = "voucher"
	KindCard           EventKind = "card"
	KindAccess         EventKind = "access"
)

// Source names, used as both attribution labels and Timeline.Errors keys.
const (
	SourceUOPS      = "uops"
	SourceUML       = "uml"
	SourceEmma      = "emma"
	SourceCustomer  = "customer-service"
	SourceDevice    = "device-blocker"
	SourceVMS       = "vms"
	SourceKordinate = "kordinate"
)

// Event is one thing that happened to a customer.
type Event struct {
	At       time.Time
	Kind     EventKind
	Source   string
	Title    string
	Detail   string
	Amount   *float64
	Currency string
	Status   string
	Ref      string
	Actor    string
	Icon     string
	// TargetID is the upstream identifier the UI deep-links to.
	TargetID string
}

// Timeline is the merged feed plus whatever went wrong assembling it.
type Timeline struct {
	Events []Event
	// Errors reports per-source retrieval failures so the UI can say
	// "wallet history unavailable" instead of silently showing an incomplete
	// story.
	Errors map[string]string
}

// Degraded reports whether any source failed, i.e. whether the feed on screen
// is known to be incomplete.
func (t *Timeline) Degraded() bool { return t != nil && len(t.Errors) > 0 }

// Filter returns the events matching any of the given kinds. No kinds means
// everything, so a handler can pass its query params through unconditionally.
func (t *Timeline) Filter(kinds ...EventKind) []Event {
	if t == nil {
		return nil
	}
	if len(kinds) == 0 {
		return t.Events
	}
	want := make(map[EventKind]bool, len(kinds))
	for _, k := range kinds {
		want[k] = true
	}
	out := make([]Event, 0, len(t.Events))
	for _, e := range t.Events {
		if want[e.Kind] {
			out = append(out, e)
		}
	}
	return out
}

// TimelineBuilder assembles timelines from the upstream set and the local store.
type TimelineBuilder struct {
	up upstream.Set
	st *Store
}

func NewTimelineBuilder(up upstream.Set, st *Store) *TimelineBuilder {
	return &TimelineBuilder{up: up, st: st}
}

// Build fans out to every source and merges the results, newest first.
//
// It returns an error only when the request itself is unusable (no customer
// identity); a source that fails lands in Timeline.Errors and the timeline is
// still returned.
func (b *TimelineBuilder) Build(ctx context.Context, account, mmGuid string, from, to time.Time) (*Timeline, error) {
	if mmGuid == "" {
		return nil, fmt.Errorf("build timeline: mm guid required")
	}
	if to.IsZero() {
		to = time.Now().UTC()
	}
	if from.IsZero() {
		from = to.AddDate(-1, 0, 0)
	}

	t := &Timeline{Errors: map[string]string{}}

	var (
		mu sync.Mutex
		wg sync.WaitGroup
	)
	// collect is the only writer path into t, so every fetcher can be a plain
	// closure without threading channels through the mapping code.
	collect := func(source string, events []Event, err error) {
		mu.Lock()
		defer mu.Unlock()
		if err != nil {
			t.Errors[source] = err.Error()
			slog.WarnContext(ctx, "timeline source failed",
				"source", source, "mm_guid", mmGuid, "error", err)
			return
		}
		t.Events = append(t.Events, events...)
	}
	run := func(source string, fn func() ([]Event, error)) {
		wg.Add(1)
		go func() {
			defer wg.Done()
			events, err := fn()
			collect(source, events, err)
		}()
	}

	if b.up.UOPS != nil {
		run(SourceUOPS, func() ([]Event, error) { return b.orders(ctx, mmGuid, from, to) })
	}
	if b.up.UML != nil {
		run(SourceUML, func() ([]Event, error) { return b.wallet(ctx, mmGuid, from, to) })
	}
	if b.up.Emma != nil {
		run(SourceEmma, func() ([]Event, error) { return b.eft(ctx, mmGuid, from, to) })
	}
	if b.up.Customer != nil {
		run(SourceCustomer, func() ([]Event, error) { return b.documents(ctx, mmGuid, from, to) })
	}
	if b.up.Device != nil {
		run(SourceDevice, func() ([]Event, error) { return b.devices(ctx, mmGuid, from, to) })
	}
	if b.up.VMS != nil {
		run(SourceVMS, func() ([]Event, error) { return b.vouchers(ctx, mmGuid, from, to) })
	}
	if b.st != nil {
		run(SourceKordinate, func() ([]Event, error) { return b.local(ctx, account, mmGuid, from, to) })
	}

	wg.Wait()

	sortEvents(t.Events)
	if len(t.Errors) == 0 {
		t.Errors = nil
	}
	return t, nil
}

// sortEvents orders newest first. The secondary keys exist so two events
// sharing a timestamp — common when a service stamps a whole batch with one
// time — keep a stable order between page loads instead of appearing to
// shuffle as goroutines finish in a different sequence.
func sortEvents(events []Event) {
	sort.SliceStable(events, func(i, j int) bool {
		a, c := events[i], events[j]
		if !a.At.Equal(c.At) {
			return a.At.After(c.At)
		}
		if a.Kind != c.Kind {
			return a.Kind < c.Kind
		}
		if a.Ref != c.Ref {
			return a.Ref < c.Ref
		}
		return a.Title < c.Title
	})
}

// ---------- upstream sources ----------

func (b *TimelineBuilder) orders(ctx context.Context, mmGuid string, from, to time.Time) ([]Event, error) {
	orders, err := b.up.UOPS.CustomerOrders(ctx, mmGuid, from, to)
	if err != nil {
		return nil, fmt.Errorf("uops orders: %w", err)
	}
	out := make([]Event, 0, len(orders))
	for _, o := range orders {
		at := parseUpstreamTime(o.TimeUpdated, o.TimeCreated)
		if !inWindow(at, from, to) {
			continue
		}
		amount := o.Amount
		source := SourceUOPS
		if o.Source != "" {
			source = o.Source
		}
		e := Event{
			At:       at,
			Kind:     KindOrder,
			Source:   source,
			Title:    orderTitle(o),
			Amount:   &amount,
			Currency: "ZAR",
			Status:   string(o.OrderStatus),
			Ref:      o.OrderReferenceNumber,
			Icon:     "send",
			TargetID: o.OrderID,
		}
		var detail []string
		if o.FeeAmount > 0 {
			detail = append(detail, "fee "+FormatZAR(o.FeeAmount))
		}
		if o.LatePayment {
			detail = append(detail, "late payment")
		}
		e.Detail = strings.Join(detail, " · ")
		out = append(out, e)
	}
	return out, nil
}

func orderTitle(o upstream.Order) string {
	verb := "Transaction"
	switch o.Product {
	case upstream.ProductRemittance:
		verb = "Sent"
	case upstream.ProductWallet:
		verb = "Wallet order"
	case upstream.ProductBanking:
		verb = "Banking payment"
	case upstream.ProductUSDSavings:
		verb = "USD savings"
	}
	title := verb + " " + FormatZAR(o.Amount)
	if o.PaymentMethod != "" {
		title += " via " + humanise(string(o.PaymentMethod))
	}
	if o.Product != "" {
		title += " (" + string(o.Product) + ")"
	}
	return title
}

func (b *TimelineBuilder) wallet(ctx context.Context, mmGuid string, from, to time.Time) ([]Event, error) {
	txns, err := b.up.UML.WalletTransactions(ctx, mmGuid, from, to)
	if err != nil {
		return nil, fmt.Errorf("uml wallet transactions: %w", err)
	}
	out := make([]Event, 0, len(txns))
	for _, tx := range txns {
		if !inWindow(tx.At, from, to) {
			continue
		}
		amount := tx.Amount
		title := tx.Description
		if title == "" {
			title = walletFallbackTitle(tx)
		}
		// Card ledger entries are the same wallet feed upstream, but an agent
		// looking for "what happened on the card" filters on them separately.
		kind := KindWalletTxn
		icon := "wallet"
		if isCardTxn(tx.Type) {
			kind, icon = KindCard, "card"
		}
		out = append(out, Event{
			At:       tx.At,
			Kind:     kind,
			Source:   SourceUML,
			Title:    title,
			Detail:   "balance " + FormatZAR(tx.Balance),
			Amount:   &amount,
			Currency: "ZAR",
			Status:   humanise(tx.Type),
			Ref:      firstNonEmpty(tx.Reference, tx.TransactionID),
			Icon:     icon,
			TargetID: tx.TransactionID,
		})
	}
	return out, nil
}

func walletFallbackTitle(tx upstream.WalletTransaction) string {
	if tx.Amount < 0 {
		return "Wallet debit " + FormatZAR(-tx.Amount)
	}
	return "Wallet credit " + FormatZAR(tx.Amount)
}

func isCardTxn(t string) bool {
	u := strings.ToUpper(t)
	return strings.Contains(u, "CARD") || strings.Contains(u, "POS") || strings.Contains(u, "ATM")
}

func (b *TimelineBuilder) eft(ctx context.Context, mmGuid string, from, to time.Time) ([]Event, error) {
	// Emma has no date filter on the per-customer endpoint, so the window is
	// applied here.
	notes, err := b.up.Emma.NotificationsByCustomer(ctx, mmGuid)
	if err != nil {
		return nil, fmt.Errorf("emma notifications: %w", err)
	}
	out := make([]Event, 0, len(notes))
	for _, n := range notes {
		if !inWindow(n.DateReceived, from, to) {
			continue
		}
		amount := n.Amount
		channel := "EFT"
		if n.PaymentChannel != "" {
			channel = humanise(n.PaymentChannel)
		}
		title := channel + " deposit " + FormatZAR(n.Amount)
		if n.Bank != "" {
			title += " from " + n.Bank
		}
		out = append(out, Event{
			At:       n.DateReceived,
			Kind:     KindEFT,
			Source:   SourceEmma,
			Title:    title,
			Detail:   eftDetail(n.ProcessOutcome),
			Amount:   &amount,
			Currency: "ZAR",
			Status:   n.ProcessOutcome,
			Ref:      n.OriginalReference,
			Icon:     "deposit",
			TargetID: strconv.FormatInt(n.EFTNotificationID, 10),
		})
	}
	return out, nil
}

func eftDetail(outcome string) string {
	switch outcome {
	case upstream.EFTManualIntervention:
		return "Needs manual handling"
	case upstream.EFTPendingProcessing:
		return "Awaiting processing"
	case upstream.EFTOrderPaid, upstream.EFTManualOrderPaid:
		return "Applied to an order"
	case upstream.EFTWalletOrderCreated:
		return "Credited to wallet"
	case upstream.EFTPurged:
		return "Purged"
	default:
		return humanise(outcome)
	}
}

func (b *TimelineBuilder) documents(ctx context.Context, mmGuid string, from, to time.Time) ([]Event, error) {
	docs, err := b.up.Customer.ListDocuments(ctx, mmGuid)
	if err != nil {
		return nil, fmt.Errorf("customer documents: %w", err)
	}
	out := make([]Event, 0, len(docs))
	for _, d := range docs {
		at := parseUpstreamTime(d.TimeCreated, d.IssueDate)
		if !inWindow(at, from, to) {
			continue
		}
		out = append(out, Event{
			At:       at,
			Kind:     KindDocument,
			Source:   SourceCustomer,
			Title:    documentTitle(d),
			Detail:   documentDetail(d),
			Status:   d.DocumentStatus,
			Ref:      d.DocumentNumber,
			Actor:    d.ProcessingAgentID,
			Icon:     "document",
			TargetID: d.MediaID,
		})
	}
	return out, nil
}

func documentTitle(d upstream.Document) string {
	label := docLabel(d.DocumentType)
	// Titles read as a sentence, so capitalise the leading label ("SA ID
	// approved", not "sA ID approved").
	if label != "" {
		label = strings.ToUpper(label[:1]) + label[1:]
	}
	switch d.DocumentStatus {
	case upstream.DocStatusApproved:
		return label + " approved"
	case upstream.DocStatusRejected:
		return label + " rejected"
	default:
		return label + " uploaded"
	}
}

func documentDetail(d upstream.Document) string {
	var parts []string
	if d.DocumentName != "" {
		parts = append(parts, d.DocumentName)
	}
	if d.IssuingCountry != "" {
		parts = append(parts, "issued by "+d.IssuingCountry)
	}
	if d.ExpiryDate != "" {
		parts = append(parts, "expires "+d.ExpiryDate)
	}
	if d.InboundChannel != "" {
		parts = append(parts, "via "+humanise(d.InboundChannel))
	}
	return strings.Join(parts, " · ")
}

func (b *TimelineBuilder) devices(ctx context.Context, mmGuid string, from, to time.Time) ([]Event, error) {
	devices, err := b.up.Device.DevicesForCustomer(ctx, mmGuid)
	if err != nil {
		return nil, fmt.Errorf("device blocker: %w", err)
	}
	out := make([]Event, 0, len(devices))
	for _, d := range devices {
		// The blocker keeps no per-change history, so LastSeen is the best
		// available "when" for the device's current state.
		at := parseUpstreamTime(d.LastSeen, d.FirstSeen)
		if !inWindow(at, from, to) {
			continue
		}
		title := "Device seen"
		if d.DeviceStatus == upstream.DeviceBlocked {
			title = "Device blocked"
		}
		detail := ""
		if n := len(d.LinkedCustomers); n > 1 {
			detail = fmt.Sprintf("shared with %d other customers", n-1)
		}
		out = append(out, Event{
			At:       at,
			Kind:     KindDevice,
			Source:   SourceDevice,
			Title:    title,
			Detail:   detail,
			Status:   string(d.DeviceStatus),
			Ref:      d.DeviceID,
			Icon:     "device",
			TargetID: d.DeviceID,
		})
	}
	return out, nil
}

func (b *TimelineBuilder) vouchers(ctx context.Context, mmGuid string, from, to time.Time) ([]Event, error) {
	vouchers, err := b.up.VMS.VouchersForCustomer(ctx, mmGuid)
	if err != nil {
		return nil, fmt.Errorf("vms vouchers: %w", err)
	}
	out := make([]Event, 0, len(vouchers))
	for _, v := range vouchers {
		currency := firstNonEmpty(v.Currency, "ZAR")
		// Issue and redemption are separate moments in the customer's story and
		// an agent chasing "did they use it" needs both.
		if inWindow(v.CreatedAt, from, to) {
			amount := v.Amount
			out = append(out, Event{
				At:       v.CreatedAt,
				Kind:     KindVoucher,
				Source:   SourceVMS,
				Title:    "Voucher issued " + formatMoney(currency, v.Amount),
				Detail:   voucherRecipient(v.Recipient),
				Amount:   &amount,
				Currency: currency,
				Status:   v.Status,
				Ref:      v.Code,
				Icon:     "voucher",
				TargetID: v.Code,
			})
		}
		if v.RedeemedAt != nil && inWindow(*v.RedeemedAt, from, to) {
			amount := v.Amount
			out = append(out, Event{
				At:       *v.RedeemedAt,
				Kind:     KindVoucher,
				Source:   SourceVMS,
				Title:    "Voucher redeemed " + formatMoney(currency, v.Amount),
				Detail:   voucherRecipient(v.Recipient),
				Amount:   &amount,
				Currency: currency,
				Status:   "REDEEMED",
				Ref:      v.Code,
				Icon:     "voucher",
				TargetID: v.Code,
			})
		}
	}
	return out, nil
}

func voucherRecipient(r upstream.VoucherRecipient) string {
	who := firstNonEmpty(r.Name, r.MSISDN, r.Email)
	if who == "" {
		return ""
	}
	return "for " + who
}

// ---------- kordinate's own tables ----------

// local gathers notes, case transitions and access-log entries. The three
// queries share one source slot because they hit the same database: if it's
// down, all three are down, and reporting one failure is the honest signal.
func (b *TimelineBuilder) local(ctx context.Context, account, mmGuid string, from, to time.Time) ([]Event, error) {
	var out []Event

	notes, err := b.st.Notes(ctx, account, mmGuid, 500)
	if err != nil {
		return nil, fmt.Errorf("local notes: %w", err)
	}
	for _, n := range notes {
		if !inWindow(n.At, from, to) {
			continue
		}
		out = append(out, Event{
			At:       n.At,
			Kind:     KindNote,
			Source:   SourceKordinate,
			Title:    truncate(collapseSpace(n.Body), 90),
			Detail:   noteDetail(n),
			Status:   n.Sentiment,
			Ref:      n.TicketRef,
			Actor:    n.Author,
			Icon:     "note",
			TargetID: strconv.FormatInt(n.ID, 10),
		})
	}

	if c, err := b.st.OpenCaseFor(ctx, account, mmGuid); err != nil {
		return nil, fmt.Errorf("local case: %w", err)
	} else if c != nil {
		history, err := b.st.CaseHistory(ctx, account, c.ID)
		if err != nil {
			return nil, fmt.Errorf("local case history: %w", err)
		}
		for _, tr := range history {
			if !inWindow(tr.At, from, to) {
				continue
			}
			out = append(out, Event{
				At:       tr.At,
				Kind:     KindCaseTransition,
				Source:   SourceKordinate,
				Title:    transitionTitle(tr),
				Detail:   tr.Note,
				Status:   string(tr.ToState),
				Ref:      strconv.FormatInt(c.ID, 10),
				Actor:    tr.Actor,
				Icon:     "flow",
				TargetID: strconv.FormatInt(c.ID, 10),
			})
		}
	}

	access, err := b.st.AccessLog(ctx, account, mmGuid, 500)
	if err != nil {
		return nil, fmt.Errorf("local access log: %w", err)
	}
	for _, a := range access {
		if !inWindow(a.At, from, to) {
			continue
		}
		out = append(out, Event{
			At:       a.At,
			Kind:     KindAccess,
			Source:   SourceKordinate,
			Title:    accessTitle(a),
			Detail:   a.Reason,
			Actor:    a.Principal,
			Ref:      a.Target,
			Icon:     "eye",
			TargetID: a.Target,
		})
	}

	return out, nil
}

func noteDetail(n Note) string {
	var parts []string
	if n.Author != "" {
		parts = append(parts, n.Author)
	}
	if n.Subject != "" && n.Subject != "customer" {
		parts = append(parts, "on "+n.Subject)
	}
	if n.TicketRef != "" {
		parts = append(parts, n.TicketRef)
	}
	return strings.Join(parts, " · ")
}

func transitionTitle(tr Transition) string {
	// An assignment change is recorded as a same-state transition; rendering it
	// as "Moved from Vetting to Vetting" would read as a bug.
	if tr.FromState == tr.ToState {
		if tr.Note != "" {
			return tr.Note + " by " + tr.Actor
		}
		return "Case updated by " + tr.Actor
	}
	if tr.FromState == "" {
		return "Case opened in " + tr.ToState.Label() + " by " + tr.Actor
	}
	return "Moved from " + tr.FromState.Label() + " to " + tr.ToState.Label() + " by " + tr.Actor
}

func accessTitle(a AccessEntry) string {
	action := "Data accessed"
	switch a.Action {
	case AccessViewCustomer:
		action = "Customer profile viewed"
	case AccessViewDocument:
		action = "Document viewed"
	case AccessRevealUnredacted:
		action = "Unredacted document revealed"
	case AccessExport:
		action = "Data exported"
	}
	if a.Principal != "" {
		return action + " by " + a.Principal
	}
	return action
}

// ---------- formatting helpers ----------

// FormatZAR renders an amount as R1,200.00. Agents read these numbers out to
// customers over the phone, so grouping is not cosmetic.
func FormatZAR(v float64) string { return "R" + thousands(v) }

func formatMoney(currency string, v float64) string {
	if currency == "" || currency == "ZAR" {
		return FormatZAR(v)
	}
	return currency + " " + thousands(v)
}

func thousands(v float64) string {
	neg := v < 0
	if neg {
		v = -v
	}
	s := strconv.FormatFloat(v, 'f', 2, 64)
	intPart, frac := s[:len(s)-3], s[len(s)-3:]

	var sb strings.Builder
	if neg {
		sb.WriteByte('-')
	}
	for i, d := range intPart {
		if i > 0 && (len(intPart)-i)%3 == 0 {
			sb.WriteByte(',')
		}
		sb.WriteRune(d)
	}
	sb.WriteString(frac)
	return sb.String()
}

// humanise turns an upstream SCREAMING_SNAKE enum into something readable
// ("MANUAL_INTERVENTION_REQUIRED" -> "Manual intervention required").
func humanise(s string) string {
	if s == "" {
		return ""
	}
	words := strings.Split(strings.ToLower(strings.ReplaceAll(s, "_", " ")), " ")
	for i, w := range words {
		if i == 0 && w != "" {
			words[i] = strings.ToUpper(w[:1]) + w[1:]
		}
	}
	return strings.Join(words, " ")
}

func truncate(s string, n int) string {
	r := []rune(s)
	if len(r) <= n {
		return s
	}
	return strings.TrimRight(string(r[:n]), " ") + "…"
}

func collapseSpace(s string) string { return strings.Join(strings.Fields(s), " ") }

func firstNonEmpty(vals ...string) string {
	for _, v := range vals {
		if v != "" {
			return v
		}
	}
	return ""
}

// inWindow keeps an event when it falls inside the requested range. A zero
// timestamp is kept: upstream records with unparseable dates are real events,
// and dropping them would lose history rather than merely misplace it.
func inWindow(at, from, to time.Time) bool {
	if at.IsZero() {
		return true
	}
	if !from.IsZero() && at.Before(from) {
		return false
	}
	if !to.IsZero() && at.After(to) {
		return false
	}
	return true
}

// upstreamTimeLayouts covers the formats the services actually emit — the
// newer ones are RFC3339, Claire-derived records are MySQL DATETIME strings.
var upstreamTimeLayouts = []string{
	time.RFC3339Nano,
	time.RFC3339,
	"2006-01-02T15:04:05",
	"2006-01-02 15:04:05",
	"2006-01-02",
}

// parseUpstreamTime tries each candidate string in order and returns the first
// that parses, or the zero time when none do.
func parseUpstreamTime(candidates ...string) time.Time {
	for _, c := range candidates {
		if c == "" {
			continue
		}
		for _, layout := range upstreamTimeLayouts {
			if t, err := time.Parse(layout, c); err == nil {
				return t.UTC()
			}
		}
	}
	return time.Time{}
}
