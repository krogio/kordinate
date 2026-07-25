package kordinate

import (
	"encoding/json"
	"html/template"
	"log/slog"
	"sort"

	"github.com/krogio/kordinate/internal/kordinate/upstream"
)

// view.go holds the view-only types and option lists the templates read.
//
// Permission flags are NOT built here. Each handler passes the one or two
// booleans its page actually reads, derived from the request's role — the same
// shape konform uses (canAdmin at standards.go). A blanket flag map invited
// dead keys and aliases that drift from the permission they claim to mirror.

// QueueFilter is the onboarding queue's filter as the UI models it: one selected
// state rather than the store's slice, because the screen offers a single
// dropdown. Kept separate from CaseQuery so the store's query stays general.
type QueueFilter struct {
	State         State
	Assignee      string
	Unassigned    bool
	OverdueOnly   bool
	IncludeClosed bool
}

// StateTile is one summary tile on the onboarding queue.
type StateTile struct {
	State   State
	Count   int
	Overdue int
}

// stateTiles builds the queue's summary tiles. Only non-terminal states get a
// tile: a count of everyone ever rejected is not work, and it would dominate
// the row.
func stateTiles(counts map[State]int, cases []Case) []StateTile {
	overdue := map[State]int{}
	for _, k := range cases {
		if k.SLADueAt != nil && k.ClosedAt == nil {
			overdue[k.State]++
		}
	}
	var out []StateTile
	for _, s := range OrderedStates() {
		if s.Terminal() {
			continue
		}
		out = append(out, StateTile{State: s, Count: counts[s], Overdue: overdue[s]})
	}
	return out
}

// DocumentQueueItem is one row on the document review queue: a case plus the
// document awaiting a decision and its last AI verdict.
type DocumentQueueItem struct {
	MMGuid      string
	DisplayName string
	MSISDN      string
	Doc         upstream.Document
	Vetting     *Vetting
}

// riskScores are the selectable compliance risk ratings (claire-admin stored
// 1–6).
func riskScores() []int { return []int{1, 2, 3, 4, 5, 6} }

// docTypes are the document types an agent may file, in the order the upload
// form offers them — identity first, then proof of income, then supporting.
func docTypes() []string {
	return []string{
		upstream.DocSAIDFront, upstream.DocSAIDBack,
		upstream.DocForeignIDFront, upstream.DocForeignIDBack,
		upstream.DocPassport, upstream.DocAsylumSeeker,
		upstream.DocVoterCardFront, upstream.DocVoterCardBack,
		upstream.DocPOID, upstream.DocPOIDBack,
		upstream.DocPayslip, upstream.DocBankStatement, upstream.DocPOSN,
		upstream.DocGeneral,
	}
}

// idTypes are the identification types the create-customer form offers.
func idTypes() []string {
	return []string{"UNSPECIFIED", "SA_ID", "PASSPORT", "ASYLUM_SEEKER", "FOREIGN_ID", "VOTER_CARD"}
}

// piiKinds are the redaction classes an agent can assign to a box.
func piiKinds() []PIIKind {
	return []PIIKind{
		PIIIDNumber, PIIPassport, PIIFace, PIISignature,
		PIIAddress, PIIDOB, PIIPhone, PIIEmail, PIIAccount, PIIOther,
	}
}

// Sentiment is a tag an agent picks when noting a call.
type Sentiment struct {
	Label string
	Icon  string
}

// defaultSentiments are the built-in call sentiments, used when the account
// hasn't customised the list.
func defaultSentiments() []Sentiment {
	return []Sentiment{
		{"Happy", "smile"}, {"Neutral", "meh"}, {"Frustrated", "frown"},
		{"Confused", "help"}, {"Escalated", "alert"},
	}
}

// assignees lists the emails a case may be assigned to: whoever already owns
// work, plus the caller. Derived from existing cases rather than the full user
// directory because the directory can hold hundreds of accounts, and the useful
// list is the handful of people actually working the queue.
func assignees(cases []Case, me string) []string {
	seen := map[string]bool{}
	if me != "" {
		seen[me] = true
	}
	for _, k := range cases {
		if k.Assignee != "" {
			seen[k.Assignee] = true
		}
	}
	out := make([]string, 0, len(seen))
	for a := range seen {
		out = append(out, a)
	}
	sort.Strings(out)
	return out
}

// regionsJSON marshals proposed redaction regions for the canvas editor's
// data-regions attribute.
//
// The return type is template.JS deliberately: as a plain string, html/template
// would entity-escape the JSON inside the attribute and the browser's
// JSON.parse would fail. This value is generated from our own structs, never
// from user input, so marking it safe is sound.
func regionsJSON(regions []Region) template.JS {
	if len(regions) == 0 {
		return template.JS("[]")
	}
	b, err := json.Marshal(regions)
	if err != nil {
		slog.Error("kordinate: encoding redaction regions failed", "error", err)
		return template.JS("[]")
	}
	return template.JS(b)
}
