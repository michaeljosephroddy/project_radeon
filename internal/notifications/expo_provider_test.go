package notifications

import "testing"

func TestParseExpoPushResultObjectData(t *testing.T) {
	body := []byte(`{"data":{"status":"ok","id":"ticket-123"}}`)

	result, err := parseExpoPushResult(body)
	if err != nil {
		t.Fatalf("parseExpoPushResult returned error: %v", err)
	}
	if result.ProviderMessageID != "ticket-123" {
		t.Fatalf("expected provider message id ticket-123, got %q", result.ProviderMessageID)
	}
	if result.PermanentFailure {
		t.Fatal("expected non-permanent result for ok ticket")
	}
}

func TestParseExpoPushResultArrayData(t *testing.T) {
	body := []byte(`{"data":[{"status":"ok","id":"ticket-456"}]}`)

	result, err := parseExpoPushResult(body)
	if err != nil {
		t.Fatalf("parseExpoPushResult returned error: %v", err)
	}
	if result.ProviderMessageID != "ticket-456" {
		t.Fatalf("expected provider message id ticket-456, got %q", result.ProviderMessageID)
	}
}

func TestParseExpoPushResultDeviceNotRegistered(t *testing.T) {
	body := []byte(`{"data":{"status":"error","id":"ticket-789","details":{"error":"DeviceNotRegistered"}}}`)

	result, err := parseExpoPushResult(body)
	if err != nil {
		t.Fatalf("parseExpoPushResult returned error: %v", err)
	}
	if result.ProviderMessageID != "ticket-789" {
		t.Fatalf("expected provider message id ticket-789, got %q", result.ProviderMessageID)
	}
	if !result.PermanentFailure {
		t.Fatal("expected permanent failure for DeviceNotRegistered")
	}
	if !result.DisableDevice {
		t.Fatal("expected device disable for DeviceNotRegistered")
	}
}
