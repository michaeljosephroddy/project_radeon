package notifications

import (
	"context"
	"fmt"
	"os"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
)

func TestCreateSupportSignalNotificationsTargetsEligibleRecipientsIntegration(t *testing.T) {
	if os.Getenv("NOTIFICATIONS_DB_TEST") != "1" {
		t.Skip("set NOTIFICATIONS_DB_TEST=1 with DATABASE_URL to run database-backed notification fanout checks")
	}
	databaseURL := os.Getenv("DATABASE_URL")
	if databaseURL == "" {
		t.Fatal("DATABASE_URL is required")
	}

	ctx := context.Background()
	pool, err := pgxpool.New(ctx, databaseURL)
	if err != nil {
		t.Fatalf("connect db: %v", err)
	}
	defer pool.Close()

	store := NewPgStore(pool)
	suffix := time.Now().UTC().Format("150405000")
	requesterID := insertNotificationTestUser(t, ctx, pool, "req"+suffix)
	friendID := insertNotificationTestUser(t, ctx, pool, "fri"+suffix)
	friendMutedID := insertNotificationTestUser(t, ctx, pool, "fmu"+suffix)
	helperID := insertNotificationTestUser(t, ctx, pool, "hlp"+suffix)
	strangerID := insertNotificationTestUser(t, ctx, pool, "str"+suffix)
	blockedID := insertNotificationTestUser(t, ctx, pool, "blk"+suffix)
	deletedID := insertNotificationTestUser(t, ctx, pool, "del"+suffix)
	ids := []uuid.UUID{requesterID, friendID, friendMutedID, helperID, strangerID, blockedID, deletedID}
	defer cleanupNotificationTestUsers(t, ctx, pool, ids)

	signalID := uuid.New()
	if _, err := pool.Exec(ctx,
		`INSERT INTO support_signals (id, user_id, reason, status, expires_at)
		VALUES ($1, $2, 'cravings', 'active', NOW() + INTERVAL '2 hours')`,
		signalID, requesterID,
	); err != nil {
		t.Fatalf("insert signal: %v", err)
	}
	insertNotificationTestFriendship(t, ctx, pool, requesterID, friendID)
	insertNotificationTestFriendship(t, ctx, pool, requesterID, friendMutedID)

	if _, err := pool.Exec(ctx,
		`INSERT INTO notification_preferences (user_id, reach_out_alerts, reach_out_helper_alerts)
		VALUES ($1, FALSE, FALSE), ($2, TRUE, TRUE), ($3, TRUE, TRUE)
		ON CONFLICT (user_id) DO UPDATE
		SET reach_out_alerts = EXCLUDED.reach_out_alerts,
			reach_out_helper_alerts = EXCLUDED.reach_out_helper_alerts`,
		friendMutedID, helperID, blockedID,
	); err != nil {
		t.Fatalf("insert preferences: %v", err)
	}
	if _, err := pool.Exec(ctx, `INSERT INTO user_blocks (blocker_id, blocked_id) VALUES ($1, $2)`, requesterID, blockedID); err != nil {
		t.Fatalf("insert block: %v", err)
	}
	if _, err := pool.Exec(ctx, `UPDATE users SET deleted_at = NOW() WHERE id = $1`, deletedID); err != nil {
		t.Fatalf("mark deleted: %v", err)
	}

	if err := store.CreateSupportSignalNotifications(ctx, signalID, requesterID); err != nil {
		t.Fatalf("create notifications: %v", err)
	}

	got := notificationRecipientsForResource(t, ctx, pool, signalID)
	want := map[uuid.UUID]bool{
		friendID: true,
		helperID: true,
	}
	if len(got) != len(want) {
		t.Fatalf("recipient count = %d, want %d: %#v", len(got), len(want), got)
	}
	for recipientID := range want {
		if !got[recipientID] {
			t.Fatalf("missing recipient %s in %#v", recipientID, got)
		}
	}
	for _, excludedID := range []uuid.UUID{requesterID, friendMutedID, strangerID, blockedID, deletedID} {
		if got[excludedID] {
			t.Fatalf("excluded recipient %s received notification", excludedID)
		}
	}

	var body string
	var reason string
	if err := pool.QueryRow(ctx,
		`SELECT body, payload->>'reason'
		FROM notifications
		WHERE resource_id = $1 AND user_id = $2`,
		signalID, friendID,
	).Scan(&body, &reason); err != nil {
		t.Fatalf("read notification body: %v", err)
	}
	if body != fmt.Sprintf("req%s is reaching out: Cravings.", suffix) {
		t.Fatalf("body = %q", body)
	}
	if reason != "cravings" {
		t.Fatalf("payload reason = %q, want cravings", reason)
	}
}

func TestCreateSupportSignalResponseNotificationIncludesChatPayloadIntegration(t *testing.T) {
	if os.Getenv("NOTIFICATIONS_DB_TEST") != "1" {
		t.Skip("set NOTIFICATIONS_DB_TEST=1 with DATABASE_URL to run database-backed notification payload checks")
	}
	databaseURL := os.Getenv("DATABASE_URL")
	if databaseURL == "" {
		t.Fatal("DATABASE_URL is required")
	}

	ctx := context.Background()
	pool, err := pgxpool.New(ctx, databaseURL)
	if err != nil {
		t.Fatalf("connect db: %v", err)
	}
	defer pool.Close()

	store := NewPgStore(pool)
	suffix := time.Now().UTC().Format("150405000")
	requesterID := insertNotificationTestUser(t, ctx, pool, "rqa"+suffix)
	responderID := insertNotificationTestUser(t, ctx, pool, "rsa"+suffix)
	defer cleanupNotificationTestUsers(t, ctx, pool, []uuid.UUID{requesterID, responderID})

	signalID := uuid.New()
	chatID := uuid.New()
	if _, err := pool.Exec(ctx,
		`INSERT INTO support_signals (id, user_id, reason, status, expires_at)
		VALUES ($1, $2, 'need_to_talk', 'active', NOW() + INTERVAL '2 hours')`,
		signalID, requesterID,
	); err != nil {
		t.Fatalf("insert signal: %v", err)
	}

	if err := store.CreateSupportSignalResponseNotification(ctx, signalID, responderID, requesterID, chatID); err != nil {
		t.Fatalf("create response notification: %v", err)
	}

	var title string
	var payloadChatID string
	var payloadSignalID string
	if err := pool.QueryRow(ctx,
		`SELECT title, payload->>'chat_id', payload->>'support_signal_id'
		FROM notifications
		WHERE resource_id = $1 AND user_id = $2`,
		signalID, requesterID,
	).Scan(&title, &payloadChatID, &payloadSignalID); err != nil {
		t.Fatalf("read response notification: %v", err)
	}
	if title != "Someone replied" {
		t.Fatalf("title = %q, want Someone replied", title)
	}
	if payloadChatID != chatID.String() {
		t.Fatalf("payload chat_id = %q, want %q", payloadChatID, chatID)
	}
	if payloadSignalID != signalID.String() {
		t.Fatalf("payload support_signal_id = %q, want %q", payloadSignalID, signalID)
	}
}

func insertNotificationTestUser(t *testing.T, ctx context.Context, pool *pgxpool.Pool, username string) uuid.UUID {
	t.Helper()

	userID := uuid.New()
	email := username + "@example.test"
	if _, err := pool.Exec(ctx,
		`INSERT INTO users (id, username, email, password_hash)
		VALUES ($1, $2, $3, 'test')`,
		userID, username, email,
	); err != nil {
		t.Fatalf("insert user %s: %v", username, err)
	}
	return userID
}

func insertNotificationTestFriendship(t *testing.T, ctx context.Context, pool *pgxpool.Pool, userAID, userBID uuid.UUID) {
	t.Helper()

	if _, err := pool.Exec(ctx,
		`INSERT INTO friendships (user_a_id, user_b_id, requester_id, status, accepted_at)
		VALUES ($1, $2, $1, 'accepted', NOW())`,
		userAID, userBID,
	); err != nil {
		t.Fatalf("insert friendship: %v", err)
	}
}

func notificationRecipientsForResource(t *testing.T, ctx context.Context, pool *pgxpool.Pool, resourceID uuid.UUID) map[uuid.UUID]bool {
	t.Helper()

	rows, err := pool.Query(ctx, `SELECT user_id FROM notifications WHERE resource_id = $1`, resourceID)
	if err != nil {
		t.Fatalf("query notification recipients: %v", err)
	}
	defer rows.Close()

	recipients := map[uuid.UUID]bool{}
	for rows.Next() {
		var userID uuid.UUID
		if err := rows.Scan(&userID); err != nil {
			t.Fatalf("scan recipient: %v", err)
		}
		recipients[userID] = true
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("iterate recipients: %v", err)
	}
	return recipients
}

func cleanupNotificationTestUsers(t *testing.T, ctx context.Context, pool *pgxpool.Pool, userIDs []uuid.UUID) {
	t.Helper()

	if _, err := pool.Exec(ctx, `DELETE FROM users WHERE id = ANY($1)`, userIDs); err != nil {
		t.Fatalf("cleanup users: %v", err)
	}
}
