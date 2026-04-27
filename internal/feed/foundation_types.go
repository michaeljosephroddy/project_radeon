package feed

import (
	"encoding/json"
	"errors"
	"strings"
	"time"

	"github.com/google/uuid"
)

var (
	ErrInvalidFeedMode     = errors.New("invalid feed mode")
	ErrInvalidFeedItemKind = errors.New("invalid feed item kind")
	ErrInvalidFeedEvent    = errors.New("invalid feed event type")
	ErrFeedFeatureDisabled = errors.New("feed feature disabled")
)

type FeedMode string

const (
	FeedModeHome    FeedMode = "home"
)

func (m FeedMode) Valid() bool {
	switch m {
	case FeedModeHome:
		return true
	default:
		return false
	}
}

type FeedItemKind string

const (
	FeedItemKindPost    FeedItemKind = "post"
	FeedItemKindReshare FeedItemKind = "reshare"
)

func (k FeedItemKind) Valid() bool {
	switch k {
	case FeedItemKindPost, FeedItemKindReshare:
		return true
	default:
		return false
	}
}

type FeedEventType string

const (
	FeedEventImpression   FeedEventType = "impression"
	FeedEventOpenPost     FeedEventType = "open_post"
	FeedEventOpenComments FeedEventType = "open_comments"
	FeedEventComment      FeedEventType = "comment"
	FeedEventLike         FeedEventType = "like"
	FeedEventUnlike       FeedEventType = "unlike"
	FeedEventShareOpen    FeedEventType = "share_open"
	FeedEventShareCreate  FeedEventType = "share_create"
	FeedEventHide         FeedEventType = "hide"
	FeedEventMuteAuthor   FeedEventType = "mute_author"
)

func (e FeedEventType) Valid() bool {
	switch e {
	case FeedEventImpression,
		FeedEventOpenPost,
		FeedEventOpenComments,
		FeedEventComment,
		FeedEventLike,
		FeedEventUnlike,
		FeedEventShareOpen,
		FeedEventShareCreate,
		FeedEventHide,
		FeedEventMuteAuthor:
		return true
	default:
		return false
	}
}

type FeedActor struct {
	UserID    uuid.UUID `json:"user_id"`
	Username  string    `json:"username"`
	AvatarURL *string   `json:"avatar_url"`
}

type ViewerFeedState struct {
	IsFriend   bool `json:"is_friend"`
	IsLiked    bool `json:"is_liked"`
	IsHidden   bool `json:"is_hidden"`
	IsMuted    bool `json:"is_muted"`
	IsReshared bool `json:"is_reshared"`
	IsOwnPost  bool `json:"is_own_post"`
	IsOwnShare bool `json:"is_own_share"`
}

type EmbeddedPost struct {
	PostID       uuid.UUID   `json:"post_id"`
	Author       FeedActor   `json:"author"`
	Body         string      `json:"body"`
	Images       []PostImage `json:"images"`
	CreatedAt    time.Time   `json:"created_at"`
	LikeCount    int         `json:"like_count"`
	CommentCount int         `json:"comment_count"`
	ShareCount   int         `json:"share_count"`
}

type ReshareMetadata struct {
	ShareID      uuid.UUID `json:"share_id"`
	OriginalPost uuid.UUID `json:"original_post_id"`
	Commentary   string    `json:"commentary"`
	CreatedAt    time.Time `json:"created_at"`
}

type FeedItem struct {
	ID              uuid.UUID        `json:"id"`
	Kind            FeedItemKind     `json:"kind"`
	Score           float64          `json:"score"`
	ServedAtKey     time.Time        `json:"served_at_key"`
	Author          FeedActor        `json:"author"`
	Body            string           `json:"body"`
	Images          []PostImage      `json:"images"`
	CreatedAt       time.Time        `json:"created_at"`
	LikeCount       int              `json:"like_count"`
	CommentCount    int              `json:"comment_count"`
	ShareCount      int              `json:"share_count"`
	ViewerState     ViewerFeedState  `json:"viewer_state"`
	OriginalPost    *EmbeddedPost    `json:"original_post,omitempty"`
	ReshareMetadata *ReshareMetadata `json:"reshare_metadata,omitempty"`
	rankingSignals  feedRankingSignals
}

type feedRankingSignals struct {
	QualityScore           float64
	RecentHideCount        int
	RecentImpressionCount  int
	AuthorRecentPostCount  int
	AuthorRecentShareCount int
	AuthorRollingHideCount int
	AffinityScore          float64
	SharedInterestCount    int
}

type FeedImpressionInput struct {
	ItemID       uuid.UUID    `json:"item_id"`
	ItemKind     FeedItemKind `json:"item_kind"`
	FeedMode     FeedMode     `json:"feed_mode"`
	SessionID    string       `json:"session_id"`
	Position     int          `json:"position"`
	ServedAt     time.Time    `json:"served_at"`
	ViewedAt     time.Time    `json:"viewed_at"`
	ViewMS       int          `json:"view_ms"`
	WasClicked   bool         `json:"was_clicked"`
	WasLiked     bool         `json:"was_liked"`
	WasCommented bool         `json:"was_commented"`
}

type FeedEventInput struct {
	ItemID    uuid.UUID       `json:"item_id"`
	ItemKind  FeedItemKind    `json:"item_kind"`
	FeedMode  FeedMode        `json:"feed_mode"`
	EventType FeedEventType   `json:"event_type"`
	Position  *int            `json:"position,omitempty"`
	EventAt   time.Time       `json:"event_at"`
	Payload   json.RawMessage `json:"payload,omitempty"`
}

type HiddenFeedItem struct {
	ItemID   uuid.UUID    `json:"item_id"`
	ItemKind FeedItemKind `json:"item_kind"`
	HiddenAt time.Time    `json:"hidden_at"`
	Item     FeedItem     `json:"item"`
}

func sanitizeCommentary(value string) string {
	return strings.TrimSpace(value)
}
