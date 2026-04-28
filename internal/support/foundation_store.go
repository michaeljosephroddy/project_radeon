package support

import (
	"context"

	"github.com/google/uuid"
)

func (s *pgStore) CreateImmediateSupportRequest(
	ctx context.Context,
	userID uuid.UUID,
	reqType string,
	message *string,
	urgency string,
	privacyLevel string,
) (*SupportRequest, error) {
	return s.createChannelSupportRequest(ctx, userID, SupportChannelImmediate, reqType, message, urgency, privacyLevel)
}

func (s *pgStore) CreateCommunitySupportRequest(
	ctx context.Context,
	userID uuid.UUID,
	reqType string,
	message *string,
	urgency string,
	privacyLevel string,
) (*SupportRequest, error) {
	return s.createChannelSupportRequest(ctx, userID, SupportChannelCommunity, reqType, message, urgency, privacyLevel)
}

func (s *pgStore) createChannelSupportRequest(
	ctx context.Context,
	userID uuid.UUID,
	channel SupportChannel,
	reqType string,
	message *string,
	urgency string,
	privacyLevel string,
) (*SupportRequest, error) {
	var req SupportRequest
	err := s.pool.QueryRow(ctx,
		`WITH inserted AS (
			INSERT INTO support_requests (
				requester_id,
				type,
				message,
				city,
				status,
				urgency,
				channel,
				privacy_level
			)
			SELECT
				u.id,
				$2,
				$3,
				u.city,
				'open',
				$4,
				$5,
				$6
			FROM users u
			WHERE u.id = $1
			RETURNING
				id,
				requester_id,
				type,
				message,
				urgency,
				status,
				created_at,
				channel,
				privacy_level
		)
		SELECT
			i.id,
			i.requester_id,
			i.type,
			i.message,
			i.urgency,
			i.status,
			i.created_at,
			i.channel,
			i.privacy_level,
			u.username,
			u.avatar_url,
			u.city
		FROM inserted i
		JOIN users u ON u.id = i.requester_id`,
		userID,
		reqType,
		message,
		urgency,
		channel,
		privacyLevel,
	).Scan(
		&req.ID,
		&req.RequesterID,
		&req.Type,
		&req.Message,
		&req.Urgency,
		&req.Status,
		&req.CreatedAt,
		&req.Channel,
		&req.PrivacyLevel,
		&req.Username,
		&req.AvatarURL,
		&req.City,
	)
	if err != nil {
		return nil, err
	}

	req.ResponseCount = 0
	req.HasResponded = false
	req.IsOwnRequest = true
	req.SortAt = req.CreatedAt

	return &req, nil
}
