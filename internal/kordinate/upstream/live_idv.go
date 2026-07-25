package upstream

import (
	"context"
	"fmt"
	"net/http"
)

type liveIDV struct{ c *client }

func newLiveIDV(cfg Config) liveIDV {
	return liveIDV{c: newClient("idv", apiV1Base(cfg.IDVServiceURL), cfg.Timeout)}
}

func (s liveIDV) ResetLoginPIN(ctx context.Context, mmGUID, agentID string) error {
	// claire-admin sent no body here, but a PIN reset must be attributable, so
	// the agent goes along when known.
	var body any
	if agentID != "" {
		body = map[string]any{"agentId": agentID}
	}
	if err := s.c.do(ctx, http.MethodPut, "/customer/pin/"+mmGUID+"/reset", body, nil); err != nil {
		return fmt.Errorf("reset login pin %s: %w", mmGUID, err)
	}
	return nil
}
