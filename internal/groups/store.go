package groups

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"regexp"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
)

type Store interface {
	CreateGroup(ctx context.Context, ownerID uuid.UUID, input CreateGroupInput) (*Group, error)
	ListGroups(ctx context.Context, viewerID uuid.UUID, params ListGroupsParams) ([]Group, error)
	GetGroup(ctx context.Context, viewerID, groupID uuid.UUID) (*Group, error)
	JoinGroup(ctx context.Context, viewerID, groupID uuid.UUID, message string) (*JoinGroupResult, error)
	LeaveGroup(ctx context.Context, viewerID, groupID uuid.UUID) error
	ListMembers(ctx context.Context, viewerID, groupID uuid.UUID, before *time.Time, limit int) ([]GroupMember, error)
	CreatePost(ctx context.Context, viewerID, groupID uuid.UUID, input CreateGroupPostInput) (*GroupPost, error)
	ListPosts(ctx context.Context, viewerID, groupID uuid.UUID, before *time.Time, limit int) ([]GroupPost, error)
	ListMedia(ctx context.Context, viewerID, groupID uuid.UUID, before *time.Time, limit int) ([]GroupMediaItem, error)
	CreateComment(ctx context.Context, viewerID, groupID, postID uuid.UUID, body string) (*GroupComment, error)
	ListComments(ctx context.Context, viewerID, groupID, postID uuid.UUID, after *time.Time, limit int) ([]GroupComment, error)
	ToggleReaction(ctx context.Context, viewerID, groupID, postID uuid.UUID) (*GroupPost, error)
	PinPost(ctx context.Context, viewerID, groupID, postID uuid.UUID, pinned bool) (*GroupPost, error)
	DeletePost(ctx context.Context, viewerID, groupID, postID uuid.UUID) error
	CreateInvite(ctx context.Context, viewerID, groupID uuid.UUID, input CreateGroupInviteInput) (*GroupInvite, error)
	AcceptInvite(ctx context.Context, viewerID uuid.UUID, token string) (*JoinGroupResult, error)
	ListJoinRequests(ctx context.Context, viewerID, groupID uuid.UUID) ([]GroupJoinRequest, error)
	ReviewJoinRequest(ctx context.Context, viewerID, groupID, requestID uuid.UUID, approve bool) (*GroupJoinRequest, error)
	ContactAdmins(ctx context.Context, viewerID, groupID uuid.UUID, subject, body string) (*GroupAdminThread, error)
	ListAdminThreads(ctx context.Context, viewerID, groupID uuid.UUID, before *time.Time, limit int) ([]GroupAdminThread, error)
	ReplyAdminThread(ctx context.Context, viewerID, groupID, threadID uuid.UUID, body string) (*GroupAdminMessage, error)
	ResolveAdminThread(ctx context.Context, viewerID, groupID, threadID uuid.UUID) (*GroupAdminThread, error)
	ReportTarget(ctx context.Context, viewerID, groupID uuid.UUID, targetType string, targetID *uuid.UUID, reason string, details *string) (*GroupReport, error)
}

type pgStore struct {
	pool *pgxpool.Pool
}

func NewPgStore(pool *pgxpool.Pool) Store {
	return &pgStore{pool: pool}
}

func (s *pgStore) CreateGroup(ctx context.Context, ownerID uuid.UUID, input CreateGroupInput) (*Group, error) {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback(ctx)

	slug := buildGroupSlug(input.Name)
	var groupID uuid.UUID
	if err := tx.QueryRow(ctx, `
		INSERT INTO groups (
			owner_id, name, slug, description, rules, avatar_url, cover_url,
			visibility, posting_permission, allow_anonymous_posts, city, country,
			tags, recovery_pathways, member_count
		)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14, 1)
		RETURNING id`,
		ownerID,
		input.Name,
		slug,
		input.Description,
		input.Rules,
		input.AvatarURL,
		input.CoverURL,
		input.Visibility,
		input.PostingPermission,
		input.AllowAnonymousPosts,
		input.City,
		input.Country,
		input.Tags,
		input.RecoveryPathways,
	).Scan(&groupID); err != nil {
		return nil, err
	}

	if _, err := tx.Exec(ctx, `
		INSERT INTO group_memberships (group_id, user_id, role, status, joined_at)
		VALUES ($1, $2, 'owner', 'active', NOW())`,
		groupID,
		ownerID,
	); err != nil {
		return nil, err
	}

	if _, err := tx.Exec(ctx, `
		INSERT INTO group_audit_events (group_id, actor_id, event_type, target_type, target_id)
		VALUES ($1, $2, 'group_created', 'group', $1)`,
		groupID,
		ownerID,
	); err != nil {
		return nil, err
	}

	if err := tx.Commit(ctx); err != nil {
		return nil, err
	}
	return s.GetGroup(ctx, ownerID, groupID)
}

func (s *pgStore) ListGroups(ctx context.Context, viewerID uuid.UUID, params ListGroupsParams) ([]Group, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT
			g.id, g.owner_id, g.name, g.slug, g.description, g.rules, g.avatar_url, g.cover_url,
			g.visibility, g.posting_permission, g.allow_anonymous_posts, g.city, g.country,
			g.tags, g.recovery_pathways, g.member_count, g.post_count, g.media_count,
			g.pending_request_count, g.created_at, g.updated_at,
			gm.role, gm.status,
			EXISTS (
				SELECT 1
				FROM group_join_requests gjr
				WHERE gjr.group_id = g.id
					AND gjr.user_id = $1
					AND gjr.status = 'pending'
			) AS has_pending_request
		FROM groups g
		LEFT JOIN group_memberships gm
			ON gm.group_id = g.id
			AND gm.user_id = $1
		WHERE g.deleted_at IS NULL
			AND (
				($2 = 'joined' AND gm.status = 'active')
				OR ($2 <> 'joined' AND (g.visibility IN ('public', 'approval_required') OR gm.status = 'active'))
			)
			AND ($3 = '' OR g.name ILIKE '%' || $3 || '%' OR COALESCE(g.description, '') ILIKE '%' || $3 || '%')
			AND ($4 = '' OR COALESCE(g.city, '') ILIKE $4)
			AND ($5 = '' OR COALESCE(g.country, '') ILIKE $5)
			AND ($6 = '' OR $6 = ANY(g.tags))
			AND ($7 = '' OR $7 = ANY(g.recovery_pathways))
			AND ($8::timestamptz IS NULL OR g.created_at < $8)
		ORDER BY g.created_at DESC
		LIMIT $9`,
		viewerID,
		normalizeMemberScope(params.MemberScope),
		strings.TrimSpace(params.Query),
		strings.TrimSpace(params.City),
		strings.TrimSpace(params.Country),
		strings.TrimSpace(params.Tag),
		strings.TrimSpace(params.RecoveryPathway),
		params.Before,
		params.Limit,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	groups := make([]Group, 0, params.Limit)
	for rows.Next() {
		group, err := scanGroup(rows)
		if err != nil {
			return nil, err
		}
		groups = append(groups, *group)
	}
	return groups, rows.Err()
}

func (s *pgStore) GetGroup(ctx context.Context, viewerID, groupID uuid.UUID) (*Group, error) {
	row := s.pool.QueryRow(ctx, `
		SELECT
			g.id, g.owner_id, g.name, g.slug, g.description, g.rules, g.avatar_url, g.cover_url,
			g.visibility, g.posting_permission, g.allow_anonymous_posts, g.city, g.country,
			g.tags, g.recovery_pathways, g.member_count, g.post_count, g.media_count,
			g.pending_request_count, g.created_at, g.updated_at,
			gm.role, gm.status,
			EXISTS (
				SELECT 1
				FROM group_join_requests gjr
				WHERE gjr.group_id = g.id
					AND gjr.user_id = $1
					AND gjr.status = 'pending'
			) AS has_pending_request
		FROM groups g
		LEFT JOIN group_memberships gm
			ON gm.group_id = g.id
			AND gm.user_id = $1
		WHERE g.id = $2
			AND g.deleted_at IS NULL
			AND (g.visibility IN ('public', 'approval_required') OR gm.status = 'active')`,
		viewerID,
		groupID,
	)
	return scanGroup(row)
}

func (s *pgStore) JoinGroup(ctx context.Context, viewerID, groupID uuid.UUID, message string) (*JoinGroupResult, error) {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback(ctx)

	var visibility GroupVisibility
	var role *GroupRole
	var status *MembershipStatus
	err = tx.QueryRow(ctx, `
		SELECT g.visibility, gm.role, gm.status
		FROM groups g
		LEFT JOIN group_memberships gm
			ON gm.group_id = g.id
			AND gm.user_id = $1
		WHERE g.id = $2
			AND g.deleted_at IS NULL
		FOR UPDATE OF g`,
		viewerID,
		groupID,
	).Scan(&visibility, &role, &status)
	if err != nil {
		if err == pgx.ErrNoRows {
			return nil, ErrNotFound
		}
		return nil, err
	}
	if status != nil && *status == MembershipStatusBanned {
		return nil, ErrForbidden
	}
	if status != nil && *status == MembershipStatusActive {
		_ = tx.Rollback(ctx)
		group, err := s.GetGroup(ctx, viewerID, groupID)
		if err != nil {
			return nil, err
		}
		return &JoinGroupResult{State: "member", Group: group}, nil
	}

	switch visibility {
	case GroupVisibilityPublic:
		tag, err := tx.Exec(ctx, `
			INSERT INTO group_memberships (group_id, user_id, role, status, joined_at)
			VALUES ($1, $2, 'member', 'active', NOW())
			ON CONFLICT (group_id, user_id) DO NOTHING`,
			groupID,
			viewerID,
		)
		if err != nil {
			return nil, err
		}
		if tag.RowsAffected() > 0 {
			if _, err := tx.Exec(ctx, `
				UPDATE groups
				SET member_count = member_count + 1, updated_at = NOW()
				WHERE id = $1`,
				groupID,
			); err != nil {
				return nil, err
			}
		}
		if err := tx.Commit(ctx); err != nil {
			return nil, err
		}
		group, err := s.GetGroup(ctx, viewerID, groupID)
		if err != nil {
			return nil, err
		}
		return &JoinGroupResult{State: "member", Group: group}, nil
	case GroupVisibilityApprovalRequired:
		tag, err := tx.Exec(ctx, `
			INSERT INTO group_join_requests (group_id, user_id, message)
			VALUES ($1, $2, NULLIF($3, ''))
			ON CONFLICT (group_id, user_id) WHERE status = 'pending'
			DO UPDATE SET message = EXCLUDED.message, updated_at = NOW()`,
			groupID,
			viewerID,
			strings.TrimSpace(message),
		)
		if err != nil {
			return nil, err
		}
		if tag.RowsAffected() > 0 {
			if _, err := tx.Exec(ctx, `
				UPDATE groups
				SET pending_request_count = (
					SELECT COUNT(*)
					FROM group_join_requests
					WHERE group_id = $1 AND status = 'pending'
				), updated_at = NOW()
				WHERE id = $1`,
				groupID,
			); err != nil {
				return nil, err
			}
		}
		if err := tx.Commit(ctx); err != nil {
			return nil, err
		}
		return &JoinGroupResult{State: "pending"}, nil
	default:
		return nil, ErrInviteRequired
	}
}

func (s *pgStore) LeaveGroup(ctx context.Context, viewerID, groupID uuid.UUID) error {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)

	var role GroupRole
	var status MembershipStatus
	err = tx.QueryRow(ctx, `
		SELECT role, status
		FROM group_memberships
		WHERE group_id = $1 AND user_id = $2
		FOR UPDATE`,
		groupID,
		viewerID,
	).Scan(&role, &status)
	if err != nil {
		if err == pgx.ErrNoRows {
			return ErrNotFound
		}
		return err
	}
	if status != MembershipStatusActive {
		return ErrNotFound
	}
	if role == GroupRoleOwner {
		return ErrOwnerCannotLeave
	}

	if _, err := tx.Exec(ctx, `
		DELETE FROM group_memberships
		WHERE group_id = $1 AND user_id = $2`,
		groupID,
		viewerID,
	); err != nil {
		return err
	}
	if _, err := tx.Exec(ctx, `
		UPDATE groups
		SET member_count = GREATEST(member_count - 1, 0), updated_at = NOW()
		WHERE id = $1`,
		groupID,
	); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

func (s *pgStore) ListMembers(ctx context.Context, viewerID, groupID uuid.UUID, before *time.Time, limit int) ([]GroupMember, error) {
	if _, err := s.GetGroup(ctx, viewerID, groupID); err != nil {
		return nil, err
	}
	rows, err := s.pool.Query(ctx, `
		SELECT gm.user_id, u.username, u.avatar_url, gm.role, gm.status, gm.joined_at, gm.created_at, gm.updated_at
		FROM group_memberships gm
		JOIN users u ON u.id = gm.user_id
		WHERE gm.group_id = $1
			AND gm.status = 'active'
			AND ($2::timestamptz IS NULL OR gm.joined_at < $2)
		ORDER BY
			CASE gm.role
				WHEN 'owner' THEN 0
				WHEN 'admin' THEN 1
				WHEN 'moderator' THEN 2
				ELSE 3
			END ASC,
			gm.joined_at DESC
		LIMIT $3`,
		groupID,
		before,
		limit,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	members := make([]GroupMember, 0, limit)
	for rows.Next() {
		var member GroupMember
		if err := rows.Scan(
			&member.UserID,
			&member.Username,
			&member.AvatarURL,
			&member.Role,
			&member.Status,
			&member.JoinedAt,
			&member.CreatedAt,
			&member.UpdatedAt,
		); err != nil {
			return nil, err
		}
		members = append(members, member)
	}
	return members, rows.Err()
}

func (s *pgStore) CreatePost(ctx context.Context, viewerID, groupID uuid.UUID, input CreateGroupPostInput) (*GroupPost, error) {
	role, err := s.requireActiveMembership(ctx, viewerID, groupID)
	if err != nil {
		return nil, err
	}
	if input.PostType == GroupPostTypeAdminAnnouncement && !canModerate(role) {
		return nil, ErrForbidden
	}

	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback(ctx)

	var postID uuid.UUID
	if err := tx.QueryRow(ctx, `
		INSERT INTO group_posts (group_id, user_id, post_type, body, anonymous, image_count)
		VALUES ($1, $2, $3, $4, $5, $6)
		RETURNING id`,
		groupID,
		viewerID,
		input.PostType,
		input.Body,
		input.Anonymous,
		len(input.Images),
	).Scan(&postID); err != nil {
		return nil, err
	}

	for index, image := range input.Images {
		if _, err := tx.Exec(ctx, `
			INSERT INTO group_post_images (group_id, post_id, image_url, thumb_url, width, height, position)
			VALUES ($1, $2, $3, $4, $5, $6, $7)`,
			groupID,
			postID,
			image.ImageURL,
			image.ThumbURL,
			image.Width,
			image.Height,
			index,
		); err != nil {
			return nil, err
		}
	}

	if _, err := tx.Exec(ctx, `
		UPDATE groups
		SET post_count = post_count + 1,
			media_count = media_count + $2,
			updated_at = NOW()
		WHERE id = $1`,
		groupID,
		len(input.Images),
	); err != nil {
		return nil, err
	}

	if err := tx.Commit(ctx); err != nil {
		return nil, err
	}
	return s.getPost(ctx, viewerID, groupID, postID)
}

func (s *pgStore) ListPosts(ctx context.Context, viewerID, groupID uuid.UUID, before *time.Time, limit int) ([]GroupPost, error) {
	if _, err := s.requireActiveMembership(ctx, viewerID, groupID); err != nil {
		return nil, err
	}
	rows, err := s.pool.Query(ctx, `
		SELECT
			gp.id, gp.group_id, gp.user_id, u.username, u.avatar_url, gp.post_type, gp.body,
			gp.anonymous, gp.pinned_at, gp.pinned_by, gp.comment_count, gp.reaction_count,
			gp.image_count,
			EXISTS (
				SELECT 1 FROM group_reactions gr
				WHERE gr.post_id = gp.id AND gr.user_id = $1 AND gr.type = 'like'
			) AS viewer_has_reacted,
			gp.created_at, gp.updated_at
		FROM group_posts gp
		JOIN users u ON u.id = gp.user_id
		WHERE gp.group_id = $2
			AND gp.deleted_at IS NULL
			AND ($3::timestamptz IS NULL OR gp.created_at < $3)
		ORDER BY gp.pinned_at DESC NULLS LAST, gp.created_at DESC
		LIMIT $4`,
		viewerID,
		groupID,
		before,
		limit,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	posts := make([]GroupPost, 0, limit)
	postIDs := make([]uuid.UUID, 0, limit)
	for rows.Next() {
		post, err := scanPost(rows)
		if err != nil {
			return nil, err
		}
		posts = append(posts, *post)
		postIDs = append(postIDs, post.ID)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	if err := s.attachImages(ctx, posts, postIDs); err != nil {
		return nil, err
	}
	return posts, nil
}

func (s *pgStore) ListMedia(ctx context.Context, viewerID, groupID uuid.UUID, before *time.Time, limit int) ([]GroupMediaItem, error) {
	if _, err := s.requireActiveMembership(ctx, viewerID, groupID); err != nil {
		return nil, err
	}
	rows, err := s.pool.Query(ctx, `
		SELECT gpi.id, gpi.group_id, gpi.post_id, gpi.image_url, gpi.thumb_url, gpi.width, gpi.height, gpi.created_at
		FROM group_post_images gpi
		JOIN group_posts gp ON gp.id = gpi.post_id
		WHERE gpi.group_id = $1
			AND gp.deleted_at IS NULL
			AND ($2::timestamptz IS NULL OR gpi.created_at < $2)
		ORDER BY gpi.created_at DESC
		LIMIT $3`,
		groupID,
		before,
		limit,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	items := make([]GroupMediaItem, 0, limit)
	for rows.Next() {
		var item GroupMediaItem
		if err := rows.Scan(&item.ID, &item.GroupID, &item.PostID, &item.ImageURL, &item.ThumbURL, &item.Width, &item.Height, &item.CreatedAt); err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	return items, rows.Err()
}

func (s *pgStore) CreateComment(ctx context.Context, viewerID, groupID, postID uuid.UUID, body string) (*GroupComment, error) {
	if _, err := s.requireActiveMembership(ctx, viewerID, groupID); err != nil {
		return nil, err
	}
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback(ctx)

	var exists bool
	if err := tx.QueryRow(ctx, `
		SELECT EXISTS (
			SELECT 1 FROM group_posts
			WHERE id = $1 AND group_id = $2 AND deleted_at IS NULL
		)`,
		postID,
		groupID,
	).Scan(&exists); err != nil {
		return nil, err
	}
	if !exists {
		return nil, ErrNotFound
	}

	var commentID uuid.UUID
	if err := tx.QueryRow(ctx, `
		INSERT INTO group_comments (group_id, post_id, user_id, body)
		VALUES ($1, $2, $3, $4)
		RETURNING id`,
		groupID,
		postID,
		viewerID,
		body,
	).Scan(&commentID); err != nil {
		return nil, err
	}
	if _, err := tx.Exec(ctx, `
		UPDATE group_posts
		SET comment_count = comment_count + 1, updated_at = NOW()
		WHERE id = $1`,
		postID,
	); err != nil {
		return nil, err
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, err
	}
	return s.getComment(ctx, groupID, commentID)
}

func (s *pgStore) ListComments(ctx context.Context, viewerID, groupID, postID uuid.UUID, after *time.Time, limit int) ([]GroupComment, error) {
	if _, err := s.requireActiveMembership(ctx, viewerID, groupID); err != nil {
		return nil, err
	}
	rows, err := s.pool.Query(ctx, `
		SELECT gc.id, gc.group_id, gc.post_id, gc.user_id, u.username, u.avatar_url, gc.body, gc.created_at, gc.updated_at
		FROM group_comments gc
		JOIN users u ON u.id = gc.user_id
		WHERE gc.group_id = $1
			AND gc.post_id = $2
			AND gc.deleted_at IS NULL
			AND ($3::timestamptz IS NULL OR gc.created_at > $3)
		ORDER BY gc.created_at ASC
		LIMIT $4`,
		groupID,
		postID,
		after,
		limit,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	comments := make([]GroupComment, 0, limit)
	for rows.Next() {
		comment, err := scanComment(rows)
		if err != nil {
			return nil, err
		}
		comments = append(comments, *comment)
	}
	return comments, rows.Err()
}

func (s *pgStore) ToggleReaction(ctx context.Context, viewerID, groupID, postID uuid.UUID) (*GroupPost, error) {
	if _, err := s.requireActiveMembership(ctx, viewerID, groupID); err != nil {
		return nil, err
	}
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback(ctx)

	var exists bool
	if err := tx.QueryRow(ctx, `
		SELECT EXISTS (
			SELECT 1 FROM group_posts
			WHERE id = $1 AND group_id = $2 AND deleted_at IS NULL
		)`,
		postID,
		groupID,
	).Scan(&exists); err != nil {
		return nil, err
	}
	if !exists {
		return nil, ErrNotFound
	}

	tag, err := tx.Exec(ctx, `
		DELETE FROM group_reactions
		WHERE post_id = $1 AND user_id = $2 AND type = 'like'`,
		postID,
		viewerID,
	)
	if err != nil {
		return nil, err
	}
	delta := -1
	if tag.RowsAffected() == 0 {
		if _, err := tx.Exec(ctx, `
			INSERT INTO group_reactions (group_id, post_id, user_id, type)
			VALUES ($1, $2, $3, 'like')`,
			groupID,
			postID,
			viewerID,
		); err != nil {
			return nil, err
		}
		delta = 1
	}
	if _, err := tx.Exec(ctx, `
		UPDATE group_posts
		SET reaction_count = GREATEST(reaction_count + $2, 0), updated_at = NOW()
		WHERE id = $1`,
		postID,
		delta,
	); err != nil {
		return nil, err
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, err
	}
	return s.getPost(ctx, viewerID, groupID, postID)
}

func (s *pgStore) PinPost(ctx context.Context, viewerID, groupID, postID uuid.UUID, pinned bool) (*GroupPost, error) {
	role, err := s.requireActiveMembership(ctx, viewerID, groupID)
	if err != nil {
		return nil, err
	}
	if !canModerate(role) {
		return nil, ErrForbidden
	}

	var tag pgconn.CommandTag
	if pinned {
		tag, err = s.pool.Exec(ctx, `
			UPDATE group_posts
			SET pinned_at = NOW(), pinned_by = $1, updated_at = NOW()
			WHERE id = $2 AND group_id = $3 AND deleted_at IS NULL`,
			viewerID,
			postID,
			groupID,
		)
	} else {
		tag, err = s.pool.Exec(ctx, `
			UPDATE group_posts
			SET pinned_at = NULL, pinned_by = NULL, updated_at = NOW()
			WHERE id = $1 AND group_id = $2 AND deleted_at IS NULL`,
			postID,
			groupID,
		)
	}
	if err != nil {
		return nil, err
	}
	if tag.RowsAffected() == 0 {
		return nil, ErrNotFound
	}
	return s.getPost(ctx, viewerID, groupID, postID)
}

func (s *pgStore) DeletePost(ctx context.Context, viewerID, groupID, postID uuid.UUID) error {
	role, err := s.requireActiveMembership(ctx, viewerID, groupID)
	if err != nil {
		return err
	}

	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)

	var authorID uuid.UUID
	var imageCount int
	if err := tx.QueryRow(ctx, `
		SELECT user_id, image_count
		FROM group_posts
		WHERE id = $1 AND group_id = $2 AND deleted_at IS NULL
		FOR UPDATE`,
		postID,
		groupID,
	).Scan(&authorID, &imageCount); err != nil {
		if err == pgx.ErrNoRows {
			return ErrNotFound
		}
		return err
	}
	if authorID != viewerID && !canModerate(role) {
		return ErrForbidden
	}
	tag, err := tx.Exec(ctx, `
		UPDATE group_posts
		SET deleted_at = NOW(), updated_at = NOW(), pinned_at = NULL, pinned_by = NULL
		WHERE id = $1 AND deleted_at IS NULL`,
		postID,
	)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	if _, err := tx.Exec(ctx, `
		UPDATE groups
		SET post_count = GREATEST(post_count - 1, 0),
			media_count = GREATEST(media_count - $2, 0),
			updated_at = NOW()
		WHERE id = $1`,
		groupID,
		imageCount,
	); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

func (s *pgStore) CreateInvite(ctx context.Context, viewerID, groupID uuid.UUID, input CreateGroupInviteInput) (*GroupInvite, error) {
	role, err := s.requireActiveMembership(ctx, viewerID, groupID)
	if err != nil {
		return nil, err
	}
	if role == GroupRoleMember {
		return nil, ErrForbidden
	}
	token, err := generateInviteToken()
	if err != nil {
		return nil, err
	}
	tokenHash := hashInviteToken(token)
	var invite GroupInvite
	if err := s.pool.QueryRow(ctx, `
		INSERT INTO group_invites (group_id, token_hash, created_by, expires_at, max_uses, requires_approval)
		VALUES ($1, $2, $3, $4, $5, $6)
		RETURNING id, group_id, expires_at, max_uses, use_count, requires_approval, revoked_at, created_at`,
		groupID,
		tokenHash,
		viewerID,
		input.ExpiresAt,
		input.MaxUses,
		input.RequiresApproval,
	).Scan(
		&invite.ID,
		&invite.GroupID,
		&invite.ExpiresAt,
		&invite.MaxUses,
		&invite.UseCount,
		&invite.RequiresApproval,
		&invite.RevokedAt,
		&invite.CreatedAt,
	); err != nil {
		return nil, err
	}
	invite.Token = token
	return &invite, nil
}

func (s *pgStore) AcceptInvite(ctx context.Context, viewerID uuid.UUID, token string) (*JoinGroupResult, error) {
	tokenHash := hashInviteToken(token)
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback(ctx)

	var invite GroupInvite
	err = tx.QueryRow(ctx, `
		SELECT id, group_id, expires_at, max_uses, use_count, requires_approval, revoked_at, created_at
		FROM group_invites
		WHERE token_hash = $1
		FOR UPDATE`,
		tokenHash,
	).Scan(&invite.ID, &invite.GroupID, &invite.ExpiresAt, &invite.MaxUses, &invite.UseCount, &invite.RequiresApproval, &invite.RevokedAt, &invite.CreatedAt)
	if err != nil {
		if err == pgx.ErrNoRows {
			return nil, ErrNotFound
		}
		return nil, err
	}
	if invite.RevokedAt != nil || (invite.ExpiresAt != nil && time.Now().UTC().After(*invite.ExpiresAt)) {
		return nil, ErrNotFound
	}
	if invite.MaxUses != nil && invite.UseCount >= *invite.MaxUses {
		return nil, ErrNotFound
	}

	var existingStatus MembershipStatus
	var hasExistingStatus bool
	if err := tx.QueryRow(ctx, `
		SELECT status
		FROM group_memberships
		WHERE group_id = $1 AND user_id = $2`,
		invite.GroupID,
		viewerID,
	).Scan(&existingStatus); err != nil {
		if err != pgx.ErrNoRows {
			return nil, err
		}
	} else {
		hasExistingStatus = true
	}
	if hasExistingStatus && existingStatus == MembershipStatusBanned {
		return nil, ErrForbidden
	}
	if hasExistingStatus && existingStatus == MembershipStatusActive {
		return &JoinGroupResult{State: "member"}, nil
	}

	if invite.RequiresApproval {
		if _, err := tx.Exec(ctx, `
			INSERT INTO group_join_requests (group_id, user_id, message)
			VALUES ($1, $2, 'Joined from invite link')
			ON CONFLICT (group_id, user_id) WHERE status = 'pending'
			DO UPDATE SET updated_at = NOW()`,
			invite.GroupID,
			viewerID,
		); err != nil {
			return nil, err
		}
		if _, err := tx.Exec(ctx, `
			UPDATE groups
			SET pending_request_count = (
				SELECT COUNT(*) FROM group_join_requests WHERE group_id = $1 AND status = 'pending'
			), updated_at = NOW()
			WHERE id = $1`,
			invite.GroupID,
		); err != nil {
			return nil, err
		}
	} else {
		if _, err := tx.Exec(ctx, `
			INSERT INTO group_memberships (group_id, user_id, role, status, invited_by, joined_at)
			SELECT $1, $2, 'member', 'active', created_by, NOW()
			FROM group_invites
			WHERE id = $3
			ON CONFLICT (group_id, user_id) DO NOTHING`,
			invite.GroupID,
			viewerID,
			invite.ID,
		); err != nil {
			return nil, err
		}
		if _, err := tx.Exec(ctx, `
			UPDATE groups
			SET member_count = member_count + 1, updated_at = NOW()
			WHERE id = $1`,
			invite.GroupID,
		); err != nil {
			return nil, err
		}
	}
	if _, err := tx.Exec(ctx, `
		UPDATE group_invites
		SET use_count = use_count + 1
		WHERE id = $1`,
		invite.ID,
	); err != nil {
		return nil, err
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, err
	}
	if invite.RequiresApproval {
		return &JoinGroupResult{State: "pending"}, nil
	}
	group, err := s.GetGroup(ctx, viewerID, invite.GroupID)
	if err != nil {
		return nil, err
	}
	return &JoinGroupResult{State: "member", Group: group}, nil
}

func (s *pgStore) ListJoinRequests(ctx context.Context, viewerID, groupID uuid.UUID) ([]GroupJoinRequest, error) {
	role, err := s.requireActiveMembership(ctx, viewerID, groupID)
	if err != nil {
		return nil, err
	}
	if !canModerate(role) {
		return nil, ErrForbidden
	}
	rows, err := s.pool.Query(ctx, `
		SELECT gjr.id, gjr.group_id, gjr.user_id, u.username, u.avatar_url, gjr.message,
			gjr.status, gjr.reviewed_by, gjr.reviewed_at, gjr.created_at, gjr.updated_at
		FROM group_join_requests gjr
		JOIN users u ON u.id = gjr.user_id
		WHERE gjr.group_id = $1 AND gjr.status = 'pending'
		ORDER BY gjr.created_at ASC`,
		groupID,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	requests := []GroupJoinRequest{}
	for rows.Next() {
		request, err := scanJoinRequest(rows)
		if err != nil {
			return nil, err
		}
		requests = append(requests, *request)
	}
	return requests, rows.Err()
}

func (s *pgStore) ReviewJoinRequest(ctx context.Context, viewerID, groupID, requestID uuid.UUID, approve bool) (*GroupJoinRequest, error) {
	role, err := s.requireActiveMembership(ctx, viewerID, groupID)
	if err != nil {
		return nil, err
	}
	if !canModerate(role) {
		return nil, ErrForbidden
	}
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback(ctx)

	status := "rejected"
	if approve {
		status = "approved"
	}
	var request GroupJoinRequest
	err = tx.QueryRow(ctx, `
		UPDATE group_join_requests
		SET status = $4, reviewed_by = $3, reviewed_at = NOW(), updated_at = NOW()
		WHERE id = $1 AND group_id = $2 AND status = 'pending'
		RETURNING id, group_id, user_id, ''::text AS username, NULL::text AS avatar_url, message,
			status, reviewed_by, reviewed_at, created_at, updated_at`,
		requestID,
		groupID,
		viewerID,
		status,
	).Scan(
		&request.ID,
		&request.GroupID,
		&request.UserID,
		&request.Username,
		&request.AvatarURL,
		&request.Message,
		&request.Status,
		&request.ReviewedBy,
		&request.ReviewedAt,
		&request.CreatedAt,
		&request.UpdatedAt,
	)
	if err != nil {
		if err == pgx.ErrNoRows {
			return nil, ErrNotFound
		}
		return nil, err
	}
	if approve {
		if _, err := tx.Exec(ctx, `
			INSERT INTO group_memberships (group_id, user_id, role, status, joined_at)
			VALUES ($1, $2, 'member', 'active', NOW())
			ON CONFLICT (group_id, user_id) DO NOTHING`,
			groupID,
			request.UserID,
		); err != nil {
			return nil, err
		}
		if _, err := tx.Exec(ctx, `
			UPDATE groups
			SET member_count = member_count + 1, updated_at = NOW()
			WHERE id = $1`,
			groupID,
		); err != nil {
			return nil, err
		}
	}
	if _, err := tx.Exec(ctx, `
		UPDATE groups
		SET pending_request_count = (
			SELECT COUNT(*) FROM group_join_requests WHERE group_id = $1 AND status = 'pending'
		), updated_at = NOW()
		WHERE id = $1`,
		groupID,
	); err != nil {
		return nil, err
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, err
	}
	return &request, nil
}

func (s *pgStore) ContactAdmins(ctx context.Context, viewerID, groupID uuid.UUID, subject, body string) (*GroupAdminThread, error) {
	if _, err := s.requireActiveMembership(ctx, viewerID, groupID); err != nil {
		return nil, err
	}
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback(ctx)

	var threadID uuid.UUID
	if err := tx.QueryRow(ctx, `
		INSERT INTO group_admin_threads (group_id, user_id, subject)
		VALUES ($1, $2, NULLIF($3, ''))
		RETURNING id`,
		groupID,
		viewerID,
		strings.TrimSpace(subject),
	).Scan(&threadID); err != nil {
		return nil, err
	}
	if _, err := tx.Exec(ctx, `
		INSERT INTO group_admin_messages (thread_id, sender_id, body)
		VALUES ($1, $2, $3)`,
		threadID,
		viewerID,
		body,
	); err != nil {
		return nil, err
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, err
	}
	return s.getAdminThread(ctx, groupID, threadID, true)
}

func (s *pgStore) ListAdminThreads(ctx context.Context, viewerID, groupID uuid.UUID, before *time.Time, limit int) ([]GroupAdminThread, error) {
	role, err := s.requireActiveMembership(ctx, viewerID, groupID)
	if err != nil {
		return nil, err
	}
	if !canModerate(role) {
		return nil, ErrForbidden
	}
	rows, err := s.pool.Query(ctx, `
		SELECT gat.id, gat.group_id, gat.user_id, u.username, u.avatar_url, gat.status, gat.subject, gat.created_at, gat.updated_at
		FROM group_admin_threads gat
		JOIN users u ON u.id = gat.user_id
		WHERE gat.group_id = $1
			AND ($2::timestamptz IS NULL OR gat.updated_at < $2)
		ORDER BY gat.updated_at DESC
		LIMIT $3`,
		groupID,
		before,
		limit,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	threads := []GroupAdminThread{}
	for rows.Next() {
		thread, err := scanAdminThread(rows)
		if err != nil {
			return nil, err
		}
		threads = append(threads, *thread)
	}
	return threads, rows.Err()
}

func (s *pgStore) ReplyAdminThread(ctx context.Context, viewerID, groupID, threadID uuid.UUID, body string) (*GroupAdminMessage, error) {
	role, err := s.requireActiveMembership(ctx, viewerID, groupID)
	if err != nil {
		return nil, err
	}
	if !canModerate(role) {
		return nil, ErrForbidden
	}
	var messageID uuid.UUID
	err = s.pool.QueryRow(ctx, `
		WITH updated AS (
			UPDATE group_admin_threads
			SET status = 'open', updated_at = NOW()
			WHERE id = $1 AND group_id = $2
			RETURNING id
		)
		INSERT INTO group_admin_messages (thread_id, sender_id, body)
		SELECT id, $3, $4 FROM updated
		RETURNING id`,
		threadID,
		groupID,
		viewerID,
		body,
	).Scan(&messageID)
	if err != nil {
		if err == pgx.ErrNoRows {
			return nil, ErrNotFound
		}
		return nil, err
	}
	return s.getAdminMessage(ctx, messageID)
}

func (s *pgStore) ResolveAdminThread(ctx context.Context, viewerID, groupID, threadID uuid.UUID) (*GroupAdminThread, error) {
	role, err := s.requireActiveMembership(ctx, viewerID, groupID)
	if err != nil {
		return nil, err
	}
	if !canModerate(role) {
		return nil, ErrForbidden
	}
	tag, err := s.pool.Exec(ctx, `
		UPDATE group_admin_threads
		SET status = 'resolved', updated_at = NOW()
		WHERE id = $1 AND group_id = $2`,
		threadID,
		groupID,
	)
	if err != nil {
		return nil, err
	}
	if tag.RowsAffected() == 0 {
		return nil, ErrNotFound
	}
	return s.getAdminThread(ctx, groupID, threadID, true)
}

func (s *pgStore) ReportTarget(ctx context.Context, viewerID, groupID uuid.UUID, targetType string, targetID *uuid.UUID, reason string, details *string) (*GroupReport, error) {
	if _, err := s.requireActiveMembership(ctx, viewerID, groupID); err != nil {
		return nil, err
	}
	var report GroupReport
	err := s.pool.QueryRow(ctx, `
		INSERT INTO group_reports (group_id, reporter_id, target_type, target_id, reason, details)
		VALUES ($1, $2, $3, $4, $5, $6)
		RETURNING id, group_id, reporter_id, target_type, target_id, reason, details, status, created_at`,
		groupID,
		viewerID,
		targetType,
		targetID,
		reason,
		details,
	).Scan(
		&report.ID,
		&report.GroupID,
		&report.ReporterID,
		&report.TargetType,
		&report.TargetID,
		&report.Reason,
		&report.Details,
		&report.Status,
		&report.CreatedAt,
	)
	if err != nil {
		return nil, err
	}
	return &report, nil
}

type groupScanner interface {
	Scan(dest ...any) error
}

func scanGroup(row groupScanner) (*Group, error) {
	var group Group
	var role *GroupRole
	var status *MembershipStatus
	err := row.Scan(
		&group.ID,
		&group.OwnerID,
		&group.Name,
		&group.Slug,
		&group.Description,
		&group.Rules,
		&group.AvatarURL,
		&group.CoverURL,
		&group.Visibility,
		&group.PostingPermission,
		&group.AllowAnonymousPosts,
		&group.City,
		&group.Country,
		&group.Tags,
		&group.RecoveryPathways,
		&group.MemberCount,
		&group.PostCount,
		&group.MediaCount,
		&group.PendingRequestCount,
		&group.CreatedAt,
		&group.UpdatedAt,
		&role,
		&status,
		&group.HasPendingRequest,
	)
	if err != nil {
		if err == pgx.ErrNoRows {
			return nil, ErrNotFound
		}
		return nil, err
	}
	group.ViewerRole = role
	group.ViewerStatus = status
	applyViewerPermissions(&group)
	return &group, nil
}

func (s *pgStore) requireActiveMembership(ctx context.Context, viewerID, groupID uuid.UUID) (GroupRole, error) {
	var role GroupRole
	var status MembershipStatus
	err := s.pool.QueryRow(ctx, `
		SELECT gm.role, gm.status
		FROM group_memberships gm
		JOIN groups g ON g.id = gm.group_id
		WHERE gm.group_id = $1
			AND gm.user_id = $2
			AND g.deleted_at IS NULL`,
		groupID,
		viewerID,
	).Scan(&role, &status)
	if err != nil {
		if err == pgx.ErrNoRows {
			return "", ErrNotFound
		}
		return "", err
	}
	if status != MembershipStatusActive {
		return "", ErrForbidden
	}
	return role, nil
}

func canModerate(role GroupRole) bool {
	return role == GroupRoleOwner || role == GroupRoleAdmin || role == GroupRoleModerator
}

type postScanner interface {
	Scan(dest ...any) error
}

func scanPost(row postScanner) (*GroupPost, error) {
	var post GroupPost
	if err := row.Scan(
		&post.ID,
		&post.GroupID,
		&post.UserID,
		&post.Username,
		&post.AvatarURL,
		&post.PostType,
		&post.Body,
		&post.Anonymous,
		&post.PinnedAt,
		&post.PinnedBy,
		&post.CommentCount,
		&post.ReactionCount,
		&post.ImageCount,
		&post.ViewerHasReacted,
		&post.CreatedAt,
		&post.UpdatedAt,
	); err != nil {
		if err == pgx.ErrNoRows {
			return nil, ErrNotFound
		}
		return nil, err
	}
	return &post, nil
}

func (s *pgStore) getPost(ctx context.Context, viewerID, groupID, postID uuid.UUID) (*GroupPost, error) {
	row := s.pool.QueryRow(ctx, `
		SELECT
			gp.id, gp.group_id, gp.user_id, u.username, u.avatar_url, gp.post_type, gp.body,
			gp.anonymous, gp.pinned_at, gp.pinned_by, gp.comment_count, gp.reaction_count,
			gp.image_count,
			EXISTS (
				SELECT 1 FROM group_reactions gr
				WHERE gr.post_id = gp.id AND gr.user_id = $1 AND gr.type = 'like'
			) AS viewer_has_reacted,
			gp.created_at, gp.updated_at
		FROM group_posts gp
		JOIN users u ON u.id = gp.user_id
		WHERE gp.id = $2
			AND gp.group_id = $3
			AND gp.deleted_at IS NULL`,
		viewerID,
		postID,
		groupID,
	)
	post, err := scanPost(row)
	if err != nil {
		return nil, err
	}
	posts := []GroupPost{*post}
	if err := s.attachImages(ctx, posts, []uuid.UUID{post.ID}); err != nil {
		return nil, err
	}
	return &posts[0], nil
}

func (s *pgStore) attachImages(ctx context.Context, posts []GroupPost, postIDs []uuid.UUID) error {
	if len(postIDs) == 0 {
		return nil
	}
	byID := make(map[uuid.UUID]int, len(posts))
	for index := range posts {
		byID[posts[index].ID] = index
		posts[index].Images = []GroupPostImage{}
	}
	rows, err := s.pool.Query(ctx, `
		SELECT id, post_id, image_url, thumb_url, width, height, position, created_at
		FROM group_post_images
		WHERE post_id = ANY($1)
		ORDER BY post_id, position ASC`,
		postIDs,
	)
	if err != nil {
		return err
	}
	defer rows.Close()

	for rows.Next() {
		var image GroupPostImage
		var postID uuid.UUID
		if err := rows.Scan(&image.ID, &postID, &image.ImageURL, &image.ThumbURL, &image.Width, &image.Height, &image.Position, &image.CreatedAt); err != nil {
			return err
		}
		if index, ok := byID[postID]; ok {
			posts[index].Images = append(posts[index].Images, image)
		}
	}
	return rows.Err()
}

type commentScanner interface {
	Scan(dest ...any) error
}

func scanComment(row commentScanner) (*GroupComment, error) {
	var comment GroupComment
	if err := row.Scan(
		&comment.ID,
		&comment.GroupID,
		&comment.PostID,
		&comment.UserID,
		&comment.Username,
		&comment.AvatarURL,
		&comment.Body,
		&comment.CreatedAt,
		&comment.UpdatedAt,
	); err != nil {
		if err == pgx.ErrNoRows {
			return nil, ErrNotFound
		}
		return nil, err
	}
	return &comment, nil
}

func (s *pgStore) getComment(ctx context.Context, groupID, commentID uuid.UUID) (*GroupComment, error) {
	row := s.pool.QueryRow(ctx, `
		SELECT gc.id, gc.group_id, gc.post_id, gc.user_id, u.username, u.avatar_url, gc.body, gc.created_at, gc.updated_at
		FROM group_comments gc
		JOIN users u ON u.id = gc.user_id
		WHERE gc.group_id = $1
			AND gc.id = $2
			AND gc.deleted_at IS NULL`,
		groupID,
		commentID,
	)
	return scanComment(row)
}

func scanJoinRequest(row groupScanner) (*GroupJoinRequest, error) {
	var request GroupJoinRequest
	if err := row.Scan(
		&request.ID,
		&request.GroupID,
		&request.UserID,
		&request.Username,
		&request.AvatarURL,
		&request.Message,
		&request.Status,
		&request.ReviewedBy,
		&request.ReviewedAt,
		&request.CreatedAt,
		&request.UpdatedAt,
	); err != nil {
		if err == pgx.ErrNoRows {
			return nil, ErrNotFound
		}
		return nil, err
	}
	return &request, nil
}

func scanAdminThread(row groupScanner) (*GroupAdminThread, error) {
	var thread GroupAdminThread
	if err := row.Scan(
		&thread.ID,
		&thread.GroupID,
		&thread.UserID,
		&thread.Username,
		&thread.AvatarURL,
		&thread.Status,
		&thread.Subject,
		&thread.CreatedAt,
		&thread.UpdatedAt,
	); err != nil {
		if err == pgx.ErrNoRows {
			return nil, ErrNotFound
		}
		return nil, err
	}
	return &thread, nil
}

func (s *pgStore) getAdminThread(ctx context.Context, groupID, threadID uuid.UUID, includeMessages bool) (*GroupAdminThread, error) {
	thread, err := scanAdminThread(s.pool.QueryRow(ctx, `
		SELECT gat.id, gat.group_id, gat.user_id, u.username, u.avatar_url, gat.status, gat.subject, gat.created_at, gat.updated_at
		FROM group_admin_threads gat
		JOIN users u ON u.id = gat.user_id
		WHERE gat.group_id = $1 AND gat.id = $2`,
		groupID,
		threadID,
	))
	if err != nil {
		return nil, err
	}
	if !includeMessages {
		return thread, nil
	}
	rows, err := s.pool.Query(ctx, `
		SELECT gam.id, gam.thread_id, gam.sender_id, u.username, u.avatar_url, gam.body, gam.created_at
		FROM group_admin_messages gam
		JOIN users u ON u.id = gam.sender_id
		WHERE gam.thread_id = $1
		ORDER BY gam.created_at ASC`,
		threadID,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	thread.Messages = []GroupAdminMessage{}
	for rows.Next() {
		message, err := scanAdminMessage(rows)
		if err != nil {
			return nil, err
		}
		thread.Messages = append(thread.Messages, *message)
	}
	return thread, rows.Err()
}

func scanAdminMessage(row groupScanner) (*GroupAdminMessage, error) {
	var message GroupAdminMessage
	if err := row.Scan(
		&message.ID,
		&message.ThreadID,
		&message.SenderID,
		&message.Username,
		&message.AvatarURL,
		&message.Body,
		&message.CreatedAt,
	); err != nil {
		if err == pgx.ErrNoRows {
			return nil, ErrNotFound
		}
		return nil, err
	}
	return &message, nil
}

func (s *pgStore) getAdminMessage(ctx context.Context, messageID uuid.UUID) (*GroupAdminMessage, error) {
	return scanAdminMessage(s.pool.QueryRow(ctx, `
		SELECT gam.id, gam.thread_id, gam.sender_id, u.username, u.avatar_url, gam.body, gam.created_at
		FROM group_admin_messages gam
		JOIN users u ON u.id = gam.sender_id
		WHERE gam.id = $1`,
		messageID,
	))
}

func generateInviteToken() (string, error) {
	bytes := make([]byte, 32)
	if _, err := rand.Read(bytes); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(bytes), nil
}

func hashInviteToken(token string) string {
	sum := sha256.Sum256([]byte(strings.TrimSpace(token)))
	return hex.EncodeToString(sum[:])
}

func applyViewerPermissions(group *Group) {
	role := GroupRole("")
	if group.ViewerRole != nil && group.ViewerStatus != nil && *group.ViewerStatus == MembershipStatusActive {
		role = *group.ViewerRole
	}

	isMember := role != ""
	isModerator := role == GroupRoleOwner || role == GroupRoleAdmin || role == GroupRoleModerator
	isAdmin := role == GroupRoleOwner || role == GroupRoleAdmin
	group.CanInvite = isMember
	group.CanManageMembers = isAdmin
	group.CanManageSettings = role == GroupRoleOwner || role == GroupRoleAdmin
	group.CanModerateContent = isModerator
	group.CanPost = isMember && (group.PostingPermission == PostingPermissionMembers || isModerator)
}

var nonSlugChars = regexp.MustCompile(`[^a-z0-9]+`)

func buildGroupSlug(name string) string {
	base := strings.ToLower(strings.TrimSpace(name))
	base = nonSlugChars.ReplaceAllString(base, "-")
	base = strings.Trim(base, "-")
	if base == "" {
		base = "group"
	}
	id := strings.ReplaceAll(uuid.NewString(), "-", "")
	return base + "-" + id[:8]
}

func normalizeMemberScope(scope string) string {
	if strings.TrimSpace(scope) == "joined" {
		return "joined"
	}
	return "discover"
}
