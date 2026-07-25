package upstream

import (
	"context"
	"fmt"
	"net/http"
	"strconv"
	"time"
)

type liveUOPS struct{ c *client }

// newLiveUOPS appends the /api/v1 prefix once, at construction, so call sites
// carry only the endpoint path.
func newLiveUOPS(cfg Config) liveUOPS {
	return liveUOPS{c: newClient("uops", apiV1Base(cfg.UOPSAPIURL), cfg.Timeout)}
}

// defaultNotActiveOrders matches claire-admin's page size for closed orders.
const defaultNotActiveOrders = 10

func (s liveUOPS) CustomerOrders(ctx context.Context, mmGUID string, from, to time.Time) ([]Order, error) {
	path := "/orders/customer" + q(map[string]string{
		"mmGlobalCustomerId":      mmGUID,
		"numberOfNotActiveOrders": strconv.Itoa(defaultNotActiveOrders),
	})

	var orders []Order
	if err := s.c.do(ctx, http.MethodGet, path, nil, &orders); err != nil {
		return nil, fmt.Errorf("uops orders %s: %w", mmGUID, err)
	}

	// UOPS has no date filter, so the window is applied here to match the
	// Claire-sourced list the UI stitches these into.
	filtered := make([]Order, 0, len(orders))
	for _, o := range orders {
		if !within(parseClaireTime(o.TimeCreated), from, to) {
			continue
		}
		o.Source = "uops"
		filtered = append(filtered, o)
	}
	return filtered, nil
}
