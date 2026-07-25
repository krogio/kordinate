package upstream

import (
	"context"
	"fmt"
	"net/http"
	"strconv"
	"time"
)

type liveClaire struct{ c *client }

func (s liveClaire) GetCustomerByMSISDN(ctx context.Context, msisdn, mmGUID string) (*ClaireCustomer, error) {
	// Claire indexes by MSISDN and can return several records for one number
	// (SIM reuse across customers), so the GUID picks the right one.
	var out struct {
		Items []struct {
			CustomerID         any    `json:"customer-id"`
			MMGlobalCustomerID string `json:"mm-global-customer-id"`
			LimitID            any    `json:"limit-id"`
			IncomeID           any    `json:"income-id"`
			IncomeSourceType   string `json:"income-source-type"`
		} `json:"items"`
	}
	if err := s.c.do(ctx, http.MethodGet, "/customers/"+q(map[string]string{"msisdn": msisdn}), nil, &out); err != nil {
		return nil, fmt.Errorf("claire customer by msisdn: %w", err)
	}

	for _, item := range out.Items {
		if item.MMGlobalCustomerID != mmGUID {
			continue
		}
		return &ClaireCustomer{
			CustomerID:       scalarString(item.CustomerID),
			LimitID:          scalarString(item.LimitID),
			IncomeID:         scalarString(item.IncomeID),
			IncomeSourceType: item.IncomeSourceType,
		}, nil
	}
	return nil, &Error{Service: s.c.name, Status: http.StatusNotFound, Message: "no claire customer for that msisdn and guid"}
}

func (s liveClaire) MonthlyLimit(ctx context.Context, limitID string) (*MonthlyLimit, error) {
	if limitID == "" {
		return nil, &Error{Service: s.c.name, Message: "limit id required"}
	}
	var out MonthlyLimit
	if err := s.c.do(ctx, http.MethodGet, "/customer-limits/"+limitID, nil, &out); err != nil {
		return nil, fmt.Errorf("monthly limit %s: %w", limitID, err)
	}
	return &out, nil
}

func (s liveClaire) MonthlyLimitBalance(ctx context.Context, mmGUID string) (*LimitBalance, error) {
	// This endpoint historically returned a bare number rather than an object,
	// so decode into a RawMessage-friendly shape and cope with both.
	var raw any
	if err := s.c.do(ctx, http.MethodGet, "/customers/"+mmGUID+"/monthly-limit", nil, &raw); err != nil {
		return nil, fmt.Errorf("monthly limit balance %s: %w", mmGUID, err)
	}

	switch v := raw.(type) {
	case float64:
		return &LimitBalance{Remaining: v}, nil
	case map[string]any:
		bal := &LimitBalance{}
		if f, ok := v["monthlyLimit"].(float64); ok {
			bal.MonthlyLimit = f
		}
		if f, ok := v["used"].(float64); ok {
			bal.Used = f
		}
		if f, ok := v["remaining"].(float64); ok {
			bal.Remaining = f
		}
		return bal, nil
	default:
		return nil, &Error{Service: s.c.name, Message: "unexpected monthly-limit payload"}
	}
}

func (s liveClaire) IncomeRanges(ctx context.Context) ([]IncomeRange, error) {
	var out struct {
		Items []IncomeRange `json:"items"`
	}
	if err := s.c.do(ctx, http.MethodGet, "/customer-incomes", nil, &out); err != nil {
		return nil, fmt.Errorf("income ranges: %w", err)
	}
	return out.Items, nil
}

func (s liveClaire) IncomeSources(ctx context.Context) ([]IncomeSource, error) {
	var out struct {
		Items []IncomeSource `json:"items"`
	}
	if err := s.c.do(ctx, http.MethodGet, "/customer-income-source", nil, &out); err != nil {
		return nil, fmt.Errorf("income sources: %w", err)
	}
	return out.Items, nil
}

func (s liveClaire) RiskMatrix(ctx context.Context, mmGUID string) (*RiskMatrix, error) {
	path := "/customers/" + mmGUID + "/onboard-risk-score" + q(map[string]string{
		"page-number": "1",
		"page-count":  "1",
	})

	var out struct {
		Items []struct {
			Score       int    `json:"score"`
			Description string `json:"description"`
			SetBy       string `json:"set-by"`
			SetAt       string `json:"set-at"`
		} `json:"items"`
	}
	if err := s.c.do(ctx, http.MethodGet, path, nil, &out); err != nil {
		return nil, fmt.Errorf("risk matrix %s: %w", mmGUID, err)
	}
	if len(out.Items) == 0 {
		return nil, &Error{Service: s.c.name, Status: http.StatusNotFound, Message: "no risk score recorded"}
	}

	// Claire returns risk as component rows; the rating is their sum.
	m := &RiskMatrix{
		Description: out.Items[0].Description,
		SetBy:       out.Items[0].SetBy,
		SetAt:       out.Items[0].SetAt,
	}
	for _, item := range out.Items {
		m.Score += item.Score
	}
	return m, nil
}

func (s liveClaire) Orders(ctx context.Context, mmGUID string, from, to time.Time) ([]Order, error) {
	// Claire cannot filter payouts by date, so a page is pulled and filtered
	// here — the same thing claire-admin did client-side.
	path := "/customers/" + mmGUID + "/payouts/" + q(map[string]string{
		"page-number": "1",
		"page-count":  "50",
	})

	var out struct {
		Items []struct {
			OrderID     any     `json:"order-id"`
			PayoutID    any     `json:"payout-id"`
			Amount      float64 `json:"amount"`
			FeeAmount   float64 `json:"fee-amount"`
			Reference   string  `json:"reference-number"`
			Status      string  `json:"status"`
			TimeCreated string  `json:"time-created"`
			TimeUpdated string  `json:"time-modified"`
		} `json:"items"`
	}
	if err := s.c.do(ctx, http.MethodGet, path, nil, &out); err != nil {
		return nil, fmt.Errorf("claire orders %s: %w", mmGUID, err)
	}

	orders := make([]Order, 0, len(out.Items))
	for _, item := range out.Items {
		created := parseClaireTime(item.TimeCreated)
		if !within(created, from, to) {
			continue
		}
		id := scalarString(item.OrderID)
		if id == "" {
			id = scalarString(item.PayoutID)
		}
		orders = append(orders, Order{
			OrderID:              id,
			MMGlobalCustomerID:   mmGUID,
			Product:              ProductRemittance,
			Amount:               item.Amount,
			FeeAmount:            item.FeeAmount,
			OrderReferenceNumber: item.Reference,
			OrderStatus:          OrderStatus(item.Status),
			TimeCreated:          item.TimeCreated,
			TimeUpdated:          item.TimeUpdated,
			Source:               "claire",
		})
	}
	return orders, nil
}

// parseClaireTime handles the formats Claire has emitted for time-created.
// An unparseable value yields the zero time, which within() treats as "keep".
func parseClaireTime(s string) time.Time {
	if s == "" {
		return time.Time{}
	}
	layouts := []string{
		time.RFC3339,
		"2006-01-02 15:04:05",
		"2006-01-02T15:04:05",
		"02/01/2006 15:04:05",
		"02-01-2006 15:04:05",
	}
	for _, l := range layouts {
		if t, err := time.Parse(l, s); err == nil {
			return t
		}
	}
	return time.Time{}
}

// within reports whether t falls in [from, to]. Zero bounds are open, and a
// zero t is kept — dropping records because a date failed to parse would hide
// transactions from an agent, which is worse than showing an extra one.
func within(t, from, to time.Time) bool {
	if t.IsZero() {
		return true
	}
	if !from.IsZero() && t.Before(from) {
		return false
	}
	if !to.IsZero() && t.After(to) {
		return false
	}
	return true
}

// scalarString normalises ids that arrive as either a JSON number or a string.
func scalarString(v any) string {
	switch t := v.(type) {
	case string:
		return t
	case float64:
		return strconv.FormatFloat(t, 'f', -1, 64)
	case nil:
		return ""
	default:
		return fmt.Sprint(t)
	}
}
