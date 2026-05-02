package notifications

import (
	"context"
	"time"

	"github.com/google/uuid"
)

const (
	NotificationTypeChatMessage       = "chat.message"
	NotificationTypeCommentMention    = "comment.mention"
	NotificationTypeGroupJoinRequest  = "group.join_request"
	NotificationTypeGroupJoinApproved = "group.join_approved"
	NotificationTypeGroupPost         = "group.post"
	NotificationTypeGroupComment      = "group.comment"
	NotificationTypeGroupAdminContact = "group.admin_contact"
	NotificationTypeGroupAdminReply   = "group.admin_reply"
	NotificationTypeGroupReport       = "group.report"

	ResourceTypeChat             = "chat"
	ResourceTypeComment          = "comment"
	ResourceTypeGroup            = "group"
	ResourceTypeGroupPost        = "group_post"
	ResourceTypeGroupComment     = "group_comment"
	ResourceTypeGroupAdminThread = "group_admin_thread"
	ResourceTypeGroupReport      = "group_report"
)

type RegisterDeviceInput struct {
	PushToken  string `json:"push_token"`
	Platform   string `json:"platform"`
	DeviceName string `json:"device_name,omitempty"`
	AppVersion string `json:"app_version,omitempty"`
}

type Preferences struct {
	ChatMessages    bool `json:"chat_messages"`
	CommentMentions bool `json:"comment_mentions"`
}

type Notification struct {
	ID           uuid.UUID      `json:"id"`
	UserID       uuid.UUID      `json:"user_id"`
	Type         string         `json:"type"`
	ActorID      *uuid.UUID     `json:"actor_id,omitempty"`
	ResourceType string         `json:"resource_type"`
	ResourceID   *uuid.UUID     `json:"resource_id,omitempty"`
	Title        string         `json:"title"`
	Body         string         `json:"body"`
	Payload      map[string]any `json:"payload"`
	CreatedAt    time.Time      `json:"created_at"`
	ReadAt       *time.Time     `json:"read_at,omitempty"`
}

type NotificationSummary struct {
	UnreadCount int `json:"unread_count"`
}

type BulkReadResult struct {
	Read    bool `json:"read"`
	Updated int  `json:"updated"`
}

type Device struct {
	ID         uuid.UUID  `json:"id"`
	UserID     uuid.UUID  `json:"user_id"`
	PushToken  string     `json:"push_token"`
	Platform   string     `json:"platform"`
	DeviceName *string    `json:"device_name,omitempty"`
	AppVersion *string    `json:"app_version,omitempty"`
	LastSeenAt time.Time  `json:"last_seen_at"`
	DisabledAt *time.Time `json:"disabled_at,omitempty"`
}

type PushMessage struct {
	To    string         `json:"to"`
	Title string         `json:"title"`
	Body  string         `json:"body"`
	Data  map[string]any `json:"data,omitempty"`
}

type PushResult struct {
	ProviderMessageID string
	PermanentFailure  bool
	DisableDevice     bool
}

type deliveryJob struct {
	ID             uuid.UUID
	NotificationID uuid.UUID
	UserDeviceID   *uuid.UUID
	PushToken      string
	Title          string
	Body           string
	Payload        map[string]any
}

type Store interface {
	UpsertDevice(ctx context.Context, userID uuid.UUID, input RegisterDeviceInput) (*Device, error)
	DisableDevice(ctx context.Context, deviceID uuid.UUID, disabledAt time.Time) error
	DeleteDevice(ctx context.Context, userID, deviceID uuid.UUID) error
	GetPreferences(ctx context.Context, userID uuid.UUID) (*Preferences, error)
	UpdatePreferences(ctx context.Context, userID uuid.UUID, input Preferences) (*Preferences, error)
	ListNotifications(ctx context.Context, userID uuid.UUID, before *time.Time, limit int) ([]Notification, error)
	GetSummary(ctx context.Context, userID uuid.UUID) (*NotificationSummary, error)
	MarkNotificationRead(ctx context.Context, userID, notificationID uuid.UUID, readAt time.Time) error
	MarkNotificationsRead(ctx context.Context, userID uuid.UUID, notificationIDs []uuid.UUID, readAt time.Time) (int, error)
	MarkAllNotificationsRead(ctx context.Context, userID uuid.UUID, readAt time.Time) (int, error)
	MarkChatRead(ctx context.Context, chatID, userID uuid.UUID, lastReadMessageID *uuid.UUID, readAt time.Time) error
	CreateChatMessageNotifications(ctx context.Context, chatID, messageID, senderID uuid.UUID, body string) error
	CreateCommentMentionNotifications(ctx context.Context, postID, commentID, authorID uuid.UUID, mentionedUserIDs []uuid.UUID, body string) error
	CreateGroupJoinRequestNotifications(ctx context.Context, groupID, requesterID uuid.UUID) error
	CreateGroupJoinApprovedNotification(ctx context.Context, groupID, reviewerID, approvedUserID uuid.UUID) error
	CreateGroupPostNotifications(ctx context.Context, groupID, postID, authorID uuid.UUID, postType, body string) error
	CreateGroupCommentNotifications(ctx context.Context, groupID, postID, commentID, authorID uuid.UUID, body string) error
	CreateGroupAdminContactNotifications(ctx context.Context, groupID, threadID, senderID uuid.UUID, body string) error
	CreateGroupAdminReplyNotification(ctx context.Context, groupID, threadID, messageID, senderID uuid.UUID, body string) error
	CreateGroupReportNotifications(ctx context.Context, groupID, reportID, reporterID uuid.UUID, targetType, reason string) error
	ClaimPendingDeliveries(ctx context.Context, limit int, now time.Time) ([]deliveryJob, error)
	MarkDeliverySent(ctx context.Context, deliveryID uuid.UUID, providerMessageID string, sentAt time.Time) error
	MarkDeliveryFailed(ctx context.Context, deliveryID uuid.UUID, retryable bool, lastError string, nextAttemptAt time.Time) error
}

type PushProvider interface {
	Send(ctx context.Context, message PushMessage) (*PushResult, error)
}
