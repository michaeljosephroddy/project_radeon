package notifications

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

var ErrNotFound = errors.New("not found")

type pgStore struct {
	pool *pgxpool.Pool
}

func NewPgStore(pool *pgxpool.Pool) Store {
	return &pgStore{pool: pool}
}

func (s *pgStore) UpsertDevice(ctx context.Context, userID uuid.UUID, input RegisterDeviceInput) (*Device, error) {
	row := s.pool.QueryRow(ctx,
		`INSERT INTO user_devices (user_id, push_token, platform, device_name, app_version, last_seen_at, updated_at, disabled_at)
		VALUES ($1, $2, $3, NULLIF($4, ''), NULLIF($5, ''), NOW(), NOW(), NULL)
		ON CONFLICT (push_token) DO UPDATE
		SET user_id = EXCLUDED.user_id,
			platform = EXCLUDED.platform,
			device_name = EXCLUDED.device_name,
			app_version = EXCLUDED.app_version,
			last_seen_at = NOW(),
			updated_at = NOW(),
			disabled_at = NULL
		RETURNING id, user_id, push_token, platform, device_name, app_version, last_seen_at, disabled_at`,
		userID, input.PushToken, input.Platform, input.DeviceName, input.AppVersion,
	)

	var device Device
	if err := row.Scan(
		&device.ID,
		&device.UserID,
		&device.PushToken,
		&device.Platform,
		&device.DeviceName,
		&device.AppVersion,
		&device.LastSeenAt,
		&device.DisabledAt,
	); err != nil {
		return nil, err
	}
	return &device, nil
}

func (s *pgStore) DisableDevice(ctx context.Context, deviceID uuid.UUID, disabledAt time.Time) error {
	_, err := s.pool.Exec(ctx,
		`UPDATE user_devices
		SET disabled_at = $2, updated_at = $2
		WHERE id = $1`,
		deviceID, disabledAt,
	)
	return err
}

func (s *pgStore) DeleteDevice(ctx context.Context, userID, deviceID uuid.UUID) error {
	tag, err := s.pool.Exec(ctx,
		`DELETE FROM user_devices WHERE id = $1 AND user_id = $2`,
		deviceID, userID,
	)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}

func (s *pgStore) GetPreferences(ctx context.Context, userID uuid.UUID) (*Preferences, error) {
	if _, err := s.pool.Exec(ctx,
		`INSERT INTO notification_preferences (user_id)
		VALUES ($1)
		ON CONFLICT (user_id) DO NOTHING`,
		userID,
	); err != nil {
		return nil, err
	}

	var prefs Preferences
	if err := s.pool.QueryRow(ctx,
		`SELECT chat_messages, comment_mentions
		FROM notification_preferences
		WHERE user_id = $1`,
		userID,
	).Scan(&prefs.ChatMessages, &prefs.CommentMentions); err != nil {
		return nil, err
	}
	return &prefs, nil
}

func (s *pgStore) UpdatePreferences(ctx context.Context, userID uuid.UUID, input Preferences) (*Preferences, error) {
	if err := s.pool.QueryRow(ctx,
		`INSERT INTO notification_preferences (user_id, chat_messages, comment_mentions, created_at, updated_at)
		VALUES ($1, $2, $3, NOW(), NOW())
		ON CONFLICT (user_id) DO UPDATE
		SET chat_messages = EXCLUDED.chat_messages,
			comment_mentions = EXCLUDED.comment_mentions,
			updated_at = NOW()
		RETURNING chat_messages, comment_mentions`,
		userID, input.ChatMessages, input.CommentMentions,
	).Scan(&input.ChatMessages, &input.CommentMentions); err != nil {
		return nil, err
	}
	return &input, nil
}

func (s *pgStore) ListNotifications(ctx context.Context, userID uuid.UUID, before *time.Time, limit int) ([]Notification, error) {
	rows, err := s.pool.Query(ctx,
		`SELECT id, user_id, type, actor_id, resource_type, resource_id, title, body, payload, created_at, read_at
		FROM notifications
		WHERE user_id = $1
			AND ($2::timestamptz IS NULL OR created_at < $2)
		ORDER BY created_at DESC
		LIMIT $3`,
		userID, before, limit,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	items := make([]Notification, 0, limit)
	for rows.Next() {
		var item Notification
		var payloadBytes []byte
		if err := rows.Scan(
			&item.ID,
			&item.UserID,
			&item.Type,
			&item.ActorID,
			&item.ResourceType,
			&item.ResourceID,
			&item.Title,
			&item.Body,
			&payloadBytes,
			&item.CreatedAt,
			&item.ReadAt,
		); err != nil {
			return nil, err
		}
		if len(payloadBytes) > 0 {
			if err := json.Unmarshal(payloadBytes, &item.Payload); err != nil {
				return nil, err
			}
		}
		if item.Payload == nil {
			item.Payload = map[string]any{}
		}
		items = append(items, item)
	}
	return items, rows.Err()
}

func (s *pgStore) GetSummary(ctx context.Context, userID uuid.UUID) (*NotificationSummary, error) {
	var summary NotificationSummary
	if err := s.pool.QueryRow(ctx,
		`SELECT COALESCE((
			SELECT unread_count
			FROM notification_counters
			WHERE user_id = $1
		), 0)`,
		userID,
	).Scan(&summary.UnreadCount); err != nil {
		return nil, err
	}
	return &summary, nil
}

func (s *pgStore) MarkNotificationRead(ctx context.Context, userID, notificationID uuid.UUID, readAt time.Time) error {
	updated, err := s.MarkNotificationsRead(ctx, userID, []uuid.UUID{notificationID}, readAt)
	if err != nil {
		return err
	}
	if updated > 0 {
		return nil
	}

	var exists bool
	if err := s.pool.QueryRow(ctx,
		`SELECT EXISTS (
			SELECT 1
			FROM notifications
			WHERE id = $1 AND user_id = $2
		)`,
		notificationID, userID,
	).Scan(&exists); err != nil {
		return err
	}
	if !exists {
		return ErrNotFound
	}
	return nil
}

func (s *pgStore) MarkNotificationsRead(ctx context.Context, userID uuid.UUID, notificationIDs []uuid.UUID, readAt time.Time) (int, error) {
	if len(notificationIDs) == 0 {
		return 0, nil
	}

	tx, err := s.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return 0, err
	}
	defer tx.Rollback(ctx)

	updated, err := markNotificationIDsRead(ctx, tx, userID, notificationIDs, readAt)
	if err != nil {
		return 0, err
	}
	if updated > 0 {
		if err := decrementUnreadCounter(ctx, tx, userID, updated); err != nil {
			return 0, err
		}
	}
	if err := tx.Commit(ctx); err != nil {
		return 0, err
	}
	return updated, nil
}

func (s *pgStore) MarkAllNotificationsRead(ctx context.Context, userID uuid.UUID, readAt time.Time) (int, error) {
	tx, err := s.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return 0, err
	}
	defer tx.Rollback(ctx)

	var updated int
	if err := tx.QueryRow(ctx,
		`WITH updated AS (
			UPDATE notifications
			SET read_at = $2
			WHERE user_id = $1
				AND read_at IS NULL
			RETURNING id
		)
		SELECT COUNT(*)::int FROM updated`,
		userID, readAt,
	).Scan(&updated); err != nil {
		return 0, err
	}

	if _, err := tx.Exec(ctx,
		`INSERT INTO notification_counters (user_id, unread_count, updated_at)
		VALUES ($1, 0, $2)
		ON CONFLICT (user_id) DO UPDATE
		SET unread_count = 0,
			updated_at = EXCLUDED.updated_at`,
		userID, readAt,
	); err != nil {
		return 0, err
	}

	if err := tx.Commit(ctx); err != nil {
		return 0, err
	}
	return updated, nil
}

func (s *pgStore) MarkChatRead(ctx context.Context, chatID, userID uuid.UUID, lastReadMessageID *uuid.UUID, readAt time.Time) error {
	_, err := s.pool.Exec(ctx,
		`INSERT INTO chat_reads (chat_id, user_id, last_read_message_id, last_read_chat_seq, last_read_at)
		SELECT
			ch.id,
			$2,
			$3,
			COALESCE(
				CASE
					WHEN $3::uuid IS NULL THEN ch.last_message_seq
					ELSE msg.chat_seq
				END,
				0
			),
			$4
		FROM chats ch
		LEFT JOIN messages msg
			ON msg.id = $3
			AND msg.chat_id = ch.id
		WHERE ch.id = $1
		ON CONFLICT (chat_id, user_id) DO UPDATE
		SET last_read_message_id = COALESCE(EXCLUDED.last_read_message_id, chat_reads.last_read_message_id),
			last_read_chat_seq = GREATEST(chat_reads.last_read_chat_seq, EXCLUDED.last_read_chat_seq),
			last_read_at = GREATEST(chat_reads.last_read_at, EXCLUDED.last_read_at)`,
		chatID, userID, lastReadMessageID, readAt,
	)
	return err
}

func (s *pgStore) CreateChatMessageNotifications(ctx context.Context, chatID, messageID, senderID uuid.UUID, body string) error {
	tx, err := s.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)

	var senderUsername string
	if err := tx.QueryRow(ctx, `SELECT username FROM users WHERE id = $1`, senderID).Scan(&senderUsername); err != nil {
		return err
	}

	type recipient struct {
		userID  uuid.UUID
		enabled bool
	}

	rows, err := tx.Query(ctx,
		`SELECT cm.user_id, COALESCE(np.chat_messages, TRUE)
		FROM chat_members cm
		LEFT JOIN notification_preferences np ON np.user_id = cm.user_id
		WHERE cm.chat_id = $1
			AND cm.user_id <> $2`,
		chatID, senderID,
	)
	if err != nil {
		return err
	}

	recipients := make([]recipient, 0, 4)
	for rows.Next() {
		var next recipient
		if err := rows.Scan(&next.userID, &next.enabled); err != nil {
			rows.Close()
			return err
		}
		recipients = append(recipients, next)
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return err
	}
	rows.Close()

	for _, recipient := range recipients {
		if !recipient.enabled {
			continue
		}
		payload := map[string]any{
			"type":            NotificationTypeChatMessage,
			"chat_id":         chatID.String(),
			"message_id":      messageID.String(),
			"notification_id": "",
			"actor_user_id":   senderID.String(),
		}
		payloadBytes, err := json.Marshal(payload)
		if err != nil {
			return err
		}

		var notificationID uuid.UUID
		if err := tx.QueryRow(ctx,
			`INSERT INTO notifications (user_id, type, actor_id, resource_type, resource_id, title, body, payload)
			VALUES ($1, $2, $3, $4, $5, $6, $7, $8)
			RETURNING id`,
			recipient.userID,
			NotificationTypeChatMessage,
			senderID,
			ResourceTypeChat,
			chatID,
			senderUsername,
			body,
			payloadBytes,
		).Scan(&notificationID); err != nil {
			return err
		}

		payload["notification_id"] = notificationID.String()
		payloadBytes, err = json.Marshal(payload)
		if err != nil {
			return err
		}
		if _, err := tx.Exec(ctx, `UPDATE notifications SET payload = $2 WHERE id = $1`, notificationID, payloadBytes); err != nil {
			return err
		}

		if err := incrementUnreadCounter(ctx, tx, recipient.userID, 1); err != nil {
			return err
		}

		if err := s.enqueueDeliveriesForNotification(ctx, tx, notificationID, recipient.userID); err != nil {
			return err
		}
	}

	return tx.Commit(ctx)
}

func (s *pgStore) CreateCommentMentionNotifications(ctx context.Context, postID, commentID, authorID uuid.UUID, mentionedUserIDs []uuid.UUID, body string) error {
	if len(mentionedUserIDs) == 0 {
		return nil
	}

	tx, err := s.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)

	var authorUsername string
	if err := tx.QueryRow(ctx, `SELECT username FROM users WHERE id = $1`, authorID).Scan(&authorUsername); err != nil {
		return err
	}

	for _, mentionedUserID := range mentionedUserIDs {
		if mentionedUserID == authorID {
			continue
		}

		var enabled bool
		if err := tx.QueryRow(ctx,
			`SELECT COALESCE(comment_mentions, TRUE)
			FROM notification_preferences
			WHERE user_id = $1`,
			mentionedUserID,
		).Scan(&enabled); err != nil {
			if !errors.Is(err, pgx.ErrNoRows) {
				return err
			}
			enabled = true
		}
		if !enabled {
			continue
		}

		payload := map[string]any{
			"type":            NotificationTypeCommentMention,
			"post_id":         postID.String(),
			"comment_id":      commentID.String(),
			"notification_id": "",
			"actor_user_id":   authorID.String(),
		}
		payloadBytes, err := json.Marshal(payload)
		if err != nil {
			return err
		}

		var notificationID uuid.UUID
		if err := tx.QueryRow(ctx,
			`INSERT INTO notifications (user_id, type, actor_id, resource_type, resource_id, title, body, payload)
			VALUES ($1, $2, $3, $4, $5, $6, $7, $8)
			RETURNING id`,
			mentionedUserID,
			NotificationTypeCommentMention,
			authorID,
			ResourceTypeComment,
			commentID,
			authorUsername,
			body,
			payloadBytes,
		).Scan(&notificationID); err != nil {
			return err
		}

		payload["notification_id"] = notificationID.String()
		payloadBytes, err = json.Marshal(payload)
		if err != nil {
			return err
		}
		if _, err := tx.Exec(ctx, `UPDATE notifications SET payload = $2 WHERE id = $1`, notificationID, payloadBytes); err != nil {
			return err
		}

		if err := incrementUnreadCounter(ctx, tx, mentionedUserID, 1); err != nil {
			return err
		}

		if err := s.enqueueDeliveriesForNotification(ctx, tx, notificationID, mentionedUserID); err != nil {
			return err
		}
	}

	return tx.Commit(ctx)
}

func (s *pgStore) CreateGroupJoinRequestNotifications(ctx context.Context, groupID, requesterID uuid.UUID) error {
	tx, err := s.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)

	groupName, actorUsername, err := groupNotificationContext(ctx, tx, groupID, requesterID)
	if err != nil {
		return err
	}
	recipients, err := groupAdminNotificationRecipients(ctx, tx, groupID, requesterID)
	if err != nil {
		return err
	}
	for _, recipientID := range recipients {
		payload := groupPayload(NotificationTypeGroupJoinRequest, groupID, requesterID)
		if err := s.createNotification(ctx, tx, recipientID, NotificationTypeGroupJoinRequest, requesterID, ResourceTypeGroup, groupID, groupName, actorUsername+" requested to join", payload); err != nil {
			return err
		}
	}
	return tx.Commit(ctx)
}

func (s *pgStore) CreateGroupJoinApprovedNotification(ctx context.Context, groupID, reviewerID, approvedUserID uuid.UUID) error {
	tx, err := s.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)

	groupName, _, err := groupNotificationContext(ctx, tx, groupID, reviewerID)
	if err != nil {
		return err
	}
	if reviewerID == approvedUserID {
		return tx.Commit(ctx)
	}
	enabled, err := groupNotificationEnabled(ctx, tx, groupID, approvedUserID, "admin")
	if err != nil {
		return err
	}
	if enabled {
		payload := groupPayload(NotificationTypeGroupJoinApproved, groupID, reviewerID)
		if err := s.createNotification(ctx, tx, approvedUserID, NotificationTypeGroupJoinApproved, reviewerID, ResourceTypeGroup, groupID, groupName, "Your join request was approved", payload); err != nil {
			return err
		}
	}
	return tx.Commit(ctx)
}

func (s *pgStore) CreateGroupPostNotifications(ctx context.Context, groupID, postID, authorID uuid.UUID, postType, body string) error {
	tx, err := s.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)

	groupName, actorUsername, err := groupNotificationContext(ctx, tx, groupID, authorID)
	if err != nil {
		return err
	}
	recipients, err := groupMemberNotificationRecipients(ctx, tx, groupID, authorID, "post")
	if err != nil {
		return err
	}
	notificationBody := actorUsername + ": " + truncateNotificationBody(body, 120)
	if postType == "admin_announcement" {
		notificationBody = "Announcement from " + actorUsername + ": " + truncateNotificationBody(body, 100)
	}
	for _, recipientID := range recipients {
		payload := groupPayload(NotificationTypeGroupPost, groupID, authorID)
		payload["post_id"] = postID.String()
		payload["post_type"] = postType
		if err := s.createNotification(ctx, tx, recipientID, NotificationTypeGroupPost, authorID, ResourceTypeGroupPost, postID, groupName, notificationBody, payload); err != nil {
			return err
		}
	}
	return tx.Commit(ctx)
}

func (s *pgStore) CreateGroupCommentNotifications(ctx context.Context, groupID, postID, commentID, authorID uuid.UUID, body string) error {
	tx, err := s.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)

	groupName, actorUsername, err := groupNotificationContext(ctx, tx, groupID, authorID)
	if err != nil {
		return err
	}
	rows, err := tx.Query(ctx,
		`SELECT DISTINCT recipient_id
		FROM (
			SELECT user_id AS recipient_id
			FROM group_posts
			WHERE id = $2 AND group_id = $1 AND deleted_at IS NULL
			UNION
			SELECT user_id AS recipient_id
			FROM group_comments
			WHERE post_id = $2 AND group_id = $1 AND deleted_at IS NULL
		) recipients
		JOIN group_memberships gm
			ON gm.group_id = $1
			AND gm.user_id = recipients.recipient_id
			AND gm.status = 'active'
		LEFT JOIN group_notification_preferences gnp
			ON gnp.group_id = $1
			AND gnp.user_id = recipients.recipient_id
		WHERE recipients.recipient_id <> $3
			AND COALESCE(gnp.comment_notifications, TRUE)
			AND (gnp.muted_until IS NULL OR gnp.muted_until < NOW())`,
		groupID,
		postID,
		authorID,
	)
	if err != nil {
		return err
	}
	recipients, err := scanRecipientIDs(rows)
	if err != nil {
		return err
	}
	for _, recipientID := range recipients {
		payload := groupPayload(NotificationTypeGroupComment, groupID, authorID)
		payload["post_id"] = postID.String()
		payload["comment_id"] = commentID.String()
		if err := s.createNotification(ctx, tx, recipientID, NotificationTypeGroupComment, authorID, ResourceTypeGroupComment, commentID, groupName, actorUsername+" commented: "+truncateNotificationBody(body, 110), payload); err != nil {
			return err
		}
	}
	return tx.Commit(ctx)
}

func (s *pgStore) CreateGroupAdminContactNotifications(ctx context.Context, groupID, threadID, senderID uuid.UUID, body string) error {
	tx, err := s.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)

	groupName, actorUsername, err := groupNotificationContext(ctx, tx, groupID, senderID)
	if err != nil {
		return err
	}
	recipients, err := groupAdminNotificationRecipients(ctx, tx, groupID, senderID)
	if err != nil {
		return err
	}
	for _, recipientID := range recipients {
		payload := groupPayload(NotificationTypeGroupAdminContact, groupID, senderID)
		payload["thread_id"] = threadID.String()
		if err := s.createNotification(ctx, tx, recipientID, NotificationTypeGroupAdminContact, senderID, ResourceTypeGroupAdminThread, threadID, groupName, actorUsername+" contacted admins: "+truncateNotificationBody(body, 95), payload); err != nil {
			return err
		}
	}
	return tx.Commit(ctx)
}

func (s *pgStore) CreateGroupAdminReplyNotification(ctx context.Context, groupID, threadID, messageID, senderID uuid.UUID, body string) error {
	tx, err := s.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)

	groupName, actorUsername, err := groupNotificationContext(ctx, tx, groupID, senderID)
	if err != nil {
		return err
	}
	var threadOwnerID uuid.UUID
	if err := tx.QueryRow(ctx,
		`SELECT user_id
		FROM group_admin_threads
		WHERE id = $1 AND group_id = $2`,
		threadID,
		groupID,
	).Scan(&threadOwnerID); err != nil {
		return err
	}
	if threadOwnerID == senderID {
		return tx.Commit(ctx)
	}
	enabled, err := groupNotificationEnabled(ctx, tx, groupID, threadOwnerID, "admin")
	if err != nil {
		return err
	}
	if enabled {
		payload := groupPayload(NotificationTypeGroupAdminReply, groupID, senderID)
		payload["thread_id"] = threadID.String()
		payload["message_id"] = messageID.String()
		if err := s.createNotification(ctx, tx, threadOwnerID, NotificationTypeGroupAdminReply, senderID, ResourceTypeGroupAdminThread, threadID, groupName, actorUsername+" replied: "+truncateNotificationBody(body, 110), payload); err != nil {
			return err
		}
	}
	return tx.Commit(ctx)
}

func (s *pgStore) CreateGroupReportNotifications(ctx context.Context, groupID, reportID, reporterID uuid.UUID, targetType, reason string) error {
	tx, err := s.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)

	groupName, actorUsername, err := groupNotificationContext(ctx, tx, groupID, reporterID)
	if err != nil {
		return err
	}
	recipients, err := groupAdminNotificationRecipients(ctx, tx, groupID, reporterID)
	if err != nil {
		return err
	}
	for _, recipientID := range recipients {
		payload := groupPayload(NotificationTypeGroupReport, groupID, reporterID)
		payload["report_id"] = reportID.String()
		payload["target_type"] = targetType
		if err := s.createNotification(ctx, tx, recipientID, NotificationTypeGroupReport, reporterID, ResourceTypeGroupReport, reportID, groupName, actorUsername+" reported "+targetType+": "+truncateNotificationBody(reason, 80), payload); err != nil {
			return err
		}
	}
	return tx.Commit(ctx)
}

func markNotificationIDsRead(ctx context.Context, tx pgx.Tx, userID uuid.UUID, notificationIDs []uuid.UUID, readAt time.Time) (int, error) {
	var updated int
	if err := tx.QueryRow(ctx,
		`WITH updated AS (
			UPDATE notifications
			SET read_at = $3
			WHERE user_id = $1
				AND id = ANY($2::uuid[])
				AND read_at IS NULL
			RETURNING id
		)
		SELECT COUNT(*)::int FROM updated`,
		userID, notificationIDs, readAt,
	).Scan(&updated); err != nil {
		return 0, err
	}
	return updated, nil
}

func incrementUnreadCounter(ctx context.Context, tx pgx.Tx, userID uuid.UUID, amount int) error {
	if amount <= 0 {
		return nil
	}
	_, err := tx.Exec(ctx,
		`INSERT INTO notification_counters (user_id, unread_count, updated_at)
		VALUES ($1, $2, NOW())
		ON CONFLICT (user_id) DO UPDATE
		SET unread_count = notification_counters.unread_count + EXCLUDED.unread_count,
			updated_at = NOW()`,
		userID, amount,
	)
	return err
}

func decrementUnreadCounter(ctx context.Context, tx pgx.Tx, userID uuid.UUID, amount int) error {
	if amount <= 0 {
		return nil
	}
	_, err := tx.Exec(ctx,
		`INSERT INTO notification_counters (user_id, unread_count, updated_at)
		VALUES ($1, 0, NOW())
		ON CONFLICT (user_id) DO UPDATE
		SET unread_count = GREATEST(notification_counters.unread_count - $2, 0),
			updated_at = NOW()`,
		userID, amount,
	)
	return err
}

func groupNotificationContext(ctx context.Context, tx pgx.Tx, groupID, actorID uuid.UUID) (string, string, error) {
	var groupName string
	var actorUsername string
	if err := tx.QueryRow(ctx,
		`SELECT g.name, u.username
		FROM groups g
		JOIN users u ON u.id = $2
		WHERE g.id = $1 AND g.deleted_at IS NULL`,
		groupID,
		actorID,
	).Scan(&groupName, &actorUsername); err != nil {
		return "", "", err
	}
	return groupName, actorUsername, nil
}

func groupPayload(notificationType string, groupID, actorID uuid.UUID) map[string]any {
	return map[string]any{
		"type":            notificationType,
		"group_id":        groupID.String(),
		"notification_id": "",
		"actor_user_id":   actorID.String(),
	}
}

func (s *pgStore) createNotification(ctx context.Context, tx pgx.Tx, userID uuid.UUID, notificationType string, actorID uuid.UUID, resourceType string, resourceID uuid.UUID, title string, body string, payload map[string]any) error {
	payloadBytes, err := json.Marshal(payload)
	if err != nil {
		return err
	}

	var notificationID uuid.UUID
	if err := tx.QueryRow(ctx,
		`INSERT INTO notifications (user_id, type, actor_id, resource_type, resource_id, title, body, payload)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8)
		RETURNING id`,
		userID,
		notificationType,
		actorID,
		resourceType,
		resourceID,
		title,
		body,
		payloadBytes,
	).Scan(&notificationID); err != nil {
		return err
	}

	payload["notification_id"] = notificationID.String()
	payloadBytes, err = json.Marshal(payload)
	if err != nil {
		return err
	}
	if _, err := tx.Exec(ctx, `UPDATE notifications SET payload = $2 WHERE id = $1`, notificationID, payloadBytes); err != nil {
		return err
	}
	if err := incrementUnreadCounter(ctx, tx, userID, 1); err != nil {
		return err
	}
	return s.enqueueDeliveriesForNotification(ctx, tx, notificationID, userID)
}

func groupAdminNotificationRecipients(ctx context.Context, tx pgx.Tx, groupID, excludeUserID uuid.UUID) ([]uuid.UUID, error) {
	rows, err := tx.Query(ctx,
		`SELECT gm.user_id
		FROM group_memberships gm
		LEFT JOIN group_notification_preferences gnp
			ON gnp.group_id = gm.group_id
			AND gnp.user_id = gm.user_id
		WHERE gm.group_id = $1
			AND gm.user_id <> $2
			AND gm.status = 'active'
			AND gm.role IN ('owner', 'admin', 'moderator')
			AND COALESCE(gnp.admin_notifications, TRUE)
			AND (gnp.muted_until IS NULL OR gnp.muted_until < NOW())`,
		groupID,
		excludeUserID,
	)
	if err != nil {
		return nil, err
	}
	return scanRecipientIDs(rows)
}

func groupMemberNotificationRecipients(ctx context.Context, tx pgx.Tx, groupID, excludeUserID uuid.UUID, preference string) ([]uuid.UUID, error) {
	preferenceColumn := "post_notifications"
	if preference == "comment" {
		preferenceColumn = "comment_notifications"
	}
	rows, err := tx.Query(ctx,
		fmt.Sprintf(`SELECT gm.user_id
		FROM group_memberships gm
		LEFT JOIN group_notification_preferences gnp
			ON gnp.group_id = gm.group_id
			AND gnp.user_id = gm.user_id
		WHERE gm.group_id = $1
			AND gm.user_id <> $2
			AND gm.status = 'active'
			AND COALESCE(gnp.%s, TRUE)
			AND (gnp.muted_until IS NULL OR gnp.muted_until < NOW())`, preferenceColumn),
		groupID,
		excludeUserID,
	)
	if err != nil {
		return nil, err
	}
	return scanRecipientIDs(rows)
}

func groupNotificationEnabled(ctx context.Context, tx pgx.Tx, groupID, userID uuid.UUID, preference string) (bool, error) {
	preferenceColumn := "post_notifications"
	switch preference {
	case "admin":
		preferenceColumn = "admin_notifications"
	case "comment":
		preferenceColumn = "comment_notifications"
	}
	var enabled bool
	if err := tx.QueryRow(ctx,
		fmt.Sprintf(`SELECT EXISTS (
			SELECT 1
			FROM group_memberships gm
			LEFT JOIN group_notification_preferences gnp
				ON gnp.group_id = gm.group_id
				AND gnp.user_id = gm.user_id
			WHERE gm.group_id = $1
				AND gm.user_id = $2
				AND gm.status = 'active'
				AND COALESCE(gnp.%s, TRUE)
				AND (gnp.muted_until IS NULL OR gnp.muted_until < NOW())
		)`, preferenceColumn),
		groupID,
		userID,
	).Scan(&enabled); err != nil {
		return false, err
	}
	return enabled, nil
}

func scanRecipientIDs(rows pgx.Rows) ([]uuid.UUID, error) {
	defer rows.Close()

	recipients := make([]uuid.UUID, 0, 8)
	for rows.Next() {
		var userID uuid.UUID
		if err := rows.Scan(&userID); err != nil {
			return nil, err
		}
		recipients = append(recipients, userID)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return recipients, nil
}

func truncateNotificationBody(body string, limit int) string {
	body = strings.Join(strings.Fields(body), " ")
	runes := []rune(body)
	if len(runes) <= limit {
		return body
	}
	if limit <= 1 {
		return string(runes[:limit])
	}
	return string(runes[:limit-1]) + "..."
}

func (s *pgStore) enqueueDeliveriesForNotification(ctx context.Context, tx pgx.Tx, notificationID, userID uuid.UUID) error {
	rows, err := tx.Query(ctx,
		`SELECT id, push_token
		FROM user_devices
		WHERE user_id = $1
			AND disabled_at IS NULL`,
		userID,
	)
	if err != nil {
		return err
	}

	type device struct {
		id        uuid.UUID
		pushToken string
	}

	devices := make([]device, 0, 4)

	for rows.Next() {
		var next device
		if err := rows.Scan(&next.id, &next.pushToken); err != nil {
			rows.Close()
			return err
		}
		devices = append(devices, next)
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return err
	}
	rows.Close()

	for _, device := range devices {
		if _, err := tx.Exec(ctx,
			`INSERT INTO notification_deliveries (notification_id, user_device_id, provider, push_token, status, scheduled_at, created_at, updated_at)
			VALUES ($1, $2, 'expo', $3, 'pending', NOW(), NOW(), NOW())`,
			notificationID, device.id, device.pushToken,
		); err != nil {
			return err
		}
	}
	return nil
}

func (s *pgStore) ClaimPendingDeliveries(ctx context.Context, limit int, now time.Time) ([]deliveryJob, error) {
	tx, err := s.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return nil, err
	}
	defer tx.Rollback(ctx)

	rows, err := tx.Query(ctx,
		`WITH claim AS (
			SELECT nd.id
			FROM notification_deliveries nd
			WHERE nd.status = 'pending'
				AND nd.scheduled_at <= $1
			ORDER BY nd.scheduled_at ASC, nd.created_at ASC
			LIMIT $2
			FOR UPDATE SKIP LOCKED
		)
		SELECT nd.id,
			nd.notification_id,
			nd.user_device_id,
			nd.push_token,
			n.title,
			n.body,
			n.payload
		FROM notification_deliveries nd
		JOIN claim ON claim.id = nd.id
		JOIN notifications n ON n.id = nd.notification_id`,
		now, limit,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	jobs := make([]deliveryJob, 0, limit)
	for rows.Next() {
		var job deliveryJob
		var payloadBytes []byte
		if err := rows.Scan(
			&job.ID,
			&job.NotificationID,
			&job.UserDeviceID,
			&job.PushToken,
			&job.Title,
			&job.Body,
			&payloadBytes,
		); err != nil {
			return nil, err
		}

		if len(payloadBytes) > 0 {
			if err := json.Unmarshal(payloadBytes, &job.Payload); err != nil {
				return nil, err
			}
		}
		if job.Payload == nil {
			job.Payload = map[string]any{}
		}
		jobs = append(jobs, job)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}

	if err := tx.Commit(ctx); err != nil {
		return nil, err
	}
	return jobs, nil
}

func (s *pgStore) MarkDeliverySent(ctx context.Context, deliveryID uuid.UUID, providerMessageID string, sentAt time.Time) error {
	_, err := s.pool.Exec(ctx,
		`UPDATE notification_deliveries
		SET status = 'sent',
			provider_message_id = NULLIF($2, ''),
			sent_at = $3,
			attempt_count = attempt_count + 1,
			last_error = NULL,
			updated_at = $3
		WHERE id = $1`,
		deliveryID, providerMessageID, sentAt,
	)
	return err
}

func (s *pgStore) MarkDeliveryFailed(ctx context.Context, deliveryID uuid.UUID, retryable bool, lastError string, nextAttemptAt time.Time) error {
	status := "failed"
	if retryable {
		status = "pending"
	}
	_, err := s.pool.Exec(ctx,
		`UPDATE notification_deliveries
		SET status = $2,
			last_error = $3,
			attempt_count = attempt_count + 1,
			scheduled_at = CASE WHEN $2 = 'pending' THEN $4 ELSE scheduled_at END,
			updated_at = NOW()
		WHERE id = $1`,
		deliveryID, status, truncateError(lastError), nextAttemptAt,
	)
	return err
}

func truncateError(value string) string {
	if len(value) <= 500 {
		return value
	}
	return fmt.Sprintf("%s...", value[:497])
}
