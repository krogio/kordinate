package upstream

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"mime/multipart"
	"net/http"
	"strconv"
)

type liveCustomer struct{ c *client }

// customer-service speaks kebab-case throughout, including on writes, so the
// request payloads below carry their own tags rather than reusing the exported
// DTOs (CustomerCreate's tags describe the caller-facing shape, not the wire).

func (s liveCustomer) Search(ctx context.Context, sq CustomerSearchQuery) ([]Customer, int, error) {
	pairs := map[string]string{
		"msisdn":               sq.MSISDN,
		"mmGlobalCustomerId":   sq.MMGlobalCustomerID,
		"identificationNumber": sq.IDNumber,
		"firstName":            sq.FirstName,
		"lastName":             sq.LastName,
		"emailAddress":         sq.EmailAddress,
		"customerStatus":       string(sq.Status),
	}
	if sq.Page > 0 {
		pairs["page"] = strconv.Itoa(sq.Page)
	}
	if sq.PerPage > 0 {
		pairs["perPage"] = strconv.Itoa(sq.PerPage)
	}

	// The service has returned both a bare array and a paged envelope across
	// versions, so decode loosely and cope with either.
	var raw json.RawMessage
	if err := s.c.do(ctx, http.MethodGet, "/customers"+q(pairs), nil, &raw); err != nil {
		return nil, 0, fmt.Errorf("search customers: %w", err)
	}

	var envelope struct {
		Items      []Customer `json:"items"`
		Customers  []Customer `json:"customers"`
		TotalCount int        `json:"total-count"`
		Total      int        `json:"total"`
	}
	if err := json.Unmarshal(raw, &envelope); err == nil {
		items := envelope.Items
		if items == nil {
			items = envelope.Customers
		}
		if items != nil {
			total := envelope.TotalCount
			if total == 0 {
				total = envelope.Total
			}
			if total == 0 {
				total = len(items)
			}
			return items, total, nil
		}
	}

	var list []Customer
	if err := json.Unmarshal(raw, &list); err != nil {
		return nil, 0, &Error{Service: s.c.name, Err: fmt.Errorf("decode customer search: %w", err)}
	}
	return list, len(list), nil
}

func (s liveCustomer) GetByGUID(ctx context.Context, guid string) (*Customer, error) {
	// Two shapes of lookup exist: /customer/{id} takes the numeric customer id,
	// while the mmGlobalCustomerId filter is what resolves a GUID.
	var out Customer
	path := "/customer" + q(map[string]string{
		"loadRelations":      "true",
		"mmGlobalCustomerId": guid,
	})
	if err := s.c.do(ctx, http.MethodGet, path, nil, &out); err != nil {
		return nil, fmt.Errorf("get customer %s: %w", guid, err)
	}
	return &out, nil
}

func (s liveCustomer) GetByMSISDN(ctx context.Context, msisdn string) (*Customer, error) {
	var out Customer
	path := "/customer" + q(map[string]string{
		"loadRelations": "true",
		"msisdn":        msisdn,
	})
	if err := s.c.do(ctx, http.MethodGet, path, nil, &out); err != nil {
		return nil, fmt.Errorf("get customer by msisdn: %w", err)
	}
	return &out, nil
}

func (s liveCustomer) Create(ctx context.Context, c CustomerCreate) (*Customer, error) {
	c.Defaults()
	body := map[string]any{
		"msisdn":                 c.MSISDN,
		"first-name":             c.FirstName,
		"last-name":              c.LastName,
		"date-of-birth":          c.DateOfBirth,
		"preferred-language":     c.PreferredLanguage,
		"inbound-channel":        c.InboundChannel,
		"identification-id-type": c.IDType,
	}
	optional := map[string]string{
		"gender":                c.Gender,
		"email-address":         c.EmailAddress,
		"street-address":        c.StreetAddress,
		"street-suburb":         c.StreetSuburb,
		"street-city":           c.StreetCity,
		"street-province":       c.StreetProvince,
		"postal-code":           c.PostalCode,
		"pin":                   c.PIN,
		"agent-id":              c.AgentID,
		"identification-number": c.IDNumber,
	}
	for k, v := range optional {
		if v != "" {
			body[k] = v
		}
	}

	var out Customer
	if err := s.c.do(ctx, http.MethodPost, "/customer", body, &out); err != nil {
		return nil, fmt.Errorf("create customer: %w", err)
	}
	return &out, nil
}

func (s liveCustomer) Update(ctx context.Context, guid string, c CustomerUpdate) (*Customer, error) {
	body := map[string]any{}
	set := func(key string, v *string) {
		if v != nil {
			body[key] = *v
		}
	}
	set("first-name", c.FirstName)
	set("last-name", c.LastName)
	set("email-address", c.EmailAddress)
	set("date-of-birth", c.DateOfBirth)
	set("gender", c.Gender)
	set("street-address", c.StreetAddress)
	set("street-suburb", c.StreetSuburb)
	set("street-city", c.StreetCity)
	set("street-province", c.StreetProvince)
	set("postal-code", c.PostalCode)

	if len(body) == 0 {
		return s.GetByGUID(ctx, guid)
	}

	var out Customer
	if err := s.c.do(ctx, http.MethodPut, "/customer/"+guid, body, &out); err != nil {
		return nil, fmt.Errorf("update customer %s: %w", guid, err)
	}
	return &out, nil
}

func (s liveCustomer) UpdateStatus(ctx context.Context, guid string, status CustomerStatus, reason string) error {
	body := map[string]any{"customer-status": string(status)}
	if reason != "" {
		body["status-change-reason"] = reason
	}
	if err := s.c.do(ctx, http.MethodPut, "/customer/"+guid+"/status", body, nil); err != nil {
		return fmt.Errorf("update customer status %s: %w", guid, err)
	}
	return nil
}

func (s liveCustomer) Deprecate(ctx context.Context, guid, reason string) error {
	body := map[string]any{"deprecation-reason": reason}

	// The service answers 200 with a result code rather than a 4xx when it
	// declines, so the code must be inspected or a refusal reads as success.
	var out struct {
		DeprecationResult string `json:"deprecation-result"`
	}
	if err := s.c.do(ctx, http.MethodPut, "/customer/"+guid+"/deprecate", body, &out); err != nil {
		return fmt.Errorf("deprecate customer %s: %w", guid, err)
	}
	if out.DeprecationResult != "" && out.DeprecationResult != "CUSTOMER_DEPRECATED" {
		return &Error{Service: s.c.name, Message: "deprecation refused: " + out.DeprecationResult}
	}
	return nil
}

func (s liveCustomer) Reinstate(ctx context.Context, guid, reason string) error {
	// Reinstatement reuses the deprecation-reason field — the upstream models
	// both transitions with one payload.
	body := map[string]any{"deprecation-reason": reason}

	var out struct {
		DeprecationResult string `json:"deprecation-result"`
	}
	if err := s.c.do(ctx, http.MethodPut, "/customer/"+guid+"/reinstate", body, &out); err != nil {
		return fmt.Errorf("reinstate customer %s: %w", guid, err)
	}
	if out.DeprecationResult != "" && out.DeprecationResult != "CUSTOMER_REINSTATED" {
		return &Error{Service: s.c.name, Message: "reinstatement refused: " + out.DeprecationResult}
	}
	return nil
}

func (s liveCustomer) UpdateIncome(ctx context.Context, guid string, incomeID, sourceType string) error {
	body := map[string]any{}
	if incomeID != "" {
		body["income-salary-range"] = incomeID
	}
	if sourceType != "" {
		body["income-source-type"] = sourceType
	}
	if len(body) == 0 {
		return &Error{Service: s.c.name, Message: "income update needs a range or a source type"}
	}
	if err := s.c.do(ctx, http.MethodPut, "/customer/"+guid+"/income", body, nil); err != nil {
		return fmt.Errorf("update customer income %s: %w", guid, err)
	}
	return nil
}

func (s liveCustomer) ListDocuments(ctx context.Context, guid string) ([]Document, error) {
	// There is no document-list endpoint; documents arrive as a relation on the
	// customer, which is why loadRelations exists.
	c, err := s.GetByGUID(ctx, guid)
	if err != nil {
		return nil, err
	}
	return c.Documents, nil
}

func (s liveCustomer) CreateDocument(ctx context.Context, guid string, d DocumentUpload) (*Document, error) {
	meta := map[string]any{
		"customer-media-type":      d.DocumentType,
		"document-type":            d.DocumentType,
		"document-inbound-channel": "KORDINATE",
		// The service rejects the upload without a name but never uses it, so a
		// synthetic one keeps callers from having to invent it.
		"document-name": d.Filename,
	}
	if d.DocumentSubType != "" {
		meta["customer-media-sub-type"] = d.DocumentSubType
		meta["document-sub-type"] = d.DocumentSubType
	}
	optional := map[string]string{
		"document-number":     d.DocumentNumber,
		"issue-date":          d.IssueDate,
		"expiry-date":         d.ExpiryDate,
		"issuing-country":     d.IssuingCountry,
		"processing-agent-id": d.AgentID,
	}
	for k, v := range optional {
		if v != "" {
			meta[k] = v
		}
	}
	if meta["document-name"] == "" {
		meta["document-name"] = d.DocumentType + ".jpg"
	}

	metaJSON, err := json.Marshal(meta)
	if err != nil {
		return nil, &Error{Service: s.c.name, Err: fmt.Errorf("encode mediaDTO: %w", err)}
	}

	var buf bytes.Buffer
	mw := multipart.NewWriter(&buf)
	fw, err := mw.CreateFormFile("file", d.Filename)
	if err != nil {
		return nil, &Error{Service: s.c.name, Err: fmt.Errorf("build upload: %w", err)}
	}
	if _, err := fw.Write(d.Data); err != nil {
		return nil, &Error{Service: s.c.name, Err: fmt.Errorf("build upload: %w", err)}
	}
	if err := mw.WriteField("mediaDTO", string(metaJSON)); err != nil {
		return nil, &Error{Service: s.c.name, Err: fmt.Errorf("build upload: %w", err)}
	}
	if err := mw.Close(); err != nil {
		return nil, &Error{Service: s.c.name, Err: fmt.Errorf("build upload: %w", err)}
	}

	var out Document
	err = s.c.doMultipart(ctx, http.MethodPost, "/customer/"+guid+"/media", mw.FormDataContentType(), buf.Bytes(), &out)
	if err != nil {
		return nil, fmt.Errorf("upload document for %s: %w", guid, err)
	}
	return &out, nil
}

func (s liveCustomer) SetDocumentStatus(ctx context.Context, guid, mediaID, status, reason string) error {
	body := map[string]any{"document-status": status}
	if reason != "" {
		body["document-approval-code"] = reason
	}
	path := "/customer/" + guid + "/media/" + mediaID + "/approval"
	if err := s.c.do(ctx, http.MethodPost, path, body, nil); err != nil {
		return fmt.Errorf("set document status %s/%s: %w", guid, mediaID, err)
	}
	return nil
}

func (s liveCustomer) FetchDocument(ctx context.Context, guid, mediaID string) ([]byte, string, error) {
	raw, ct, err := s.c.doRaw(ctx, http.MethodGet, "/customer/"+guid+"/media/"+mediaID)
	if err != nil {
		return nil, "", fmt.Errorf("fetch document %s/%s: %w", guid, mediaID, err)
	}
	return raw, ct, nil
}

func (s liveCustomer) BulkSuspend(ctx context.Context, guids []string, reason string) (map[string]error, error) {
	body := map[string]any{"customer-list": guids}
	if reason != "" {
		body["suspension-reason"] = reason
	}

	// Per-customer outcomes come back as a map of guid -> bool; a false there is
	// a partial failure, not a call failure, so it belongs in the result map.
	var out struct {
		Results map[string]bool `json:"suspension-results"`
	}
	if err := s.c.do(ctx, http.MethodPost, "/customers/suspend", body, &out); err != nil {
		return nil, fmt.Errorf("bulk suspend: %w", err)
	}
	if out.Results == nil {
		return nil, &Error{Service: s.c.name, Message: "bulk suspend returned no results"}
	}

	results := make(map[string]error, len(out.Results))
	for guid, ok := range out.Results {
		if ok {
			results[guid] = nil
			continue
		}
		results[guid] = &Error{Service: s.c.name, Message: "suspension failed"}
	}
	return results, nil
}
