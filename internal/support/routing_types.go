package support

import (
	"time"

	"github.com/google/uuid"
)

type SupportChannel string

const (
	SupportChannelImmediate SupportChannel = "immediate"
	SupportChannelCommunity SupportChannel = "community"
)

type SupportRoutingStatus string

const (
	SupportRoutingPending       SupportRoutingStatus = "pending"
	SupportRoutingOffered       SupportRoutingStatus = "offered"
	SupportRoutingMatched       SupportRoutingStatus = "matched"
	SupportRoutingFallback      SupportRoutingStatus = "fallback"
	SupportRoutingClosed        SupportRoutingStatus = "closed"
	SupportRoutingNotApplicable SupportRoutingStatus = "not_applicable"
)

type SupportOfferStatus string

const (
	SupportOfferPending  SupportOfferStatus = "pending"
	SupportOfferAccepted SupportOfferStatus = "accepted"
	SupportOfferDeclined SupportOfferStatus = "declined"
	SupportOfferExpired  SupportOfferStatus = "expired"
	SupportOfferClosed   SupportOfferStatus = "closed"
)

type SupportSessionStatus string

const (
	SupportSessionPending   SupportSessionStatus = "pending"
	SupportSessionActive    SupportSessionStatus = "active"
	SupportSessionCompleted SupportSessionStatus = "completed"
	SupportSessionCancelled SupportSessionStatus = "cancelled"
)

type SupportResponderProfile struct {
	UserID                  uuid.UUID  `json:"user_id"`
	IsAvailableForImmediate bool       `json:"is_available_for_immediate"`
	IsAvailableForCommunity bool       `json:"is_available_for_community"`
	SupportsChat            bool       `json:"supports_chat"`
	SupportsCheckIns        bool       `json:"supports_check_ins"`
	SupportsInPerson        bool       `json:"supports_in_person"`
	MaxConcurrentSessions   int        `json:"max_concurrent_sessions"`
	Languages               []string   `json:"languages"`
	IsActive                bool       `json:"is_active"`
	AvailableNow            bool       `json:"available_now"`
	ActiveSessionCount      int        `json:"active_session_count"`
	AcceptanceRate          float64    `json:"acceptance_rate"`
	CompletionRate          float64    `json:"completion_rate"`
	HelpfulnessScore        float64    `json:"helpfulness_score"`
	MedianResponseSeconds   int        `json:"median_response_seconds"`
	UpdatedAt               time.Time  `json:"updated_at"`
	LastSeenAt              *time.Time `json:"last_seen_at,omitempty"`
	LastSessionCompletedAt  *time.Time `json:"last_session_completed_at,omitempty"`
}

type SupportOffer struct {
	ID                 uuid.UUID          `json:"id"`
	SupportRequestID   uuid.UUID          `json:"support_request_id"`
	RequesterID        uuid.UUID          `json:"requester_id"`
	RequesterUsername  string             `json:"requester_username"`
	RequesterAvatarURL *string            `json:"requester_avatar_url,omitempty"`
	RequestType        string             `json:"request_type"`
	RequestMessage     *string            `json:"request_message,omitempty"`
	RequestUrgency     string             `json:"request_urgency"`
	RequestChannel     SupportChannel     `json:"request_channel"`
	Status             SupportOfferStatus `json:"status"`
	MatchScore         float64            `json:"match_score"`
	FitSummary         *string            `json:"fit_summary,omitempty"`
	BatchNumber        int                `json:"batch_number"`
	OfferedAt          time.Time          `json:"offered_at"`
	ExpiresAt          time.Time          `json:"expires_at"`
	RespondedAt        *time.Time         `json:"responded_at,omitempty"`
	SortAt             time.Time          `json:"-"`
}

type SupportSession struct {
	ID                uuid.UUID            `json:"id"`
	SupportRequestID  uuid.UUID            `json:"support_request_id"`
	RequesterID       uuid.UUID            `json:"requester_id"`
	RequesterUsername string               `json:"requester_username"`
	ResponderID       uuid.UUID            `json:"responder_id"`
	ResponderUsername string               `json:"responder_username"`
	Status            SupportSessionStatus `json:"status"`
	Outcome           *string              `json:"outcome,omitempty"`
	ChatID            *uuid.UUID           `json:"chat_id,omitempty"`
	StartedAt         *time.Time           `json:"started_at,omitempty"`
	CompletedAt       *time.Time           `json:"completed_at,omitempty"`
	CancelledAt       *time.Time           `json:"cancelled_at,omitempty"`
	CreatedAt         time.Time            `json:"created_at"`
	SortAt            time.Time            `json:"-"`
}

type SupportOfferPage struct {
	Items      []SupportOffer `json:"items"`
	Limit      int            `json:"limit"`
	HasMore    bool           `json:"has_more"`
	NextCursor *string        `json:"next_cursor,omitempty"`
}

type SupportSessionsPage struct {
	Items      []SupportSession `json:"items"`
	Limit      int              `json:"limit"`
	HasMore    bool             `json:"has_more"`
	NextCursor *string          `json:"next_cursor,omitempty"`
}

type SupportHomePayload struct {
	ResponderProfile      *SupportResponderProfile `json:"responder_profile,omitempty"`
	ActiveRequest         *SupportRequest          `json:"active_request,omitempty"`
	PendingOfferCount     int                      `json:"pending_offer_count"`
	ActiveSessionCount    int                      `json:"active_session_count"`
	CommunityRequestCount int                      `json:"community_request_count"`
}

type UpdateSupportResponderProfileInput struct {
	IsAvailableForImmediate bool
	IsAvailableForCommunity bool
	SupportsChat            bool
	SupportsCheckIns        bool
	SupportsInPerson        bool
	MaxConcurrentSessions   int
	Languages               []string
	AvailableNow            bool
	IsActive                bool
}
