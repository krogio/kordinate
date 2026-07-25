package upstream

import (
	"context"
	"fmt"
	"net/http"
	"regexp"
	"strings"
	"sync"
	"time"
)

type liveUML struct{ c *client }

var umlVersionSuffix = regexp.MustCompile(`/v\d+$`)

// newLiveUML strips any trailing /vN from UML_URL. UML versions per-endpoint
// rather than per-deployment, so the version in config is only a default and
// each call re-attaches the one it actually needs.
func newLiveUML(cfg Config) liveUML {
	base := umlVersionSuffix.ReplaceAllString(strings.TrimRight(cfg.UMLURL, "/"), "")
	return liveUML{c: newClient("uml", base, cfg.Timeout)}
}

// path builds a versioned UML URL. Callers pass "v1"/"v2" explicitly so the
// version is visible at the call site rather than buried in config.
func (s liveUML) path(version, p string) string {
	if p != "" && p[0] == '?' {
		return "/" + version + p
	}
	if p != "" && !strings.HasPrefix(p, "/") {
		p = "/" + p
	}
	return "/" + version + p
}

func (s liveUML) WalletBalance(ctx context.Context, mmGUID string) (float64, error) {
	var out struct {
		CurrentBalance float64 `json:"currentBalance"`
	}
	if err := s.c.do(ctx, http.MethodGet, s.path("v1", "/wallet/"+mmGUID+"/balance"), nil, &out); err != nil {
		return 0, fmt.Errorf("wallet balance %s: %w", mmGUID, err)
	}
	return out.CurrentBalance, nil
}

func (s liveUML) CloeBalance(ctx context.Context, mmGUID string) (*CloeBalance, error) {
	var out CloeBalance
	if err := s.c.do(ctx, http.MethodGet, s.path("v1", "/cloe/customer/"+mmGUID+"/balance"), nil, &out); err != nil {
		return nil, fmt.Errorf("cloe balance %s: %w", mmGUID, err)
	}
	return &out, nil
}

func (s liveUML) CardBalance(ctx context.Context, msisdn string, transactionAmount float64) (*CardBalance, error) {
	body := map[string]any{
		"transactionAmount": transactionAmount,
		"msisdn":            msisdn,
	}
	var out CardBalance
	if err := s.c.do(ctx, http.MethodPost, s.path("v1", "/banking/balance/check"), body, &out); err != nil {
		return nil, fmt.Errorf("card balance %s: %w", msisdn, err)
	}
	return &out, nil
}

// Balances fans out across the three balance sources. One slow or broken
// product must not hide the other two — an agent needs whatever is available —
// so failures land in Balances.Errors and the corresponding pointer stays nil.
func (s liveUML) Balances(ctx context.Context, mmGUID, msisdn string) (*Balances, error) {
	// Card balance is only meaningful for a live banking customer, and the
	// status call gates it exactly as claire-admin's cardDetails session did.
	status, statusErr := s.BankingStatus(ctx, mmGUID)

	var (
		mu  sync.Mutex
		wg  sync.WaitGroup
		out = &Balances{Errors: map[string]string{}}
	)
	fail := func(product string, err error) {
		mu.Lock()
		defer mu.Unlock()
		out.Errors[product] = err.Error()
	}

	wg.Add(2)
	go func() {
		defer wg.Done()
		b, err := s.WalletBalance(ctx, mmGUID)
		if err != nil {
			fail("wallet", err)
			return
		}
		mu.Lock()
		out.Wallet = &b
		mu.Unlock()
	}()
	go func() {
		defer wg.Done()
		cloe, err := s.CloeBalance(ctx, mmGUID)
		if err != nil {
			fail("usdc", err)
			return
		}
		mu.Lock()
		out.USDC = &cloe.USDCBalance
		out.ZAR = &cloe.ZARBalance
		mu.Unlock()
	}()

	switch {
	case statusErr != nil:
		// Without a status we can't tell "no card" from "card we didn't check",
		// so record it rather than silently reporting no card balance.
		fail("card", fmt.Errorf("banking status unavailable: %w", statusErr))
	case status.IsActive() && msisdn != "":
		wg.Add(1)
		go func() {
			defer wg.Done()
			card, err := s.CardBalance(ctx, msisdn, 0)
			if err != nil {
				fail("card", err)
				return
			}
			mu.Lock()
			out.Card = &card.AvailableBalance
			mu.Unlock()
		}()
	}

	wg.Wait()
	if len(out.Errors) == 0 {
		out.Errors = nil
	}
	return out, nil
}

func (s liveUML) BankingStatus(ctx context.Context, mmGUID string) (*BankingStatus, error) {
	var out BankingStatus
	if err := s.c.do(ctx, http.MethodGet, s.path("v2", "/banking/customer/"+mmGUID+"/status"), nil, &out); err != nil {
		return nil, fmt.Errorf("banking status %s: %w", mmGUID, err)
	}
	return &out, nil
}

func (s liveUML) BankingEligibility(ctx context.Context, mmGUID string) (*BankingEligibility, error) {
	var out BankingEligibility
	if err := s.c.do(ctx, http.MethodGet, s.path("v1", "/banking/customer/"+mmGUID+"/eligibility"), nil, &out); err != nil {
		return nil, fmt.Errorf("banking eligibility %s: %w", mmGUID, err)
	}
	return &out, nil
}

// bankingCustomerEnvelope is the {"customer": {...}} wrapper both customer
// lookups return.
type bankingCustomerEnvelope struct {
	Customer *BankingCustomer `json:"customer"`
}

func (s liveUML) BankingCustomer(ctx context.Context, mmGUID string) (*BankingCustomer, error) {
	var out bankingCustomerEnvelope
	if err := s.c.do(ctx, http.MethodGet, s.path("v1", "/banking/mamacustomer/"+mmGUID), nil, &out); err != nil {
		return nil, fmt.Errorf("banking customer %s: %w", mmGUID, err)
	}
	if out.Customer == nil {
		return nil, &Error{Service: s.c.name, Status: http.StatusNotFound, Message: "no banking customer"}
	}
	return out.Customer, nil
}

func (s liveUML) MamaBankingCustomer(ctx context.Context, mmGUID string) (*BankingCustomer, error) {
	var out bankingCustomerEnvelope
	if err := s.c.do(ctx, http.MethodGet, s.path("v1", "/banking/customer/"+mmGUID), nil, &out); err != nil {
		return nil, fmt.Errorf("mama banking customer %s: %w", mmGUID, err)
	}
	if out.Customer == nil {
		return nil, &Error{Service: s.c.name, Status: http.StatusNotFound, Message: "no banking customer"}
	}
	return out.Customer, nil
}

func (s liveUML) OptInForBanking(ctx context.Context, mmGUID string) error {
	body := map[string]any{"mmGlobalCustomerId": mmGUID}

	// Opt-in answers 200 with success=false when it declines, so the flag has to
	// be read or a refusal looks like an enrolment.
	var out struct {
		Success  bool             `json:"success"`
		Customer *BankingCustomer `json:"customer"`
	}
	if err := s.c.do(ctx, http.MethodPost, s.path("v1", "/banking/opt-in"), body, &out); err != nil {
		return fmt.Errorf("banking opt-in %s: %w", mmGUID, err)
	}
	if !out.Success || out.Customer == nil {
		return &Error{Service: s.c.name, Message: "banking opt-in declined"}
	}
	return nil
}

func (s liveUML) BlockCard(ctx context.Context, mmGUID, reason string) error {
	body := map[string]any{"mama-card-block-reason": reason}
	if err := s.c.do(ctx, http.MethodPut, s.path("v1", "/banking/customer/"+mmGUID+"/card/block"), body, nil); err != nil {
		return fmt.Errorf("block card %s: %w", mmGUID, err)
	}
	return nil
}

func (s liveUML) UnblockCard(ctx context.Context, mmGUID, reason string) error {
	body := map[string]any{"mama-card-unblock-reason": reason}
	if err := s.c.do(ctx, http.MethodPut, s.path("v1", "/banking/customer/"+mmGUID+"/card/unblock"), body, nil); err != nil {
		return fmt.Errorf("unblock card %s: %w", mmGUID, err)
	}
	return nil
}

func (s liveUML) ResetCardPIN(ctx context.Context, mmGUID string) error {
	if err := s.c.do(ctx, http.MethodPut, s.path("v1", "/banking/customer/"+mmGUID+"/card/pin/reset"), nil, nil); err != nil {
		return fmt.Errorf("reset card pin %s: %w", mmGUID, err)
	}
	return nil
}

func (s liveUML) ReallocateCard(ctx context.Context, mmGUID, cardSequenceNumber string) error {
	body := map[string]any{"cardSequenceNumber": cardSequenceNumber}
	if err := s.c.do(ctx, http.MethodPut, s.path("v1", "/banking/customer/"+mmGUID+"/card/reallocate"), body, nil); err != nil {
		return fmt.Errorf("reallocate card %s: %w", mmGUID, err)
	}
	return nil
}

func (s liveUML) RetryCardAllocation(ctx context.Context, mmGUID string) error {
	if err := s.c.do(ctx, http.MethodPut, s.path("v1", "/banking/customer/"+mmGUID+"/card/retry-allocation"), nil, nil); err != nil {
		return fmt.Errorf("retry card allocation %s: %w", mmGUID, err)
	}
	return nil
}

func (s liveUML) BankingDetails(ctx context.Context, mmGUID string) (*BankingDetails, error) {
	var out BankingDetails
	if err := s.c.do(ctx, http.MethodGet, s.path("v1", "/banking/customer/"+mmGUID+"/details"), nil, &out); err != nil {
		return nil, fmt.Errorf("banking details %s: %w", mmGUID, err)
	}
	return &out, nil
}

func (s liveUML) WalletTransactions(ctx context.Context, mmGUID string, from, to time.Time) ([]WalletTransaction, error) {
	path := s.path("v1", "/wallet/"+mmGUID+"/transactions") + q(map[string]string{
		"from": dateParam(from),
		"to":   dateParam(to),
	})

	var out struct {
		Transactions []WalletTransaction `json:"transactions"`
		Items        []WalletTransaction `json:"items"`
	}
	if err := s.c.do(ctx, http.MethodGet, path, nil, &out); err != nil {
		return nil, fmt.Errorf("wallet transactions %s: %w", mmGUID, err)
	}
	if out.Transactions != nil {
		return out.Transactions, nil
	}
	return out.Items, nil
}

// dateParam renders a time for upstream query filters, treating the zero time
// as "unset" so q() drops it.
func dateParam(t time.Time) string {
	if t.IsZero() {
		return ""
	}
	return t.Format("2006-01-02")
}
