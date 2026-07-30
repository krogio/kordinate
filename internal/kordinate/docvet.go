package kordinate

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/krogio/kordinate/internal/kordinate/upstream"
	"github.com/krogio/kore/ai"
)

// docvet.go is AI-assisted document vetting: a vision model reads an uploaded
// FICA document, reports what it can see, and flags concerns.
//
// The verdict is ADVISORY and always has been by design. A confident-sounding
// model must never be the thing that decides whether a customer is onboarded or
// declined — that is a regulated human decision, and the audit trail records
// the agent who made it. What this earns is speed: the agent opens a document
// already knowing "this passport expired in 2023" or "the name doesn't match
// the customer record", instead of squinting at a photo to find out.
//
// The same reasoning applies to the PII detection in redact.go: detection is
// automated, the decision to reveal an unredacted original is not.

// VetVerdict is the model's overall read on a document.
type VetVerdict string

const (
	// VerdictPass — nothing of concern found.
	VerdictPass VetVerdict = "pass"
	// VerdictConcerns — usable but something needs an agent's eye.
	VerdictConcerns VetVerdict = "concerns"
	// VerdictFail — not usable as submitted (illegible, wrong document, expired).
	VerdictFail VetVerdict = "fail"
	// VerdictError — the check could not be run. Distinct from "fail" so an
	// outage never reads as a finding against the customer.
	VerdictError VetVerdict = "error"
)

// Vetting is the stored result of one AI vetting pass.
type Vetting struct {
	MediaID     string            `json:"mediaId"`
	DocType     string            `json:"docType"`
	Verdict     VetVerdict        `json:"verdict"`
	Confidence  string            `json:"confidence"` // high | medium | low
	Legible     bool              `json:"legible"`
	TypeMatches bool              `json:"typeMatches"`
	NameMatches bool              `json:"nameMatches"`
	DOBMatches  bool              `json:"dobMatches"`
	Expired     bool              `json:"expired"`
	Extracted   map[string]string `json:"extracted"`
	Findings    []string          `json:"findings"`
	Model       string            `json:"model"`
	RequestedBy string            `json:"requestedBy"`
	At          time.Time         `json:"at"`
}

// Advisory is the fixed caveat rendered next to every verdict. Kept as a
// constant so the UI cannot show a verdict without it.
const Advisory = "AI assessment — advisory only. A human reviewer decides."

// vetSchema is the JSON contract the model must return. Constraining the shape
// keeps the parse deterministic and the prompt short.
const vetSchema = `{
  "documentType": "the document type you actually see, using one of: SA_ID_FRONT, SA_ID_BACK, FOREIGN_ID_FRONT, FOREIGN_ID_BACK, PASSPORT, ASYLUM_SEEKER, VOTER_CARD_FRONT, VOTER_CARD_BACK, BANK_STATEMENT, PAYSLIP, OTHER",
  "legible": true,
  "confidence": "high | medium | low",
  "extracted": {
    "fullName": "", "firstName": "", "lastName": "",
    "idNumber": "", "documentNumber": "",
    "dateOfBirth": "YYYY-MM-DD or empty",
    "issueDate": "YYYY-MM-DD or empty",
    "expiryDate": "YYYY-MM-DD or empty",
    "issuingCountry": "", "nationality": ""
  },
  "concerns": ["short, specific, human-readable concerns"],
  "tampering": {"suspected": false, "why": ""}
}`

// vetSystem is the vetting system prompt. It is written to make the model
// report rather than decide, and to say "I can't tell" instead of guessing —
// a fabricated ID number is far worse than a blank field.
const vetSystem = `You are assisting a financial-services compliance officer reviewing a customer identity document.

Report only what you can actually SEE in the image. Rules:
- If a field is not visible or not legible, return an empty string. Never guess, infer, or complete a partial value.
- Do not speculate about the person. Do not judge whether the customer should be approved — that is the officer's decision.
- Flag concerns factually and specifically: what is wrong and where. "Expiry date 2023-04-11 is in the past" beats "document may be invalid".
- Note only tampering signals you can point at (mismatched fonts, misaligned text, inconsistent background, evidence of digital editing). Photo glare, a crease, or a low-quality scan are NOT tampering — call those legibility issues.
- Judge legibility on whether the key fields can be read, not on whether the image is pretty.`

// VetRequest is one vetting call.
type VetRequest struct {
	// Customer is the record to check the document against — the name/DOB
	// comparison is the highest-value check, because a mismatch is how
	// borrowed and stolen documents show up.
	Customer *upstream.Customer
	// Doc is the document metadata as stored upstream.
	Doc upstream.Document
	// Data is the raw document image bytes; MediaType its IANA type.
	Data      []byte
	MediaType string
	// Principal is the agent requesting the check, recorded on the result.
	Principal string
}

// Vetter runs AI document vetting. Nil-safe: with no AI client configured,
// Vet reports VerdictError with a clear reason rather than failing the page —
// document review has to keep working without AI.
type Vetter struct {
	ai  *ai.Client
	now func() time.Time
}

func NewVetter(c *ai.Client) *Vetter { return &Vetter{ai: c, now: time.Now} }

// Available reports whether AI vetting can run, so the UI can hide the button
// rather than offer an action that will fail.
func (v *Vetter) Available() bool { return v != nil && v.ai != nil && v.ai.Enabled() }

// Vet runs a vetting pass over one document.
func (v *Vetter) Vet(ctx context.Context, req VetRequest) (*Vetting, error) {
	res := &Vetting{
		MediaID:     req.Doc.MediaID,
		DocType:     req.Doc.DocumentType,
		RequestedBy: req.Principal,
		At:          v.now().UTC(),
		Extracted:   map[string]string{},
	}
	if !v.Available() {
		res.Verdict = VerdictError
		res.Findings = []string{"AI vetting is not configured (Settings → AI)."}
		return res, nil
	}
	if len(req.Data) == 0 {
		res.Verdict = VerdictError
		res.Findings = []string{"The stored document has no content to review."}
		return res, nil
	}

	out, err := v.ai.Complete(ctx, ai.Request{
		System: vetSystem,
		Prompt: vetPrompt(req),
		Images: []ai.Image{{MediaType: req.MediaType, Data: req.Data}},
		JSON:   true,
		// Cap the response: the schema is small, and a runaway generation on a
		// document image is pure cost.
		MaxTokens: 1500,
		// A verdict on a customer-submitted document: reads text a stranger wrote,
		// and its answer gates what happens to the document. Plan tier — this is
		// not the call to economise on.
		Task: ai.TaskPlan,
	})
	if err != nil {
		res.Verdict = VerdictError
		res.Findings = []string{"The AI check could not be completed: " + err.Error()}
		return res, nil
	}

	if err := applyModelOutput(res, out, req); err != nil {
		res.Verdict = VerdictError
		res.Findings = []string{"The AI returned an unreadable response."}
		return res, nil
	}
	res.Model = v.ai.ProviderName()
	return res, nil
}

func vetPrompt(req VetRequest) string {
	var b strings.Builder
	b.WriteString("Review this customer identity document.\n\n")

	b.WriteString("The document was submitted as: ")
	b.WriteString(DocLabel(req.Doc.DocumentType))
	b.WriteString("\n")

	// Give the model the record to compare against. Only the fields that
	// matter for matching — there is no reason to send the model more customer
	// PII than the check requires.
	if c := req.Customer; c != nil {
		b.WriteString("\nThe customer record says:\n")
		fmt.Fprintf(&b, "- Name: %s\n", c.FullName())
		if c.DateOfBirth != "" {
			fmt.Fprintf(&b, "- Date of birth: %s\n", c.DateOfBirth)
		}
		for _, id := range c.IDNumbers {
			if id.IdentificationNumber != "" {
				fmt.Fprintf(&b, "- ID/passport number on file: %s\n", id.IdentificationNumber)
				break
			}
		}
	}
	if req.Doc.ExpiryDate != "" {
		fmt.Fprintf(&b, "- Expiry date recorded on upload: %s\n", req.Doc.ExpiryDate)
	}
	fmt.Fprintf(&b, "\nToday's date is %s.\n", time.Now().Format("2006-01-02"))

	b.WriteString("\nReturn JSON in exactly this shape:\n")
	b.WriteString(vetSchema)
	return b.String()
}

// modelVetting is the wire shape of the model's reply.
type modelVetting struct {
	DocumentType string            `json:"documentType"`
	Legible      bool              `json:"legible"`
	Confidence   string            `json:"confidence"`
	Extracted    map[string]string `json:"extracted"`
	Concerns     []string          `json:"concerns"`
	Tampering    struct {
		Suspected bool   `json:"suspected"`
		Why       string `json:"why"`
	} `json:"tampering"`
}

// applyModelOutput parses the model reply and derives the verdict.
//
// The verdict is computed HERE, from the model's observations, rather than
// asked for directly. Deriving it keeps the rule visible and consistent — an
// expired document is a fail whether or not the model chose to call it one.
func applyModelOutput(res *Vetting, out string, req VetRequest) error {
	var m modelVetting
	if err := json.Unmarshal([]byte(out), &m); err != nil {
		return fmt.Errorf("parse vetting response: %w", err)
	}

	res.Legible = m.Legible
	res.Confidence = normaliseConfidence(m.Confidence)
	if m.Extracted != nil {
		res.Extracted = m.Extracted
	}
	res.Findings = append(res.Findings, m.Concerns...)

	// Type check: compare what the model saw against what was claimed. OTHER
	// isn't a mismatch on its own — plenty of legitimate supporting documents
	// don't fit the taxonomy.
	seen := strings.ToUpper(strings.TrimSpace(m.DocumentType))
	res.TypeMatches = seen == "" || seen == "OTHER" ||
		strings.EqualFold(seen, req.Doc.DocumentType) ||
		sameDocFamily(seen, req.Doc.DocumentType)
	if !res.TypeMatches {
		res.Findings = append(res.Findings,
			fmt.Sprintf("Submitted as %s but appears to be %s.",
				DocLabel(req.Doc.DocumentType), DocLabel(seen)))
	}

	// Name and DOB matching against the customer record.
	if req.Customer != nil {
		res.NameMatches = nameMatches(req.Customer, res.Extracted)
		if !res.NameMatches && res.Extracted["fullName"] != "" {
			res.Findings = append(res.Findings,
				fmt.Sprintf("Name on document (%s) does not match the customer record (%s).",
					res.Extracted["fullName"], req.Customer.FullName()))
		}
		res.DOBMatches = dobMatches(req.Customer.DateOfBirth, res.Extracted["dateOfBirth"])
		if !res.DOBMatches && res.Extracted["dateOfBirth"] != "" && req.Customer.DateOfBirth != "" {
			res.Findings = append(res.Findings,
				fmt.Sprintf("Date of birth on document (%s) does not match the record (%s).",
					res.Extracted["dateOfBirth"], req.Customer.DateOfBirth))
		}
	}

	// Expiry: trust the document over the upload metadata, since the metadata
	// is hand-entered and the document is the evidence.
	if exp := res.Extracted["expiryDate"]; exp != "" {
		if t, err := parseFlexibleDate(exp); err == nil && t.Before(time.Now()) {
			res.Expired = true
			res.Findings = append(res.Findings,
				fmt.Sprintf("Document expired on %s.", t.Format("2 January 2006")))
		}
	}

	if m.Tampering.Suspected {
		why := m.Tampering.Why
		if why == "" {
			why = "no detail given"
		}
		res.Findings = append(res.Findings, "Possible tampering: "+why)
	}

	switch {
	case !res.Legible, res.Expired, !res.TypeMatches, m.Tampering.Suspected:
		res.Verdict = VerdictFail
	case len(res.Findings) > 0, !res.NameMatches && req.Customer != nil:
		res.Verdict = VerdictConcerns
	default:
		res.Verdict = VerdictPass
	}
	if !res.Legible {
		res.Findings = append(res.Findings, "Key fields are not legible in this image.")
	}
	return nil
}

// docFamilies groups types that a model may reasonably report interchangeably —
// it cannot know from a photo whether the operator filed an ID as POID.
var docFamilies = [][]string{
	{upstream.DocSAIDFront, upstream.DocSAIDBack, upstream.DocPOID, upstream.DocPOIDBack},
	{upstream.DocForeignIDFront, upstream.DocForeignIDBack, upstream.DocPassport, upstream.DocPOID},
	{upstream.DocVoterCardFront, upstream.DocVoterCardBack},
	{upstream.DocBankStatement, upstream.DocPayslip, upstream.DocPOSN},
}

func sameDocFamily(a, b string) bool {
	for _, fam := range docFamilies {
		if contains(fam, a) && contains(fam, b) {
			return true
		}
	}
	return false
}

// nameMatches compares the document name to the record. Deliberately lenient
// on ordering and middle names — "Tendai M Chikwanha" and "Chikwanha Tendai"
// are the same person, and a false mismatch sends a real customer away.
func nameMatches(c *upstream.Customer, extracted map[string]string) bool {
	docName := strings.TrimSpace(extracted["fullName"])
	if docName == "" {
		docName = strings.TrimSpace(extracted["firstName"] + " " + extracted["lastName"])
	}
	if docName == "" {
		// Nothing read off the document — not a mismatch, just no evidence.
		return true
	}
	docParts := nameTokens(docName)
	recParts := nameTokens(c.FullName())
	if len(docParts) == 0 || len(recParts) == 0 {
		return true
	}
	// Every token in the shorter list must appear in the longer one, so a
	// missing middle name passes but a different surname does not.
	shorter, longer := docParts, recParts
	if len(longer) < len(shorter) {
		shorter, longer = longer, shorter
	}
	matched := 0
	for _, s := range shorter {
		if contains(longer, s) {
			matched++
		}
	}
	// Require at least two matching tokens when both sides have two or more —
	// one shared token (often a common first name) is too weak a signal.
	need := 2
	if len(shorter) < 2 {
		need = 1
	}
	return matched >= need
}

// nameTokens lowercases and splits a name, dropping single initials which carry
// no matching value.
func nameTokens(s string) []string {
	var out []string
	for _, part := range strings.FieldsFunc(strings.ToLower(s), func(r rune) bool {
		return r == ' ' || r == '-' || r == '.' || r == ','
	}) {
		if len(part) > 1 {
			out = append(out, part)
		}
	}
	return out
}

// dobMatches compares dates of birth across the several formats these services
// and documents use. Absent on either side is not a mismatch.
func dobMatches(record, doc string) bool {
	if record == "" || doc == "" {
		return true
	}
	rt, rerr := parseFlexibleDate(record)
	dt, derr := parseFlexibleDate(doc)
	if rerr != nil || derr != nil {
		return strings.TrimSpace(record) == strings.TrimSpace(doc)
	}
	return rt.Year() == dt.Year() && rt.Month() == dt.Month() && rt.Day() == dt.Day()
}

// parseFlexibleDate handles the date formats seen across these services and
// documents — the customer service, Claire and the documents themselves each
// differ, and claire-admin had scattered ad-hoc conversions for it.
func parseFlexibleDate(s string) (time.Time, error) {
	s = strings.TrimSpace(s)
	layouts := []string{
		"2006-01-02", time.RFC3339, "2006/01/02",
		"02/01/2006", "02-01-2006", "02 January 2006", "2 January 2006",
		"20060102",
	}
	for _, l := range layouts {
		if t, err := time.Parse(l, s); err == nil {
			return t, nil
		}
	}
	return time.Time{}, fmt.Errorf("unrecognised date %q", s)
}

func normaliseConfidence(c string) string {
	switch strings.ToLower(strings.TrimSpace(c)) {
	case "high":
		return "high"
	case "medium", "med":
		return "medium"
	case "low":
		return "low"
	default:
		return "low"
	}
}

// Summary is a one-line description of the verdict for list views.
func (v *Vetting) Summary() string {
	switch v.Verdict {
	case VerdictPass:
		return "No concerns found"
	case VerdictConcerns:
		return fmt.Sprintf("%d concern(s) to review", len(v.Findings))
	case VerdictFail:
		if len(v.Findings) > 0 {
			return v.Findings[0]
		}
		return "Not usable as submitted"
	default:
		return "Check could not be run"
	}
}
