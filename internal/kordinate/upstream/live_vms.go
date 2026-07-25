package upstream

import (
	"context"
	"fmt"
	"net/http"
)

type liveVMS struct{ c *client }

// VMS names the recipient in split first/last fields; Voucher carries a single
// display name, so voucherWire does the translation both ways.
type voucherWire struct {
	Code              string  `json:"voucherCode"`
	Amount            float64 `json:"amount"`
	Currency          string  `json:"currency"`
	Status            string  `json:"status"`
	Product           string  `json:"product"`
	ReceiverFirstName string  `json:"receiverFirstName"`
	ReceiverLastName  string  `json:"receiverLastName"`
	ReceiverMSISDN    string  `json:"receiverMsisdn"`
	ReceiverEmail     string  `json:"receiverEmail"`
	CreatedAt         string  `json:"createdAt"`
	RedeemedAt        string  `json:"redeemedAt"`
	ExpiresAt         string  `json:"expiresAt"`
}

func (v voucherWire) toVoucher() Voucher {
	name := v.ReceiverFirstName
	if v.ReceiverLastName != "" {
		if name != "" {
			name += " "
		}
		name += v.ReceiverLastName
	}

	out := Voucher{
		Code:      v.Code,
		Amount:    v.Amount,
		Currency:  v.Currency,
		Status:    v.Status,
		Product:   v.Product,
		CreatedAt: parseClaireTime(v.CreatedAt),
		Recipient: VoucherRecipient{
			Name:   name,
			MSISDN: v.ReceiverMSISDN,
			Email:  v.ReceiverEmail,
		},
	}
	if t := parseClaireTime(v.RedeemedAt); !t.IsZero() {
		out.RedeemedAt = &t
	}
	if t := parseClaireTime(v.ExpiresAt); !t.IsZero() {
		out.ExpiresAt = &t
	}
	return out
}

func (s liveVMS) Voucher(ctx context.Context, code string) (*Voucher, error) {
	var out voucherWire
	if err := s.c.do(ctx, http.MethodGet, "/vouchers/"+code, nil, &out); err != nil {
		return nil, fmt.Errorf("get voucher %s: %w", code, err)
	}
	if out.Code == "" {
		out.Code = code
	}
	v := out.toVoucher()
	return &v, nil
}

func (s liveVMS) VouchersForCustomer(ctx context.Context, mmGUID string) ([]Voucher, error) {
	path := "/vouchers" + q(map[string]string{"mmGlobalCustomerId": mmGUID})

	var out struct {
		Vouchers []voucherWire `json:"vouchers"`
		Items    []voucherWire `json:"items"`
	}
	if err := s.c.do(ctx, http.MethodGet, path, nil, &out); err != nil {
		return nil, fmt.Errorf("vouchers for customer %s: %w", mmGUID, err)
	}
	wire := out.Vouchers
	if wire == nil {
		wire = out.Items
	}

	vouchers := make([]Voucher, 0, len(wire))
	for _, w := range wire {
		vouchers = append(vouchers, w.toVoucher())
	}
	return vouchers, nil
}

func (s liveVMS) Cancel(ctx context.Context, code, reason, agentID string) error {
	// The institution fields are required by VMS and are constants for us — the
	// acting operator goes in institutionBranchEmployee, which is what VMS
	// records against the cancellation.
	body := map[string]any{
		"action":                    "CANCEL",
		"institution":               "Mama Money",
		"institutionBranch":         "Mama Money",
		"institutionBranchEmployee": agentID,
	}
	if reason != "" {
		body["reason"] = reason
	}
	if err := s.c.do(ctx, http.MethodPut, "/vouchers/"+code+"/status", body, nil); err != nil {
		return fmt.Errorf("cancel voucher %s: %w", code, err)
	}
	return nil
}

func (s liveVMS) UpdateRecipient(ctx context.Context, code string, r VoucherRecipient) error {
	first, last := splitName(r.Name)
	body := map[string]any{
		"receiverFirstName": first,
		"receiverLastName":  last,
	}
	if r.MSISDN != "" {
		body["receiverMsisdn"] = r.MSISDN
	}
	if r.Email != "" {
		body["receiverEmail"] = r.Email
	}
	if err := s.c.do(ctx, http.MethodPut, "/vouchers/"+code+"/recipient", body, nil); err != nil {
		return fmt.Errorf("update voucher recipient %s: %w", code, err)
	}
	return nil
}

func (s liveVMS) Create(ctx context.Context, req VoucherCreate) ([]Voucher, error) {
	quantity := req.Quantity
	if quantity < 1 {
		quantity = 1
	}
	body := map[string]any{
		"amount":   req.Amount,
		"currency": req.Currency,
		"quantity": quantity,
	}
	if req.Product != "" {
		body["product"] = req.Product
	}
	if req.AgentID != "" {
		body["institutionBranchEmployee"] = req.AgentID
	}

	var out struct {
		Vouchers []voucherWire `json:"vouchers"`
		Items    []voucherWire `json:"items"`
	}
	if err := s.c.do(ctx, http.MethodPost, "/vouchers", body, &out); err != nil {
		return nil, fmt.Errorf("create vouchers: %w", err)
	}
	wire := out.Vouchers
	if wire == nil {
		wire = out.Items
	}

	vouchers := make([]Voucher, 0, len(wire))
	for _, w := range wire {
		vouchers = append(vouchers, w.toVoucher())
	}
	return vouchers, nil
}

// splitName splits a display name on the first space. VMS insists on two
// fields; the whole name becomes the first name when there is no surname.
func splitName(name string) (first, last string) {
	for i := 0; i < len(name); i++ {
		if name[i] == ' ' {
			return name[:i], name[i+1:]
		}
	}
	return name, ""
}
