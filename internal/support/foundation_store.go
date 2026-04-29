package support

import (
	"context"

	"github.com/google/uuid"
)

func (s *pgStore) CreateSupportRequest(ctx context.Context, userID uuid.UUID, input CreateSupportRequestInput) (*SupportRequest, error) {
	input = normalizeCreateSupportRequestInput(input)

	locationVisibility := "hidden"
	var locationCity *string
	var locationRegion *string
	var locationCountry *string
	var locationApproxLat *float64
	var locationApproxLng *float64
	if input.Location != nil {
		locationVisibility = input.Location.Visibility
		locationCity = input.Location.City
		locationRegion = input.Location.Region
		locationCountry = input.Location.Country
		locationApproxLat = input.Location.ApproximateLat
		locationApproxLng = input.Location.ApproximateLng
	}

	var req SupportRequest
	err := s.pool.QueryRow(ctx,
		`WITH inserted AS (
			INSERT INTO support_requests (
				requester_id,
				type,
				support_type,
				message,
				city,
				status,
				urgency,
				channel,
				privacy_level,
				topics,
				preferred_gender,
				location_visibility,
				location_city,
				location_region,
				location_country,
				location_approx_lat,
				location_approx_lng
			)
			SELECT
				u.id,
				$2,
				$2,
				$3,
				COALESCE($8, u.city),
				'open',
				$4,
				$5,
				$6,
				$7,
				$9,
				$10,
				$8,
				$11,
				$12,
				$13,
				$14
			FROM users u
			WHERE u.id = $1
				RETURNING
					id,
					requester_id,
					support_type,
					message,
					urgency,
					status,
					created_at,
					privacy_level,
					topics,
					preferred_gender,
				location_visibility,
				location_city,
				location_region,
				location_country,
				location_approx_lat,
				location_approx_lng
		)
			SELECT
				i.id,
				i.requester_id,
				i.support_type,
				i.message,
				i.urgency,
				i.status,
				i.created_at,
				i.privacy_level,
				i.topics,
				i.preferred_gender,
			i.location_visibility,
			i.location_city,
			i.location_region,
			i.location_country,
			i.location_approx_lat,
			i.location_approx_lng,
			u.username,
			u.avatar_url,
			u.city
		FROM inserted i
		JOIN users u ON u.id = i.requester_id`,
		userID,
		input.SupportType,
		input.Message,
		input.Urgency,
		"community",
		input.PrivacyLevel,
		input.Topics,
		locationCity,
		input.PreferredGender,
		locationVisibility,
		locationRegion,
		locationCountry,
		locationApproxLat,
		locationApproxLng,
	).Scan(
		&req.ID,
		&req.RequesterID,
		&req.SupportType,
		&req.Message,
		&req.Urgency,
		&req.Status,
		&req.CreatedAt,
		&req.PrivacyLevel,
		&req.Topics,
		&req.PreferredGender,
		&locationVisibility,
		&locationCity,
		&locationRegion,
		&locationCountry,
		&locationApproxLat,
		&locationApproxLng,
		&req.Username,
		&req.AvatarURL,
		&req.City,
	)
	if err != nil {
		return nil, err
	}

	setSupportRequestLocation(&req, locationVisibility, locationCity, locationRegion, locationCountry, locationApproxLat, locationApproxLng)
	req.ResponseCount = 0
	req.OfferCount = 0
	req.HasResponded = false
	req.HasOffered = false
	req.HasReplied = false
	req.IsOwnRequest = true
	req.SortAt = req.CreatedAt

	return &req, nil
}
