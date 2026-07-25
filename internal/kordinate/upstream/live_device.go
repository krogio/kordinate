package upstream

import (
	"context"
	"fmt"
	"net/http"
)

type liveDevice struct{ c *client }

// The device blocker speaks kebab-case on the wire, unlike the Device struct's
// camelCase JSON (which is kordinate's own view), so responses are decoded
// through deviceWire.
type deviceWire struct {
	DeviceID        string   `json:"device-id"`
	DeviceStatus    string   `json:"device-status"`
	LinkedCustomers []string `json:"linked-customers"`
	FirstSeen       string   `json:"first-seen"`
	LastSeen        string   `json:"last-seen"`
}

func (d deviceWire) toDevice() Device {
	return Device{
		DeviceID:        d.DeviceID,
		DeviceStatus:    DeviceStatus(d.DeviceStatus),
		LinkedCustomers: d.LinkedCustomers,
		FirstSeen:       d.FirstSeen,
		LastSeen:        d.LastSeen,
	}
}

func (s liveDevice) Device(ctx context.Context, deviceID string) (*Device, error) {
	var out deviceWire
	if err := s.c.do(ctx, http.MethodGet, "/device/"+deviceID, nil, &out); err != nil {
		return nil, fmt.Errorf("get device %s: %w", deviceID, err)
	}
	// The upstream answers 200 with an empty body for an unknown device, so an
	// absent id is the only signal that nothing was found.
	if out.DeviceID == "" {
		out.DeviceID = deviceID
	}
	d := out.toDevice()
	return &d, nil
}

func (s liveDevice) DevicesForCustomer(ctx context.Context, mmGUID string) ([]Device, error) {
	var out []deviceWire
	if err := s.c.do(ctx, http.MethodGet, "/customer/"+mmGUID, nil, &out); err != nil {
		return nil, fmt.Errorf("devices for customer %s: %w", mmGUID, err)
	}
	devices := make([]Device, 0, len(out))
	for _, w := range out {
		devices = append(devices, w.toDevice())
	}
	return devices, nil
}

func (s liveDevice) SetStatus(ctx context.Context, deviceID string, status DeviceStatus, reason, agentID string) error {
	body := map[string]any{"device-status": string(status)}
	if reason != "" {
		body["reason"] = reason
	}
	if agentID != "" {
		body["processing-agent-id"] = agentID
	}
	if err := s.c.do(ctx, http.MethodPut, "/device/"+deviceID+"/status", body, nil); err != nil {
		return fmt.Errorf("set device status %s: %w", deviceID, err)
	}
	return nil
}

func (s liveDevice) Register(ctx context.Context, deviceID string) error {
	// Registration defaults to ACTIVE — a device is only created here so it can
	// be tracked, and blocking is a separate, deliberate act.
	body := map[string]any{
		"device-id":     deviceID,
		"device-status": string(DeviceActive),
	}
	if err := s.c.do(ctx, http.MethodPost, "/device", body, nil); err != nil {
		return fmt.Errorf("register device %s: %w", deviceID, err)
	}
	return nil
}

func (s liveDevice) PatchAndUpdateLinked(ctx context.Context, deviceID string, status DeviceStatus, linkedCustomers []string, reason, agentID string) error {
	// linked-customers must be an array, never null: the upstream treats a null
	// as "no change" and would leave the linked customers untouched.
	if linkedCustomers == nil {
		linkedCustomers = []string{}
	}
	body := map[string]any{
		"device-status":    string(status),
		"linked-customers": linkedCustomers,
	}
	if reason != "" {
		body["reason"] = reason
	}
	if agentID != "" {
		body["processing-agent-id"] = agentID
	}
	if err := s.c.do(ctx, http.MethodPatch, "/device/"+deviceID, body, nil); err != nil {
		return fmt.Errorf("patch device %s: %w", deviceID, err)
	}
	return nil
}
