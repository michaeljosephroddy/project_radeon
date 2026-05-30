package notifications

import (
	"testing"

	"github.com/google/uuid"
)

func TestSupportSignalNotificationBodyUsesReasonLabel(t *testing.T) {
	body := supportSignalNotificationBody("Alex", "overwhelmed")

	if body != "Alex is reaching out: Feeling overwhelmed." {
		t.Fatalf("body = %q", body)
	}
}

func TestSupportSignalNotificationBodyFallsBackForUnknownReason(t *testing.T) {
	body := supportSignalNotificationBody("Alex", "unknown")

	if body != "Alex is reaching out for sober support." {
		t.Fatalf("body = %q", body)
	}
}

func TestSupportSignalPayloadIncludesReason(t *testing.T) {
	signalID := uuid.MustParse("00000000-0000-0000-0000-000000000001")
	actorID := uuid.MustParse("00000000-0000-0000-0000-000000000002")

	payload := supportSignalPayload(signalID, actorID, "cravings")

	assertPayloadString(t, payload, "type", NotificationTypeSupportSignal)
	assertPayloadString(t, payload, "support_signal_id", signalID.String())
	assertPayloadString(t, payload, "actor_user_id", actorID.String())
	assertPayloadString(t, payload, "reason", "cravings")
	assertPayloadString(t, payload, "notification_id", "")
}

func TestSupportSignalResponsePayloadIncludesChatID(t *testing.T) {
	signalID := uuid.MustParse("00000000-0000-0000-0000-000000000001")
	actorID := uuid.MustParse("00000000-0000-0000-0000-000000000002")
	chatID := uuid.MustParse("00000000-0000-0000-0000-000000000003")

	payload := supportSignalResponsePayload(signalID, actorID, chatID)

	assertPayloadString(t, payload, "type", NotificationTypeSupportSignalResponse)
	assertPayloadString(t, payload, "support_signal_id", signalID.String())
	assertPayloadString(t, payload, "actor_user_id", actorID.String())
	assertPayloadString(t, payload, "chat_id", chatID.String())
	assertPayloadString(t, payload, "notification_id", "")
}

func assertPayloadString(t *testing.T, payload map[string]any, key string, want string) {
	t.Helper()

	got, ok := payload[key].(string)
	if !ok {
		t.Fatalf("payload[%q] type = %T, want string", key, payload[key])
	}
	if got != want {
		t.Fatalf("payload[%q] = %q, want %q", key, got, want)
	}
}
