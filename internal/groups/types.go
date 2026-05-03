package groups

import (
	"errors"
	"time"

	"github.com/google/uuid"
)

var (
	ErrNotFound         = errors.New("not found")
	ErrForbidden        = errors.New("forbidden")
	ErrInviteRequired   = errors.New("invite required")
	ErrOwnerCannotLeave = errors.New("owner cannot leave")
	ErrConflict         = errors.New("conflict")
)

const SystemGroupKeyCommunitySupport = "community_support"

type GroupVisibility string

const (
	GroupVisibilityPublic           GroupVisibility = "public"
	GroupVisibilityApprovalRequired GroupVisibility = "approval_required"
	GroupVisibilityInviteOnly       GroupVisibility = "invite_only"
	GroupVisibilityPrivateHidden    GroupVisibility = "private_hidden"
)

func (v GroupVisibility) Valid() bool {
	switch v {
	case GroupVisibilityPublic, GroupVisibilityApprovalRequired, GroupVisibilityInviteOnly, GroupVisibilityPrivateHidden:
		return true
	default:
		return false
	}
}

type GroupRole string

const (
	GroupRoleOwner     GroupRole = "owner"
	GroupRoleAdmin     GroupRole = "admin"
	GroupRoleModerator GroupRole = "moderator"
	GroupRoleMember    GroupRole = "member"
)

type MembershipStatus string

const (
	MembershipStatusActive MembershipStatus = "active"
	MembershipStatusBanned MembershipStatus = "banned"
)

type PostingPermission string

const (
	PostingPermissionMembers PostingPermission = "members"
	PostingPermissionAdmins  PostingPermission = "admins"
)

func (p PostingPermission) Valid() bool {
	switch p {
	case PostingPermissionMembers, PostingPermissionAdmins:
		return true
	default:
		return false
	}
}

type Group struct {
	ID                  uuid.UUID           `json:"id"`
	OwnerID             uuid.UUID           `json:"owner_id"`
	Name                string              `json:"name"`
	Slug                string              `json:"slug"`
	Description         *string             `json:"description,omitempty"`
	Rules               *string             `json:"rules,omitempty"`
	AvatarURL           *string             `json:"avatar_url,omitempty"`
	CoverURL            *string             `json:"cover_url,omitempty"`
	Visibility          GroupVisibility     `json:"visibility"`
	PostingPermission   PostingPermission   `json:"posting_permission"`
	AllowAnonymousPosts bool                `json:"allow_anonymous_posts"`
	City                *string             `json:"city,omitempty"`
	Country             *string             `json:"country,omitempty"`
	Tags                []string            `json:"tags"`
	RecoveryPathways    []string            `json:"recovery_pathways"`
	MemberCount         int                 `json:"member_count"`
	PostCount           int                 `json:"post_count"`
	MediaCount          int                 `json:"media_count"`
	PendingRequestCount int                 `json:"pending_request_count"`
	IsSystem            bool                `json:"is_system"`
	SystemKey           *string             `json:"system_key,omitempty"`
	LockedSettings      bool                `json:"locked_settings"`
	ViewerRole          *GroupRole          `json:"viewer_role,omitempty"`
	ViewerStatus        *MembershipStatus   `json:"viewer_status,omitempty"`
	HasPendingRequest   bool                `json:"has_pending_request"`
	CanPost             bool                `json:"can_post"`
	CanInvite           bool                `json:"can_invite"`
	CanManageMembers    bool                `json:"can_manage_members"`
	CanManageSettings   bool                `json:"can_manage_settings"`
	CanModerateContent  bool                `json:"can_moderate_content"`
	Owner               *GroupAdminPreview  `json:"owner,omitempty"`
	Admins              []GroupAdminPreview `json:"admins"`
	CreatedAt           time.Time           `json:"created_at"`
	UpdatedAt           time.Time           `json:"updated_at"`
}

type GroupAdminPreview struct {
	UserID    uuid.UUID `json:"user_id"`
	Username  string    `json:"username"`
	AvatarURL *string   `json:"avatar_url,omitempty"`
	Role      GroupRole `json:"role"`
}

type GroupPostType string

const (
	GroupPostTypeStandard          GroupPostType = "standard"
	GroupPostTypeMilestone         GroupPostType = "milestone"
	GroupPostTypeNeedSupport       GroupPostType = "need_support"
	GroupPostTypeAdminAnnouncement GroupPostType = "admin_announcement"
	GroupPostTypeCheckIn           GroupPostType = "check_in"
)

func (t GroupPostType) Valid() bool {
	switch t {
	case GroupPostTypeStandard, GroupPostTypeMilestone, GroupPostTypeNeedSupport, GroupPostTypeAdminAnnouncement, GroupPostTypeCheckIn:
		return true
	default:
		return false
	}
}

type GroupPostImage struct {
	ID        uuid.UUID `json:"id"`
	ImageURL  string    `json:"image_url"`
	ThumbURL  *string   `json:"thumb_url,omitempty"`
	Width     int       `json:"width"`
	Height    int       `json:"height"`
	Position  int       `json:"position"`
	CreatedAt time.Time `json:"created_at"`
}

type GroupPost struct {
	ID               uuid.UUID            `json:"id"`
	GroupID          uuid.UUID            `json:"group_id"`
	UserID           uuid.UUID            `json:"user_id"`
	Username         string               `json:"username"`
	AvatarURL        *string              `json:"avatar_url,omitempty"`
	PostType         GroupPostType        `json:"post_type"`
	Body             string               `json:"body"`
	Anonymous        bool                 `json:"anonymous"`
	PinnedAt         *time.Time           `json:"pinned_at,omitempty"`
	PinnedBy         *uuid.UUID           `json:"pinned_by,omitempty"`
	CommentCount     int                  `json:"comment_count"`
	ReactionCount    int                  `json:"reaction_count"`
	ImageCount       int                  `json:"image_count"`
	SupportRequestID *uuid.UUID           `json:"support_request_id,omitempty"`
	SupportRequest   *GroupSupportRequest `json:"support_request,omitempty"`
	ViewerHasReacted bool                 `json:"viewer_has_reacted"`
	Images           []GroupPostImage     `json:"images"`
	CreatedAt        time.Time            `json:"created_at"`
	UpdatedAt        time.Time            `json:"updated_at"`
}

type GroupSupportLocation struct {
	City           *string  `json:"city,omitempty"`
	Region         *string  `json:"region,omitempty"`
	Country        *string  `json:"country,omitempty"`
	ApproximateLat *float64 `json:"approximate_lat,omitempty"`
	ApproximateLng *float64 `json:"approximate_lng,omitempty"`
	Visibility     string   `json:"visibility"`
}

type GroupSupportRequest struct {
	ID                  uuid.UUID             `json:"id"`
	RequesterID         uuid.UUID             `json:"requester_id"`
	Username            string                `json:"username"`
	AvatarURL           *string               `json:"avatar_url,omitempty"`
	City                *string               `json:"city,omitempty"`
	SupportType         string                `json:"support_type"`
	Topics              []string              `json:"topics"`
	PreferredGender     *string               `json:"preferred_gender,omitempty"`
	Location            *GroupSupportLocation `json:"location,omitempty"`
	Message             *string               `json:"message,omitempty"`
	Urgency             string                `json:"urgency"`
	Status              string                `json:"status"`
	ReplyCount          int                   `json:"reply_count"`
	OfferCount          int                   `json:"offer_count"`
	ViewCount           int                   `json:"view_count"`
	IsPriority          bool                  `json:"is_priority"`
	GroupPostID         *uuid.UUID            `json:"group_post_id,omitempty"`
	CreatedAt           time.Time             `json:"created_at"`
	AcceptedResponderID *uuid.UUID            `json:"accepted_responder_id,omitempty"`
	AcceptedAt          *time.Time            `json:"accepted_at,omitempty"`
	ClosedAt            *time.Time            `json:"closed_at,omitempty"`
	ResponderID         *uuid.UUID            `json:"responder_id,omitempty"`
	ResponderUsername   *string               `json:"responder_username,omitempty"`
	ResponderAvatarURL  *string               `json:"responder_avatar_url,omitempty"`
	ChatID              *uuid.UUID            `json:"chat_id,omitempty"`
	HasOffered          bool                  `json:"has_offered"`
	HasReplied          bool                  `json:"has_replied"`
	AlreadyChatting     bool                  `json:"already_chatting"`
	ExistingChatID      *uuid.UUID            `json:"existing_chat_id,omitempty"`
	IsOwnRequest        bool                  `json:"is_own_request"`
}

type GroupComment struct {
	ID        uuid.UUID `json:"id"`
	GroupID   uuid.UUID `json:"group_id"`
	PostID    uuid.UUID `json:"post_id"`
	UserID    uuid.UUID `json:"user_id"`
	Username  string    `json:"username"`
	AvatarURL *string   `json:"avatar_url,omitempty"`
	Body      string    `json:"body"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}

type GroupMediaItem struct {
	ID        uuid.UUID `json:"id"`
	GroupID   uuid.UUID `json:"group_id"`
	PostID    uuid.UUID `json:"post_id"`
	ImageURL  string    `json:"image_url"`
	ThumbURL  *string   `json:"thumb_url,omitempty"`
	Width     int       `json:"width"`
	Height    int       `json:"height"`
	CreatedAt time.Time `json:"created_at"`
}

type GroupMember struct {
	UserID    uuid.UUID        `json:"user_id"`
	Username  string           `json:"username"`
	AvatarURL *string          `json:"avatar_url,omitempty"`
	Role      GroupRole        `json:"role"`
	Status    MembershipStatus `json:"status"`
	JoinedAt  *time.Time       `json:"joined_at,omitempty"`
	CreatedAt time.Time        `json:"created_at"`
	UpdatedAt time.Time        `json:"updated_at"`
}

type CreateGroupInput struct {
	Name                string
	Description         *string
	Rules               *string
	AvatarURL           *string
	CoverURL            *string
	Visibility          GroupVisibility
	PostingPermission   PostingPermission
	AllowAnonymousPosts bool
	City                *string
	Country             *string
	Tags                []string
	RecoveryPathways    []string
}

type ListGroupsParams struct {
	Query           string
	City            string
	Country         string
	Tag             string
	RecoveryPathway string
	Visibility      string
	GroupType       string
	MemberScope     string
	Before          *time.Time
	Limit           int
}

type JoinGroupResult struct {
	State string `json:"state"`
	Group *Group `json:"group,omitempty"`
}

type CreateGroupInviteInput struct {
	ExpiresAt        *time.Time
	MaxUses          *int
	RequiresApproval bool
}

type GroupInvite struct {
	ID               uuid.UUID  `json:"id"`
	GroupID          uuid.UUID  `json:"group_id"`
	Token            string     `json:"token,omitempty"`
	ExpiresAt        *time.Time `json:"expires_at,omitempty"`
	MaxUses          *int       `json:"max_uses,omitempty"`
	UseCount         int        `json:"use_count"`
	RequiresApproval bool       `json:"requires_approval"`
	RevokedAt        *time.Time `json:"revoked_at,omitempty"`
	CreatedAt        time.Time  `json:"created_at"`
}

type GroupInvitePreview struct {
	Token            string          `json:"token"`
	GroupID          uuid.UUID       `json:"group_id"`
	GroupName        string          `json:"group_name"`
	GroupSlug        string          `json:"group_slug"`
	GroupAvatarURL   *string         `json:"group_avatar_url,omitempty"`
	Visibility       GroupVisibility `json:"visibility"`
	RequiresApproval bool            `json:"requires_approval"`
	ExpiresAt        *time.Time      `json:"expires_at,omitempty"`
	MaxUses          *int            `json:"max_uses,omitempty"`
	UseCount         int             `json:"use_count"`
	ViewerStatus     string          `json:"viewer_status"`
	CreatedAt        time.Time       `json:"created_at"`
}

type GroupJoinRequest struct {
	ID         uuid.UUID  `json:"id"`
	GroupID    uuid.UUID  `json:"group_id"`
	UserID     uuid.UUID  `json:"user_id"`
	Username   string     `json:"username"`
	AvatarURL  *string    `json:"avatar_url,omitempty"`
	Message    *string    `json:"message,omitempty"`
	Status     string     `json:"status"`
	ReviewedBy *uuid.UUID `json:"reviewed_by,omitempty"`
	ReviewedAt *time.Time `json:"reviewed_at,omitempty"`
	CreatedAt  time.Time  `json:"created_at"`
	UpdatedAt  time.Time  `json:"updated_at"`
}

type GroupAdminThread struct {
	ID        uuid.UUID           `json:"id"`
	GroupID   uuid.UUID           `json:"group_id"`
	UserID    uuid.UUID           `json:"user_id"`
	Username  string              `json:"username"`
	AvatarURL *string             `json:"avatar_url,omitempty"`
	Status    string              `json:"status"`
	Subject   *string             `json:"subject,omitempty"`
	CreatedAt time.Time           `json:"created_at"`
	UpdatedAt time.Time           `json:"updated_at"`
	Messages  []GroupAdminMessage `json:"messages,omitempty"`
}

type GroupAdminMessage struct {
	ID        uuid.UUID `json:"id"`
	ThreadID  uuid.UUID `json:"thread_id"`
	SenderID  uuid.UUID `json:"sender_id"`
	Username  string    `json:"username"`
	AvatarURL *string   `json:"avatar_url,omitempty"`
	Body      string    `json:"body"`
	CreatedAt time.Time `json:"created_at"`
}

type GroupReport struct {
	ID                uuid.UUID  `json:"id"`
	GroupID           uuid.UUID  `json:"group_id"`
	ReporterID        uuid.UUID  `json:"reporter_id"`
	TargetType        string     `json:"target_type"`
	TargetID          *uuid.UUID `json:"target_id,omitempty"`
	Reason            string     `json:"reason"`
	Details           *string    `json:"details,omitempty"`
	Status            string     `json:"status"`
	ReporterUsername  string     `json:"reporter_username,omitempty"`
	ReporterAvatarURL *string    `json:"reporter_avatar_url,omitempty"`
	ReviewedBy        *uuid.UUID `json:"reviewed_by,omitempty"`
	ReviewedAt        *time.Time `json:"reviewed_at,omitempty"`
	CreatedAt         time.Time  `json:"created_at"`
}

type CreateGroupPostInput struct {
	PostType  GroupPostType
	Body      string
	Anonymous bool
	Images    []CreateGroupPostImageInput
}

type CreateGroupPostImageInput struct {
	ImageURL string
	ThumbURL *string
	Width    int
	Height   int
}
