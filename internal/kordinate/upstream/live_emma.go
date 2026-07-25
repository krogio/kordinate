package upstream

import (
	"context"
	"fmt"
	"net/http"
	"strconv"
)

type liveEmma struct{ c *client }

func newLiveEmma(cfg Config) liveEmma {
	return liveEmma{c: newClient("emma", apiV1Base(cfg.EmmaAPIURL), cfg.Timeout)}
}

func (s liveEmma) PendingManualNotifications(ctx context.Context) ([]EFTNotification, error) {
	var out []EFTNotification
	if err := s.c.do(ctx, http.MethodGet, "/eft-notifications/pending-manual", nil, &out); err != nil {
		return nil, fmt.Errorf("pending manual eft notifications: %w", err)
	}
	return out, nil
}

func (s liveEmma) NotificationsByCustomer(ctx context.Context, mmGUID string) ([]EFTNotification, error) {
	path := "/eft-notifications" + q(map[string]string{"mmGlobalCustomerId": mmGUID})
	var out []EFTNotification
	if err := s.c.do(ctx, http.MethodGet, path, nil, &out); err != nil {
		return nil, fmt.Errorf("eft notifications for %s: %w", mmGUID, err)
	}
	return out, nil
}

func (s liveEmma) AssignDeposit(ctx context.Context, notificationID int64, mmGUID, agentID string) error {
	body := map[string]any{
		"mmGlobalCustomerId": mmGUID,
		"agentId":            agentID,
	}
	path := "/eft-notifications/" + strconv.FormatInt(notificationID, 10) + "/assign"
	if err := s.c.do(ctx, http.MethodPut, path, body, nil); err != nil {
		return fmt.Errorf("assign deposit %d: %w", notificationID, err)
	}
	return nil
}

func (s liveEmma) RefundDeposit(ctx context.Context, notificationID int64, reason, agentID string) error {
	body := map[string]any{
		"reason":  reason,
		"agentId": agentID,
	}
	path := "/eft-notifications/" + strconv.FormatInt(notificationID, 10) + "/refund"
	if err := s.c.do(ctx, http.MethodPost, path, body, nil); err != nil {
		return fmt.Errorf("refund deposit %d: %w", notificationID, err)
	}
	return nil
}

func (s liveEmma) MarkSuccess(ctx context.Context, notificationID int64, agentID string) error {
	body := map[string]any{"agentId": agentID}
	path := "/eft-notifications/" + strconv.FormatInt(notificationID, 10) + "/success"
	if err := s.c.do(ctx, http.MethodPut, path, body, nil); err != nil {
		return fmt.Errorf("mark deposit %d successful: %w", notificationID, err)
	}
	return nil
}

func (s liveEmma) SearchUnmatched(ctx context.Context, sq UnmatchedQuery) ([]EFTNotification, error) {
	pairs := map[string]string{
		"reference": sq.Reference,
		"bank":      sq.Bank,
		"from":      dateParam(sq.From),
		"to":        dateParam(sq.To),
	}
	if sq.AmountMin > 0 {
		pairs["amountMin"] = strconv.FormatFloat(sq.AmountMin, 'f', -1, 64)
	}
	if sq.AmountMax > 0 {
		pairs["amountMax"] = strconv.FormatFloat(sq.AmountMax, 'f', -1, 64)
	}

	var out []EFTNotification
	if err := s.c.do(ctx, http.MethodGet, "/eft-notifications/unmatched"+q(pairs), nil, &out); err != nil {
		return nil, fmt.Errorf("search unmatched deposits: %w", err)
	}
	return out, nil
}
