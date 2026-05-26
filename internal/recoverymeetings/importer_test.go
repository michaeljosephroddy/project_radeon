package recoverymeetings

import (
	"errors"
	"testing"
)

func TestParseSnapshotBytesPreservesOnlineCredentials(t *testing.T) {
	snapshot, err := ParseSnapshotBytes([]byte(validSnapshotJSON()), false)
	if err != nil {
		t.Fatalf("ParseSnapshotBytes returned error: %v", err)
	}

	if snapshot.SchemaVersion != SnapshotSchemaVersion {
		t.Fatalf("schema version = %q", snapshot.SchemaVersion)
	}
	if len(snapshot.Meetings) != 1 {
		t.Fatalf("meetings length = %d, want 1", len(snapshot.Meetings))
	}
	meeting := snapshot.Meetings[0]
	if meeting.OnlineURL == nil || *meeting.OnlineURL != "https://zoom.example/j/123456789" {
		t.Fatalf("online_url = %#v", meeting.OnlineURL)
	}
	if meeting.PhoneJoinInfo == nil || *meeting.PhoneJoinInfo != "Meeting ID: 123 456 789 Passcode: sober" {
		t.Fatalf("phone_join_info = %#v", meeting.PhoneJoinInfo)
	}
	if got := meeting.Occurrences[0].Timezone; got != "Europe/Dublin" {
		t.Fatalf("timezone = %q", got)
	}
	if meeting.CountryCode == nil || *meeting.CountryCode != "IE" {
		t.Fatalf("country_code = %#v", meeting.CountryCode)
	}
	if meeting.RegionCode == nil || *meeting.RegionCode != "L" {
		t.Fatalf("region_code = %#v", meeting.RegionCode)
	}
}

func TestParseSnapshotBytesRejectsInvalidOccurrenceTime(t *testing.T) {
	payload := validSnapshotJSONWith(`"start_time_local":"not-a-time"`)
	_, err := ParseSnapshotBytes([]byte(payload), false)
	if !errors.Is(err, ErrInvalidSnapshot) {
		t.Fatalf("error = %v, want ErrInvalidSnapshot", err)
	}
}

func TestParseSnapshotBytesRejectsDuplicateSourceKey(t *testing.T) {
	payload := `{
		"schema_version":"2026-04-30",
		"generated_at":"2026-05-23T22:07:07Z",
		"meetings":[` + validMeetingJSON(`"source_record_id":"same"`) + `,` + validMeetingJSON(`"source_record_id":"same"`) + `]
	}`
	_, err := ParseSnapshotBytes([]byte(payload), false)
	if !errors.Is(err, ErrInvalidSnapshot) {
		t.Fatalf("error = %v, want ErrInvalidSnapshot", err)
	}
}

func TestParseSnapshotBytesRequiresMeetingsUnlessAllowed(t *testing.T) {
	payload := `{"schema_version":"2026-04-30","generated_at":"2026-05-23T22:07:07Z","meetings":[]}`
	if _, err := ParseSnapshotBytes([]byte(payload), false); !errors.Is(err, ErrInvalidSnapshot) {
		t.Fatalf("error = %v, want ErrInvalidSnapshot", err)
	}
	if _, err := ParseSnapshotBytes([]byte(payload), true); err != nil {
		t.Fatalf("allow empty returned error: %v", err)
	}
}

func validSnapshotJSON() string {
	return `{
		"schema_version":"2026-04-30",
		"generated_at":"2026-05-23T22:07:07Z",
		"meetings":[` + validMeetingJSON(`"source_record_id":"daily-reflection-monday"`) + `]
	}`
}

func validMeetingJSON(sourceRecordField string) string {
	return `{
		"accessibility_notes":null,
		"address_line1":"1 Main Street",
		"address_line2":null,
		"city":"Dublin",
		"country":"IE",
		"country_code":"IE",
		"fellowship":"ca",
		"formats":["Open","Discussion"],
		"is_approximate_location":false,
		"language":null,
		"last_verified_at":null,
		"latitude":null,
		"longitude":null,
		"meeting_type":"online",
		"name":"Daily Reflection",
		"occurrences":[{"day_of_week":1,"end_time_local":"20:30:00","start_time_local":"19:30:00","timezone":"Europe/Dublin"}],
		"online_url":"https://zoom.example/j/123456789",
		"phone_join_info":"Meeting ID: 123 456 789 Passcode: sober",
		"postal_code":null,
		"region":"Leinster",
		"region_code":"L",
		"source_id":"ca-ie-feed",
		` + sourceRecordField + `,
		"source_url":"https://example.org/meetings.json",
		"venue_name":"Online"
	}`
}

func validSnapshotJSONWith(occurrenceReplacement string) string {
	return `{
		"schema_version":"2026-04-30",
		"generated_at":"2026-05-23T22:07:07Z",
		"meetings":[{
			"fellowship":"ca",
			"source_id":"ca-ie-feed",
			"source_record_id":"daily-reflection-monday",
			"source_url":"https://example.org/meetings.json",
			"name":"Daily Reflection",
			"meeting_type":"online",
			"formats":[],
			"is_approximate_location":false,
			"occurrences":[{"day_of_week":1,"end_time_local":"20:30:00",` + occurrenceReplacement + `,"timezone":"Europe/Dublin"}]
		}]
	}`
}
