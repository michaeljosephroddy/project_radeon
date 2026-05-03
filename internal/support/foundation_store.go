package support

import (
	"context"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
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

	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback(ctx)

	supportGroupID, err := ensureCommunitySupportGroupTx(ctx, tx, userID)
	if err != nil {
		return nil, err
	}

	var req SupportRequest
	err = tx.QueryRow(ctx,
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

	var groupPostID uuid.UUID
	if err := tx.QueryRow(ctx, `
		INSERT INTO group_posts (group_id, user_id, post_type, body, anonymous, support_request_id)
		VALUES ($1, $2, 'need_support', LEFT(COALESCE(NULLIF(BTRIM($3::text), ''), 'Support request'), 4000), FALSE, $4)
		RETURNING id`,
		supportGroupID,
		userID,
		input.Message,
		req.ID,
	).Scan(&groupPostID); err != nil {
		return nil, err
	}
	if _, err := tx.Exec(ctx, `
		UPDATE support_requests
		SET group_post_id = $2
		WHERE id = $1`,
		req.ID,
		groupPostID,
	); err != nil {
		return nil, err
	}
	if _, err := tx.Exec(ctx, `
		UPDATE groups
		SET post_count = post_count + 1, updated_at = NOW()
		WHERE id = $1`,
		supportGroupID,
	); err != nil {
		return nil, err
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, err
	}

	setSupportRequestLocation(&req, locationVisibility, locationCity, locationRegion, locationCountry, locationApproxLat, locationApproxLng)
	req.GroupPostID = &groupPostID
	req.ResponseCount = 0
	req.OfferCount = 0
	req.HasResponded = false
	req.HasOffered = false
	req.HasReplied = false
	req.IsOwnRequest = true
	req.SortAt = req.CreatedAt

	return &req, nil
}

func ensureCommunitySupportGroupTx(ctx context.Context, tx pgx.Tx, userID uuid.UUID) (uuid.UUID, error) {
	var groupID uuid.UUID
	err := tx.QueryRow(ctx, `
		WITH owner_user AS (
			SELECT id
			FROM users
			ORDER BY created_at ASC, id ASC
			LIMIT 1
		),
		upserted AS (
			INSERT INTO groups (
				owner_id,
				name,
				slug,
				description,
				rules,
				visibility,
				posting_permission,
				allow_anonymous_posts,
				tags,
				recovery_pathways,
				is_system,
				system_key,
				locked_settings
			)
			VALUES (
				COALESCE((SELECT id FROM owner_user), $1),
				'Community Support',
				'system-community-support',
				'A community-wide support space for help requests and peer replies.',
				'Be kind, stay recovery-focused, and use private offers only when you can genuinely help.',
				'public',
				'members',
				FALSE,
				ARRAY['support', 'community'],
				ARRAY[]::TEXT[],
				TRUE,
				'community_support',
				TRUE
			)
			ON CONFLICT (system_key) WHERE system_key IS NOT NULL
			DO UPDATE SET
				name = EXCLUDED.name,
				description = EXCLUDED.description,
				rules = EXCLUDED.rules,
				visibility = 'public',
				posting_permission = 'members',
				is_system = TRUE,
				locked_settings = TRUE,
				deleted_at = NULL,
				updated_at = NOW()
			RETURNING id
		)
		SELECT id FROM upserted`,
		userID,
	).Scan(&groupID)
	if err != nil {
		return uuid.Nil, err
	}

	if _, err := tx.Exec(ctx, `
		INSERT INTO group_memberships (group_id, user_id, role, status, joined_at)
		VALUES ($1, $2, 'member', 'active', NOW())
		ON CONFLICT (group_id, user_id) DO UPDATE
		SET status = 'active',
			role = 'member',
			joined_at = COALESCE(group_memberships.joined_at, NOW()),
			updated_at = NOW()`,
		groupID,
		userID,
	); err != nil {
		return uuid.Nil, err
	}

	if _, err := tx.Exec(ctx, `
		UPDATE groups
		SET member_count = (
				SELECT COUNT(*)
				FROM group_memberships
				WHERE group_id = $1
					AND status = 'active'
			),
			updated_at = NOW()
		WHERE id = $1`,
		groupID,
	); err != nil {
		return uuid.Nil, err
	}

	return groupID, nil
}
