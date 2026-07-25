package kordinate

import (
	"encoding/json"
	"io"
	"net/http"
	"strconv"
	"strings"

	"github.com/krogio/kordinate/internal/kordinate/upstream"
)

// handlers_docs.go is document review: upload, AI vetting, redaction, and the
// approve/reject decision.
//
// The access model here is the strictest in the product. A document is served
// REDACTED by default; the unredacted original requires a separate capability
// and a stated reason, and every reveal is written to the access log. That is
// the inversion of claire-admin, where any agent who could open a customer
// could see a full ID document with no record that they had.

// maxUploadBytes bounds a document upload. FICA documents are photos and scans;
// 20 MiB is generous for that and small enough to reject a mistake early.
const maxUploadBytes = 20 << 20

func (m *Module) documentQueue(w http.ResponseWriter, r *http.Request) {

	// The review queue is the set of cases sitting in document-bearing states —
	// the work, not every document ever uploaded.
	cases, err := m.st.ListCases(r.Context(), account(r), CaseQuery{
		States: []State{StateDocsSubmitted, StateVetting, StateInfoRequested},
		Limit:  200,
	})
	if err != nil {
		m.fail(w, r, err)
		return
	}
	// Flatten to document rows: the queue is a list of documents awaiting a
	// decision, not a list of cases the agent then has to drill into.
	items := make([]DocumentQueueItem, 0, len(cases))
	wantType := strings.TrimSpace(r.URL.Query().Get("doc_type"))
	wantStatus := strings.ToUpper(strings.TrimSpace(r.URL.Query().Get("status")))
	vetted := false
	for _, k := range cases {
		docs, derr := m.up.Customer.ListDocuments(r.Context(), k.MMGuid)
		if derr != nil {
			continue
		}
		for _, doc := range docs {
			if wantType != "" && doc.DocumentType != wantType {
				continue
			}
			st := strings.ToUpper(doc.DocumentStatus)
			if st == "" {
				st = upstream.DocStatusPending
			}
			if wantStatus != "" && st != wantStatus {
				continue
			}
			item := DocumentQueueItem{
				MMGuid: k.MMGuid, DisplayName: k.DisplayName, MSISDN: k.MSISDN, Doc: doc,
			}
			if v, verr := m.st.LatestVetting(r.Context(), account(r), doc.MediaID); verr == nil && v != nil {
				item.Vetting, vetted = v, true
			}
			items = append(items, item)
		}
	}

	data := map[string]any{"CanApprove": canEdit(r), "CanVet": canEdit(r)}
	data["Cases"] = cases
	data["Items"] = items
	data["Vetted"] = vetted
	data["DocTypes"] = docTypes()
	data["Filter"] = map[string]string{"Status": wantStatus, "DocType": wantType}
	data["Now"] = m.now()
	data["VetAvailable"] = m.vet.Available()
	m.d.Render(w, r, "kordinate_documents.html", data)
}

func (m *Module) documentView(w http.ResponseWriter, r *http.Request) {
	guid := strings.TrimSpace(r.URL.Query().Get("guid"))
	mediaID := strings.TrimSpace(r.URL.Query().Get("media_id"))
	if guid == "" || mediaID == "" {
		m.fail(w, r, errBadRequest("A customer and document are required."))
		return
	}

	cust, err := m.up.Customer.GetByGUID(r.Context(), guid)
	if err != nil {
		m.fail(w, r, err)
		return
	}
	docs, err := m.up.Customer.ListDocuments(r.Context(), guid)
	if err != nil {
		m.fail(w, r, err)
		return
	}

	doc, ok := findDoc(docs, mediaID)
	if !ok {
		m.fail(w, r, errNotFound("That document doesn't exist on this customer."))
		return
	}
	m.logAccess(r, guid, AccessViewDocument, mediaID, "")

	data := map[string]any{
		"CanApprove": canEdit(r),
		"CanVet":     canEdit(r),
		"CanRedact":  canEdit(r),
		// The one permission no role confers — a per-user, expiring, logged grant.
		"CanReveal": m.st.CanReveal(r.Context(), account(r), principal(r)),
	}
	data["Customer"] = cust
	data["Doc"] = doc
	data["Document"] = doc
	data["Advisory"] = Advisory
	data["VetAvailable"] = m.vet.Available()
	data["PIIKinds"] = piiKinds()
	data["Unredacted"] = r.URL.Query().Get("revealed") == "1"

	if v, verr := m.st.LatestVetting(r.Context(), account(r), mediaID); verr == nil && v != nil {
		data["Vetting"] = v
		data["SensitiveFields"] = SensitiveFields(v.Extracted)
	}

	// An existing redaction is what the editor loads; otherwise offer the
	// conventional boxes for this document type as a starting point to drag,
	// rather than making the agent draw every box from scratch.
	stored, _ := m.st.Redaction(r.Context(), account(r), mediaID)
	switch {
	case stored != nil:
		data["HasRedaction"] = true
		data["ProposedJSON"] = regionsJSON(stored.Regions)
	default:
		proposed := DefaultRegions(doc.DocumentType)
		data["Proposed"] = len(proposed) > 0
		data["ProposedJSON"] = regionsJSON(proposed)
	}

	// Redaction only applies to raster formats; say so plainly rather than
	// offering a control that can't work (see redact.go on why not PDFs).
	mediaType := doc.CustomerMediaType
	data["Redactable"] = mediaType == "" || Redactable(mediaType)
	if !Redactable(mediaType) && mediaType != "" {
		data["RedactionUnsupported"] = RedactionUnsupportedReason(mediaType)
		data["Redactable"] = false
	}
	m.d.Render(w, r, "kordinate_document.html", data)
}

// documentFile serves a document's bytes. It serves the REDACTED derivative
// unless the caller both holds CapRevealUnredacted and explicitly asks — and
// the reveal is logged with the reason.
func (m *Module) documentFile(w http.ResponseWriter, r *http.Request) {
	guid := strings.TrimSpace(r.URL.Query().Get("guid"))
	mediaID := strings.TrimSpace(r.URL.Query().Get("media_id"))
	if guid == "" || mediaID == "" {
		http.Error(w, "a customer and document are required", http.StatusBadRequest)
		return
	}

	data, mediaType, err := m.up.Customer.FetchDocument(r.Context(), guid, mediaID)
	if err != nil {
		http.Error(w, "document unavailable", http.StatusBadGateway)
		return
	}

	wantRaw := r.URL.Query().Get("raw") == "1"
	if wantRaw {
		if !m.st.CanReveal(r.Context(), account(r), principal(r)) {
			http.Error(w, "not permitted to view unredacted documents", http.StatusForbidden)
			return
		}
		reason := strings.TrimSpace(r.URL.Query().Get("reason"))
		if reason == "" {
			http.Error(w, "a reason is required to view an unredacted document", http.StatusBadRequest)
			return
		}
		m.logAccess(r, guid, AccessRevealUnredacted, mediaID, reason)
		m.audit(r.Context(), "kordinate.document.reveal", guid+"/"+mediaID, principal(r))
		serveBytes(w, data, mediaType)
		return
	}

	// Apply the stored redaction. If redaction fails for a redactable format,
	// serve NOTHING rather than falling back to the original — a failed
	// redaction that silently serves the raw document is the exact leak this
	// whole path exists to prevent.
	if regions := m.storedRegions(r, guid, mediaID); len(regions) > 0 {
		out, outType, rerr := Apply(data, mediaType, regions)
		if rerr != nil {
			http.Error(w, "the redacted copy could not be produced", http.StatusInternalServerError)
			return
		}
		serveBytes(w, out, outType)
		return
	}
	serveBytes(w, data, mediaType)
}

// storedRegions loads the redaction applied to a document, if any.
func (m *Module) storedRegions(r *http.Request, guid, mediaID string) []Region {
	red, err := m.st.Redaction(r.Context(), account(r), mediaID)
	if err != nil || red == nil {
		return nil
	}
	return red.Regions
}

func serveBytes(w http.ResponseWriter, b []byte, mediaType string) {
	if mediaType == "" {
		mediaType = "application/octet-stream"
	}
	w.Header().Set("Content-Type", mediaType)
	w.Header().Set("Content-Length", strconv.Itoa(len(b)))
	// Identity documents must not sit in a shared cache.
	w.Header().Set("Cache-Control", "private, no-store")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	_, _ = w.Write(b)
}

func (m *Module) documentUpload(w http.ResponseWriter, r *http.Request) {
	if !canEdit(r) {
		m.denyEdit(w, r)
		return
	}
	if err := r.ParseMultipartForm(maxUploadBytes); err != nil {
		m.fail(w, r, errBadRequest("That upload was too large or malformed (limit 20 MB)."))
		return
	}
	guid := strings.TrimSpace(r.FormValue("guid"))
	docType := strings.TrimSpace(r.FormValue("document_type"))
	if guid == "" || docType == "" {
		m.fail(w, r, errBadRequest("A customer and document type are required."))
		return
	}

	file, hdr, err := r.FormFile("file")
	if err != nil {
		m.fail(w, r, errBadRequest("Choose a file to upload."))
		return
	}
	defer file.Close()

	data, err := io.ReadAll(io.LimitReader(file, maxUploadBytes))
	if err != nil {
		m.fail(w, r, err)
		return
	}

	up := upstream.DocumentUpload{
		DocumentType:    docType,
		DocumentSubType: strings.TrimSpace(r.FormValue("document_sub_type")),
		DocumentNumber:  strings.TrimSpace(r.FormValue("document_number")),
		IssueDate:       strings.TrimSpace(r.FormValue("issue_date")),
		ExpiryDate:      strings.TrimSpace(r.FormValue("expiry_date")),
		IssuingCountry:  strings.TrimSpace(r.FormValue("issuing_country")),
		Filename:        hdr.Filename,
		MediaType:       hdr.Header.Get("Content-Type"),
		Data:            data,
		AgentID:         principal(r),
	}
	if _, err := m.up.Customer.CreateDocument(r.Context(), guid, up); err != nil {
		m.fail(w, r, err)
		return
	}
	m.audit(r.Context(), "kordinate.document.upload", guid+"/"+docType, principal(r))

	// A submission is what moves a case out of "waiting on the customer".
	if kase, cerr := m.st.OpenCaseFor(r.Context(), account(r), guid); cerr == nil && kase != nil {
		if kase.State == StateKYCPending || kase.State == StateInfoRequested {
			_ = m.st.MoveCase(r.Context(), account(r), kase.ID, StateDocsSubmitted, principal(r), "document uploaded: "+DocLabel(docType))
		}
	}
	redirectToCustomer(w, r, guid)
}

func (m *Module) documentApprove(w http.ResponseWriter, r *http.Request) {
	if !canEdit(r) {
		m.denyEdit(w, r)
		return
	}
	guid := strings.TrimSpace(r.FormValue("guid"))
	mediaID := strings.TrimSpace(r.FormValue("media_id"))
	status := strings.ToUpper(strings.TrimSpace(r.FormValue("status")))
	reason := strings.TrimSpace(r.FormValue("reason"))

	if guid == "" || mediaID == "" {
		m.fail(w, r, errBadRequest("A customer and document are required."))
		return
	}
	if status != upstream.DocStatusApproved && status != upstream.DocStatusRejected {
		m.fail(w, r, errBadRequest("A document is either approved or rejected."))
		return
	}
	// A rejection is what the customer is told to act on, so it must say why.
	if status == upstream.DocStatusRejected && reason == "" {
		m.fail(w, r, errBadRequest("A rejection needs a reason — the customer is told what to re-submit."))
		return
	}

	if err := m.up.Customer.SetDocumentStatus(r.Context(), guid, mediaID, status, reason); err != nil {
		m.fail(w, r, err)
		return
	}
	m.audit(r.Context(), "kordinate.document."+strings.ToLower(status), guid+"/"+mediaID, principal(r))

	if _, err := m.st.AddNote(r.Context(), account(r), Note{
		MMGuid: guid, Subject: "document", SubjectID: mediaID,
		Body:   "Document " + strings.ToLower(status) + ". " + reason,
		Author: principal(r),
	}); err != nil {
		m.fail(w, r, err)
		return
	}
	redirectToCustomer(w, r, guid)
}

// documentVet runs an AI vetting pass and stores the verdict. The result is
// advisory: it is recorded and displayed, and it never changes the document's
// approval status on its own.
func (m *Module) documentVet(w http.ResponseWriter, r *http.Request) {
	if !canEdit(r) {
		writeJSONError(w, http.StatusForbidden, "You don't have permission to vet documents.")
		return
	}
	guid := strings.TrimSpace(r.FormValue("guid"))
	mediaID := strings.TrimSpace(r.FormValue("media_id"))
	if guid == "" || mediaID == "" {
		writeJSONError(w, http.StatusBadRequest, "A customer and document are required.")
		return
	}

	cust, err := m.up.Customer.GetByGUID(r.Context(), guid)
	if err != nil {
		writeJSONError(w, http.StatusBadGateway, err.Error())
		return
	}
	docs, err := m.up.Customer.ListDocuments(r.Context(), guid)
	if err != nil {
		writeJSONError(w, http.StatusBadGateway, err.Error())
		return
	}
	doc, ok := findDoc(docs, mediaID)
	if !ok {
		writeJSONError(w, http.StatusNotFound, "That document doesn't exist on this customer.")
		return
	}

	// Vetting reads the ORIGINAL: the model must see the ID number to check it
	// against the record, and it cannot do that through a redaction. This is a
	// machine read, not a human one — no unredacted bytes reach the browser.
	data, mediaType, err := m.up.Customer.FetchDocument(r.Context(), guid, mediaID)
	if err != nil {
		writeJSONError(w, http.StatusBadGateway, "The document could not be retrieved.")
		return
	}

	v, err := m.vet.Vet(r.Context(), VetRequest{
		Customer: cust, Doc: doc, Data: data, MediaType: mediaType, Principal: principal(r),
	})
	if err != nil {
		writeJSONError(w, http.StatusInternalServerError, err.Error())
		return
	}
	if err := m.st.SaveVetting(r.Context(), account(r), guid, *v); err != nil {
		writeJSONError(w, http.StatusInternalServerError, err.Error())
		return
	}
	m.audit(r.Context(), "kordinate.document.vet", guid+"/"+mediaID, principal(r))

	writeJSON(w, http.StatusOK, map[string]any{
		"vetting":   v,
		"advisory":  Advisory,
		"sensitive": SensitiveFields(v.Extracted),
	})
}

// documentRedact stores a redaction and verifies it can actually be produced
// before recording it — a stored redaction that fails to render would leave the
// document unservable.
func (m *Module) documentRedact(w http.ResponseWriter, r *http.Request) {
	if !canEdit(r) {
		writeJSONError(w, http.StatusForbidden, "You don't have permission to redact documents.")
		return
	}

	var in struct {
		GUID    string   `json:"guid"`
		MediaID string   `json:"mediaId"`
		Regions []Region `json:"regions"`
	}
	if err := json.NewDecoder(io.LimitReader(r.Body, 1<<20)).Decode(&in); err != nil {
		writeJSONError(w, http.StatusBadRequest, "The redaction could not be read.")
		return
	}
	if in.GUID == "" || in.MediaID == "" || len(in.Regions) == 0 {
		writeJSONError(w, http.StatusBadRequest, "A customer, document and at least one region are required.")
		return
	}

	data, mediaType, err := m.up.Customer.FetchDocument(r.Context(), in.GUID, in.MediaID)
	if err != nil {
		writeJSONError(w, http.StatusBadGateway, "The document could not be retrieved.")
		return
	}
	if _, _, err := Apply(data, mediaType, in.Regions); err != nil {
		writeJSONError(w, http.StatusBadRequest, err.Error())
		return
	}

	auto := true
	for _, rg := range in.Regions {
		if !rg.Auto {
			auto = false
			break
		}
	}
	if err := m.st.SaveRedaction(r.Context(), account(r), in.GUID, Redaction{
		MediaID: in.MediaID, Regions: in.Regions, Auto: auto, AppliedBy: principal(r),
	}); err != nil {
		writeJSONError(w, http.StatusInternalServerError, err.Error())
		return
	}
	m.audit(r.Context(), "kordinate.document.redact", in.GUID+"/"+in.MediaID, principal(r))
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "regions": len(in.Regions)})
}

// documentReveal records an intent to view an unredacted document and returns
// the URL that serves it. Separating the record from the fetch means the reason
// is captured even if the agent abandons the view.
func (m *Module) documentReveal(w http.ResponseWriter, r *http.Request) {
	if !m.st.CanReveal(r.Context(), account(r), principal(r)) {
		m.deny(w, r, "You don't have permission to view unredacted documents. "+
			"An administrator can grant it on the Access log screen.")
		return
	}
	guid := strings.TrimSpace(r.FormValue("guid"))
	mediaID := strings.TrimSpace(r.FormValue("media_id"))
	reason := strings.TrimSpace(r.FormValue("reason"))
	if guid == "" || mediaID == "" || reason == "" {
		m.fail(w, r, errBadRequest("Revealing an unredacted document requires a stated reason."))
		return
	}
	m.logAccess(r, guid, AccessRevealUnredacted, mediaID, reason)
	m.audit(r.Context(), "kordinate.document.reveal_request", guid+"/"+mediaID, principal(r))

	http.Redirect(w, r, "/kordinate/documents/view?guid="+guid+"&media_id="+mediaID+"&revealed=1", http.StatusSeeOther)
}

func findDoc(docs []upstream.Document, mediaID string) (upstream.Document, bool) {
	for _, d := range docs {
		if d.MediaID == mediaID {
			return d, true
		}
	}
	return upstream.Document{}, false
}
