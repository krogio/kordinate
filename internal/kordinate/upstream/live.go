package upstream

import "strings"

// NewLive builds the live HTTP client set. Construction never fails: a service
// with no URL configured returns a "no base URL configured" error per call, so
// one missing env var degrades that one service instead of preventing startup.
func NewLive(cfg Config) Set {
	return Set{
		Customer: liveCustomer{c: newClient("customer-service", cfg.CustomerServiceURL, cfg.Timeout)},
		Claire:   liveClaire{c: newClaireClient(cfg)},
		UML:      newLiveUML(cfg),
		UOPS:     newLiveUOPS(cfg),
		Emma:     newLiveEmma(cfg),
		IDV:      newLiveIDV(cfg),
		Device:   liveDevice{c: newClient("device-blocker", cfg.DeviceBlockerURL, cfg.Timeout)},
		VMS:      liveVMS{c: newClient("vms", cfg.VMSAPIURL, cfg.Timeout)},
	}
}

// apiV1Base appends the /api/v1 prefix that UOPS, Emma and IDV all share,
// tolerating a URL that already carries it so both env styles work.
func apiV1Base(rawURL string) string {
	base := strings.TrimRight(rawURL, "/")
	if base == "" || strings.HasSuffix(base, "/api/v1") {
		return base
	}
	return base + "/api/v1"
}

// newClaireClient wires Claire's credentials. claire-admin signed these calls
// with OAuth1; kordinate sends the same consumer credentials as headers, which
// is what the current Claire gateway accepts. Requests are unsigned if the
// credentials are absent, so a misconfigured deployment fails at Claire with a
// 401 rather than silently reading nothing.
func newClaireClient(cfg Config) *client {
	c := newClient("claire", cfg.ClaireAPIURL, cfg.Timeout)
	if cfg.ClaireConsumerKey != "" {
		c.headers["X-Consumer-Key"] = cfg.ClaireConsumerKey
		c.headers["X-Consumer-Secret"] = cfg.ClaireConsumerSecret
	}
	if cfg.ClaireToken != "" {
		c.headers["X-Auth-Token"] = cfg.ClaireToken
		c.headers["X-Auth-Token-Secret"] = cfg.ClaireTokenSecret
	}
	return c
}
