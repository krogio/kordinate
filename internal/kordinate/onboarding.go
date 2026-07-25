package kordinate

import (
	"fmt"
	"time"

	"github.com/krogio/kordinate/internal/kordinate/upstream"
)

// onboarding.go is the customer onboarding lifecycle: the explicit state
// machine claire-admin never had.
//
// In claire-admin, onboarding was implicit — a customer's readiness was
// inferred by an agent eyeballing a document list and a status field, and the
// work in flight lived in people's heads and spreadsheets. That is the gap this
// closes: named states, legal transitions, per-state document requirements,
// SLA clocks, and an audit trail on every move.
//
// The lifecycle is deliberately NOT a mirror of the customer service's
// CustomerStatus. Upstream status is what the customer can DO (transact or
// not); the case state is where the WORK has got to. They move independently:
// a SUSPENDED customer can have an open remediation case, and an ACTIVE
// customer can be mid-review.

// State is a position in the onboarding lifecycle.
type State string

const (
	// StateLead — captured, nothing verified yet.
	StateLead State = "LEAD"
	// StateKYCPending — waiting on the customer to submit documents.
	StateKYCPending State = "KYC_PENDING"
	// StateDocsSubmitted — documents are in and awaiting a vetting pass.
	StateDocsSubmitted State = "DOCS_SUBMITTED"
	// StateVetting — under document review (AI-assisted, human-decided).
	StateVetting State = "VETTING"
	// StateInfoRequested — bounced back to the customer for a re-submission.
	StateInfoRequested State = "INFO_REQUESTED"
	// StateScreening — in sanctions/PEP screening.
	StateScreening State = "SCREENING"
	// StateComplianceReview — escalated to AML for a judgement call.
	StateComplianceReview State = "COMPLIANCE_REVIEW"
	// StateApproved — cleared; activation pending.
	StateApproved State = "APPROVED"
	// StateActive — fully onboarded and transacting. Terminal.
	StateActive State = "ACTIVE"
	// StateRejected — declined. Terminal.
	StateRejected State = "REJECTED"
	// StateAbandoned — customer never completed. Terminal.
	StateAbandoned State = "ABANDONED"
)

// stateMeta describes a state for the UI and the rules engine.
type stateMeta struct {
	Label string
	// Description is shown to the agent working the queue.
	Description string
	// Terminal states close the case.
	Terminal bool
	// SLAHours is the clock an open case in this state runs against. Zero means
	// no SLA — states where we're waiting on the CUSTOMER don't accrue a
	// breach against us.
	SLAHours int
	// Next lists the states a case may legally move to.
	Next []State
	// RequiredDocs are the document types that must be APPROVED before the
	// case may advance past vetting.
	RequiredDocs []string
	// Owner is the team that works this state, used for routing and to gate
	// the transition by role.
	Owner Team
}

// Team is the group that owns a state's work. It maps onto kore roles/groups
// via roles.go.
type Team string

const (
	TeamActivations Team = "activations"
	TeamAML         Team = "aml"
	TeamCS          Team = "cs"
	TeamNone        Team = ""
)

// lifecycle is the single source of truth for legal transitions. Keeping it as
// data (not scattered conditionals) is what makes the machine auditable and
// lets the UI render exactly the moves that will be accepted.
var lifecycle = map[State]stateMeta{
	StateLead: {
		Label:       "Lead",
		Description: "Captured, nothing verified yet.",
		SLAHours:    48,
		Owner:       TeamActivations,
		Next:        []State{StateKYCPending, StateAbandoned},
	},
	StateKYCPending: {
		Label:       "KYC pending",
		Description: "Waiting on the customer to submit documents.",
		Owner:       TeamActivations,
		// No SLA: the clock is on the customer, not on us.
		Next: []State{StateDocsSubmitted, StateAbandoned},
	},
	StateDocsSubmitted: {
		Label:       "Documents submitted",
		Description: "Documents received, awaiting review.",
		SLAHours:    24,
		Owner:       TeamActivations,
		Next:        []State{StateVetting, StateInfoRequested, StateAbandoned},
	},
	StateVetting: {
		Label:        "Vetting",
		Description:  "Under document review.",
		SLAHours:     24,
		Owner:        TeamActivations,
		RequiredDocs: []string{upstream.DocPOID},
		Next:         []State{StateScreening, StateInfoRequested, StateComplianceReview, StateRejected},
	},
	StateInfoRequested: {
		Label:       "Information requested",
		Description: "Returned to the customer for a re-submission.",
		Owner:       TeamActivations,
		// Waiting on the customer again — no SLA against us.
		Next: []State{StateDocsSubmitted, StateAbandoned},
	},
	StateScreening: {
		Label:       "Screening",
		Description: "Sanctions and PEP screening in progress.",
		SLAHours:    12,
		Owner:       TeamAML,
		Next:        []State{StateApproved, StateComplianceReview, StateRejected},
	},
	StateComplianceReview: {
		Label:       "Compliance review",
		Description: "Escalated to AML for a decision.",
		SLAHours:    48,
		Owner:       TeamAML,
		Next:        []State{StateApproved, StateRejected, StateInfoRequested},
	},
	StateApproved: {
		Label:       "Approved",
		Description: "Cleared; activation pending.",
		SLAHours:    8,
		Owner:       TeamActivations,
		Next:        []State{StateActive, StateComplianceReview},
	},
	StateActive:    {Label: "Active", Description: "Fully onboarded.", Terminal: true, Owner: TeamNone},
	StateRejected:  {Label: "Rejected", Description: "Application declined.", Terminal: true, Owner: TeamNone},
	StateAbandoned: {Label: "Abandoned", Description: "Customer never completed onboarding.", Terminal: true, Owner: TeamNone},
}

// orderedStates is the display order for the pipeline view — the natural
// left-to-right progression, with terminal outcomes last.
var orderedStates = []State{
	StateLead, StateKYCPending, StateDocsSubmitted, StateVetting,
	StateInfoRequested, StateScreening, StateComplianceReview, StateApproved,
	StateActive, StateRejected, StateAbandoned,
}

// OrderedStates returns the lifecycle states in pipeline order.
func OrderedStates() []State { return orderedStates }

// Valid reports whether a state is one the lifecycle knows.
func (s State) Valid() bool { _, ok := lifecycle[s]; return ok }

// Label is the human name for a state.
func (s State) Label() string {
	if m, ok := lifecycle[s]; ok {
		return m.Label
	}
	return string(s)
}

// Description explains the state to the agent working it.
func (s State) Description() string {
	if m, ok := lifecycle[s]; ok {
		return m.Description
	}
	return ""
}

// Owner returns the team that works this state.
func (s State) Owner() Team {
	if m, ok := lifecycle[s]; ok {
		return m.Owner
	}
	return TeamNone
}

// NextStates lists the legal transitions out of a state.
func (s State) NextStates() []State {
	if m, ok := lifecycle[s]; ok {
		return m.Next
	}
	return nil
}

// RequiredDocs lists document types that must be approved to leave this state.
func (s State) RequiredDocs() []string {
	if m, ok := lifecycle[s]; ok {
		return m.RequiredDocs
	}
	return nil
}

// terminal reports whether a state closes the case.
func terminal(s State) bool {
	m, ok := lifecycle[s]
	return ok && m.Terminal
}

// Terminal reports whether this state closes the case.
func (s State) Terminal() bool { return terminal(s) }

// slaDue computes the SLA deadline for entering a state, or nil where the state
// carries no clock (because we're waiting on the customer).
func slaDue(s State, from time.Time) *time.Time {
	m, ok := lifecycle[s]
	if !ok || m.SLAHours == 0 {
		return nil
	}
	due := from.Add(time.Duration(m.SLAHours) * time.Hour)
	return &due
}

// TransitionError explains a rejected move in terms an agent can act on.
type TransitionError struct {
	From, To State
	Reason   string
}

func (e *TransitionError) Error() string {
	return fmt.Sprintf("cannot move from %s to %s: %s", e.From.Label(), e.To.Label(), e.Reason)
}

// CanTransition reports whether a move is legal, ignoring document
// requirements (which need upstream data — see CheckTransition).
func CanTransition(from, to State) error {
	if !from.Valid() {
		return &TransitionError{From: from, To: to, Reason: "unknown current state"}
	}
	if !to.Valid() {
		return &TransitionError{From: from, To: to, Reason: "unknown target state"}
	}
	if from == to {
		return &TransitionError{From: from, To: to, Reason: "already in this state"}
	}
	if terminal(from) {
		return &TransitionError{From: from, To: to, Reason: "the case is closed"}
	}
	for _, n := range lifecycle[from].Next {
		if n == to {
			return nil
		}
	}
	return &TransitionError{From: from, To: to, Reason: "not a permitted next step"}
}

// DocRequirement is one outstanding document need, for display.
type DocRequirement struct {
	DocType   string
	Label     string
	Status    string
	Satisfied bool
}

// CheckTransition validates a move including document requirements. docs is the
// customer's current document set from the customer service.
//
// The document rule is deliberately satisfied by ANY approved document in the
// required family — a customer proves identity with an SA ID *or* a passport
// *or* an asylum permit, and hard-coding one type would lock out the migrant
// customers who are most of the book.
func CheckTransition(from, to State, docs []upstream.Document) error {
	if err := CanTransition(from, to); err != nil {
		return err
	}
	// Requirements gate progress FORWARD out of vetting, not the bounce-backs
	// (INFO_REQUESTED) or the decline path — you must always be able to reject
	// or ask for more, whatever the document state.
	if to == StateInfoRequested || to == StateRejected || to == StateAbandoned {
		return nil
	}
	for _, req := range from.RequiredDocs() {
		if !docSatisfied(req, docs) {
			return &TransitionError{From: from, To: to,
				Reason: "an approved " + docLabel(req) + " is required first"}
		}
	}
	return nil
}

// identityDocs are the document types that each independently prove identity.
var identityDocs = []string{
	upstream.DocSAIDFront, upstream.DocPassport, upstream.DocForeignIDFront,
	upstream.DocAsylumSeeker, upstream.DocVoterCardFront, upstream.DocPOID,
}

// proofOfIncomeDocs each independently prove source of funds.
var proofOfIncomeDocs = []string{
	upstream.DocPayslip, upstream.DocBankStatement, upstream.DocPOSN,
}

// docSatisfied reports whether an approved, unexpired document meets the
// requirement — treating the identity and income families as interchangeable
// within themselves.
func docSatisfied(required string, docs []upstream.Document) bool {
	family := []string{required}
	switch {
	case contains(identityDocs, required):
		family = identityDocs
	case contains(proofOfIncomeDocs, required):
		family = proofOfIncomeDocs
	}
	for _, d := range docs {
		if d.DocumentStatus != upstream.DocStatusApproved {
			continue
		}
		if contains(family, d.DocumentType) && !docExpired(d) {
			return true
		}
	}
	return false
}

// docExpired reports whether a document's expiry has passed. An unparseable or
// absent expiry counts as NOT expired — many legitimate documents (SA ID book)
// have no expiry, and guessing would block real customers.
func docExpired(d upstream.Document) bool {
	if d.ExpiryDate == "" {
		return false
	}
	for _, layout := range []string{"2006-01-02", time.RFC3339, "02/01/2006"} {
		if t, err := time.Parse(layout, d.ExpiryDate); err == nil {
			return t.Before(time.Now())
		}
	}
	return false
}

// Outstanding lists what a case still needs before it can advance, so the UI
// can show an agent the actual blocking items rather than a bare error.
func Outstanding(s State, docs []upstream.Document) []DocRequirement {
	var out []DocRequirement
	for _, req := range s.RequiredDocs() {
		r := DocRequirement{DocType: req, Label: docLabel(req)}
		if docSatisfied(req, docs) {
			r.Satisfied, r.Status = true, "approved"
		} else {
			r.Status = docPresentStatus(req, docs)
		}
		out = append(out, r)
	}
	return out
}

// docPresentStatus describes why a requirement isn't met yet — pending review,
// rejected, expired, or simply missing. The distinction decides the agent's
// next action, so a flat "missing" would be actively unhelpful.
func docPresentStatus(required string, docs []upstream.Document) string {
	family := []string{required}
	switch {
	case contains(identityDocs, required):
		family = identityDocs
	case contains(proofOfIncomeDocs, required):
		family = proofOfIncomeDocs
	}
	var sawPending, sawRejected, sawExpired bool
	for _, d := range docs {
		if !contains(family, d.DocumentType) {
			continue
		}
		switch {
		case d.DocumentStatus == upstream.DocStatusApproved && docExpired(d):
			sawExpired = true
		case d.DocumentStatus == upstream.DocStatusRejected:
			sawRejected = true
		case d.DocumentStatus == upstream.DocStatusPending || d.DocumentStatus == "":
			sawPending = true
		}
	}
	switch {
	case sawPending:
		return "awaiting review"
	case sawExpired:
		return "expired"
	case sawRejected:
		return "rejected — re-submission needed"
	default:
		return "not submitted"
	}
}

// docLabels are the human names for document types.
var docLabels = map[string]string{
	upstream.DocSAIDFront:      "SA ID (front)",
	upstream.DocSAIDBack:       "SA ID (back)",
	upstream.DocForeignIDFront: "foreign ID (front)",
	upstream.DocForeignIDBack:  "foreign ID (back)",
	upstream.DocPassport:       "passport",
	upstream.DocAsylumSeeker:   "asylum seeker permit",
	upstream.DocVoterCardFront: "voter card (front)",
	upstream.DocVoterCardBack:  "voter card (back)",
	upstream.DocPOID:           "proof of identity",
	upstream.DocPOIDBack:       "proof of identity (back)",
	upstream.DocPOSN:           "proof of source of funds",
	upstream.DocBankStatement:  "bank statement",
	upstream.DocPayslip:        "payslip",
	upstream.DocGeneral:        "supporting document",
	upstream.DocUnspecified:    "document",
}

// DocLabel is the human name for a document type.
func DocLabel(t string) string { return docLabel(t) }

func docLabel(t string) string {
	if l, ok := docLabels[t]; ok {
		return l
	}
	return t
}

// SuggestState infers the lifecycle state a customer is really in from upstream
// data. It is used to seed a case for a customer who predates kordinate — the
// legacy book has no case records, so the first view has to derive one rather
// than dumping every existing customer into LEAD.
func SuggestState(c *upstream.Customer, docs []upstream.Document) State {
	switch c.Status {
	case upstream.StatusActive:
		return StateActive
	case upstream.StatusPermanentlyBlocked, upstream.StatusBlockedPositiveMatch:
		return StateRejected
	case upstream.StatusDuplicate:
		return StateAbandoned
	case upstream.StatusUndergoingScreening:
		return StateScreening
	}
	// Not active: place by document progress.
	if len(docs) == 0 {
		return StateKYCPending
	}
	if docSatisfied(upstream.DocPOID, docs) {
		return StateApproved
	}
	for _, d := range docs {
		if d.DocumentStatus == upstream.DocStatusPending || d.DocumentStatus == "" {
			return StateDocsSubmitted
		}
	}
	return StateInfoRequested
}

func contains(list []string, v string) bool {
	for _, s := range list {
		if s == v {
			return true
		}
	}
	return false
}
