package groups

import (
	"context"
	"net/url"
	"strconv"
	"time"

	"github.com/google/uuid"
	appcache "github.com/project_radeon/api/pkg/cache"
)

const (
	groupsListTTL       = 45 * time.Second
	groupDetailTTL      = 90 * time.Second
	groupCollectionTTL  = 45 * time.Second
	groupAdminListTTL   = 30 * time.Second
	groupJoinRequestTTL = 30 * time.Second
)

type cachedStore struct {
	inner Store
	cache appcache.Store
}

func NewCachedStore(inner Store, store appcache.Store) Store {
	if store == nil {
		store = appcache.NewDisabled()
	}
	return &cachedStore{inner: inner, cache: store}
}

func (s *cachedStore) CreateGroup(ctx context.Context, ownerID uuid.UUID, input CreateGroupInput) (*Group, error) {
	group, err := s.inner.CreateGroup(ctx, ownerID, input)
	if err != nil {
		return nil, err
	}
	s.bumpGroupVersions(ctx, group.ID, ownerID)
	return group, nil
}

func (s *cachedStore) ListGroups(ctx context.Context, viewerID uuid.UUID, params ListGroupsParams) ([]Group, error) {
	groupsVersion, err := s.cache.GetVersion(ctx, s.groupsVersionKey())
	if err != nil {
		return s.inner.ListGroups(ctx, viewerID, params)
	}
	userVersion, err := s.cache.GetVersion(ctx, s.userGroupsVersionKey(viewerID))
	if err != nil {
		return s.inner.ListGroups(ctx, viewerID, params)
	}

	key := s.cache.Key(
		"groups", "list",
		"v", strconv.FormatInt(groupsVersion, 10),
		"user_v", strconv.FormatInt(userVersion, 10),
		"viewer", viewerID.String(),
		"q", encodeGroupCachePart(params.Query),
		"city", encodeGroupCachePart(params.City),
		"country", encodeGroupCachePart(params.Country),
		"tag", encodeGroupCachePart(params.Tag),
		"pathway", encodeGroupCachePart(params.RecoveryPathway),
		"scope", encodeGroupCachePart(params.MemberScope),
		"before", encodeGroupOptionalTime(params.Before),
		"limit", strconv.Itoa(params.Limit),
	)
	var groups []Group
	if err := s.cache.ReadThrough(ctx, key, groupsListTTL, &groups, func(ctx context.Context, dest any) error {
		loaded, err := s.inner.ListGroups(ctx, viewerID, params)
		if err != nil {
			return err
		}
		*dest.(*[]Group) = loaded
		return nil
	}); err != nil {
		return nil, err
	}
	return groups, nil
}

func (s *cachedStore) GetGroup(ctx context.Context, viewerID, groupID uuid.UUID) (*Group, error) {
	groupVersion, userVersion, err := s.groupAndUserVersions(ctx, groupID, viewerID)
	if err != nil {
		return s.inner.GetGroup(ctx, viewerID, groupID)
	}
	key := s.cache.Key(
		"groups", "detail",
		"id", groupID.String(),
		"viewer", viewerID.String(),
		"v", strconv.FormatInt(groupVersion, 10),
		"user_v", strconv.FormatInt(userVersion, 10),
	)
	var group Group
	if err := s.cache.ReadThrough(ctx, key, groupDetailTTL, &group, func(ctx context.Context, dest any) error {
		loaded, err := s.inner.GetGroup(ctx, viewerID, groupID)
		if err != nil {
			return err
		}
		*dest.(*Group) = *loaded
		return nil
	}); err != nil {
		return nil, err
	}
	return &group, nil
}

func (s *cachedStore) JoinGroup(ctx context.Context, viewerID, groupID uuid.UUID, message string) (*JoinGroupResult, error) {
	result, err := s.inner.JoinGroup(ctx, viewerID, groupID, message)
	if err != nil {
		return nil, err
	}
	s.bumpGroupVersions(ctx, groupID, viewerID)
	return result, nil
}

func (s *cachedStore) LeaveGroup(ctx context.Context, viewerID, groupID uuid.UUID) error {
	if err := s.inner.LeaveGroup(ctx, viewerID, groupID); err != nil {
		return err
	}
	s.bumpGroupVersions(ctx, groupID, viewerID)
	return nil
}

func (s *cachedStore) ListMembers(ctx context.Context, viewerID, groupID uuid.UUID, before *time.Time, limit int) ([]GroupMember, error) {
	version, err := s.cache.GetVersion(ctx, s.groupVersionKey(groupID))
	if err != nil {
		return s.inner.ListMembers(ctx, viewerID, groupID, before, limit)
	}
	key := s.cache.Key(
		"groups", "members",
		"id", groupID.String(),
		"viewer", viewerID.String(),
		"v", strconv.FormatInt(version, 10),
		"before", encodeGroupOptionalTime(before),
		"limit", strconv.Itoa(limit),
	)
	var members []GroupMember
	if err := s.cache.ReadThrough(ctx, key, groupCollectionTTL, &members, func(ctx context.Context, dest any) error {
		loaded, err := s.inner.ListMembers(ctx, viewerID, groupID, before, limit)
		if err != nil {
			return err
		}
		*dest.(*[]GroupMember) = loaded
		return nil
	}); err != nil {
		return nil, err
	}
	return members, nil
}

func (s *cachedStore) CreatePost(ctx context.Context, viewerID, groupID uuid.UUID, input CreateGroupPostInput) (*GroupPost, error) {
	post, err := s.inner.CreatePost(ctx, viewerID, groupID, input)
	if err != nil {
		return nil, err
	}
	s.bumpGroupVersions(ctx, groupID, viewerID)
	return post, nil
}

func (s *cachedStore) ListPosts(ctx context.Context, viewerID, groupID uuid.UUID, before *time.Time, limit int) ([]GroupPost, error) {
	version, err := s.cache.GetVersion(ctx, s.groupVersionKey(groupID))
	if err != nil {
		return s.inner.ListPosts(ctx, viewerID, groupID, before, limit)
	}
	key := s.cache.Key(
		"groups", "posts",
		"id", groupID.String(),
		"viewer", viewerID.String(),
		"v", strconv.FormatInt(version, 10),
		"before", encodeGroupOptionalTime(before),
		"limit", strconv.Itoa(limit),
	)
	var posts []GroupPost
	if err := s.cache.ReadThrough(ctx, key, groupCollectionTTL, &posts, func(ctx context.Context, dest any) error {
		loaded, err := s.inner.ListPosts(ctx, viewerID, groupID, before, limit)
		if err != nil {
			return err
		}
		*dest.(*[]GroupPost) = loaded
		return nil
	}); err != nil {
		return nil, err
	}
	return posts, nil
}

func (s *cachedStore) ListMedia(ctx context.Context, viewerID, groupID uuid.UUID, before *time.Time, limit int) ([]GroupMediaItem, error) {
	version, err := s.cache.GetVersion(ctx, s.groupVersionKey(groupID))
	if err != nil {
		return s.inner.ListMedia(ctx, viewerID, groupID, before, limit)
	}
	key := s.cache.Key(
		"groups", "media",
		"id", groupID.String(),
		"viewer", viewerID.String(),
		"v", strconv.FormatInt(version, 10),
		"before", encodeGroupOptionalTime(before),
		"limit", strconv.Itoa(limit),
	)
	var media []GroupMediaItem
	if err := s.cache.ReadThrough(ctx, key, groupCollectionTTL, &media, func(ctx context.Context, dest any) error {
		loaded, err := s.inner.ListMedia(ctx, viewerID, groupID, before, limit)
		if err != nil {
			return err
		}
		*dest.(*[]GroupMediaItem) = loaded
		return nil
	}); err != nil {
		return nil, err
	}
	return media, nil
}

func (s *cachedStore) CreateComment(ctx context.Context, viewerID, groupID, postID uuid.UUID, body string) (*GroupComment, error) {
	comment, err := s.inner.CreateComment(ctx, viewerID, groupID, postID, body)
	if err != nil {
		return nil, err
	}
	s.bumpGroupVersions(ctx, groupID, viewerID)
	return comment, nil
}

func (s *cachedStore) ListComments(ctx context.Context, viewerID, groupID, postID uuid.UUID, after *time.Time, limit int) ([]GroupComment, error) {
	version, err := s.cache.GetVersion(ctx, s.groupVersionKey(groupID))
	if err != nil {
		return s.inner.ListComments(ctx, viewerID, groupID, postID, after, limit)
	}
	key := s.cache.Key(
		"groups", "comments",
		"id", groupID.String(),
		"post", postID.String(),
		"viewer", viewerID.String(),
		"v", strconv.FormatInt(version, 10),
		"after", encodeGroupOptionalTime(after),
		"limit", strconv.Itoa(limit),
	)
	var comments []GroupComment
	if err := s.cache.ReadThrough(ctx, key, groupCollectionTTL, &comments, func(ctx context.Context, dest any) error {
		loaded, err := s.inner.ListComments(ctx, viewerID, groupID, postID, after, limit)
		if err != nil {
			return err
		}
		*dest.(*[]GroupComment) = loaded
		return nil
	}); err != nil {
		return nil, err
	}
	return comments, nil
}

func (s *cachedStore) ToggleReaction(ctx context.Context, viewerID, groupID, postID uuid.UUID) (*GroupPost, error) {
	post, err := s.inner.ToggleReaction(ctx, viewerID, groupID, postID)
	if err != nil {
		return nil, err
	}
	s.bumpGroupVersions(ctx, groupID, viewerID)
	return post, nil
}

func (s *cachedStore) PinPost(ctx context.Context, viewerID, groupID, postID uuid.UUID, pinned bool) (*GroupPost, error) {
	post, err := s.inner.PinPost(ctx, viewerID, groupID, postID, pinned)
	if err != nil {
		return nil, err
	}
	s.bumpGroupVersions(ctx, groupID, viewerID)
	return post, nil
}

func (s *cachedStore) DeletePost(ctx context.Context, viewerID, groupID, postID uuid.UUID) error {
	if err := s.inner.DeletePost(ctx, viewerID, groupID, postID); err != nil {
		return err
	}
	s.bumpGroupVersions(ctx, groupID, viewerID)
	return nil
}

func (s *cachedStore) CreateInvite(ctx context.Context, viewerID, groupID uuid.UUID, input CreateGroupInviteInput) (*GroupInvite, error) {
	invite, err := s.inner.CreateInvite(ctx, viewerID, groupID, input)
	if err != nil {
		return nil, err
	}
	s.bumpGroupVersions(ctx, groupID, viewerID)
	return invite, nil
}

func (s *cachedStore) AcceptInvite(ctx context.Context, viewerID uuid.UUID, token string) (*JoinGroupResult, error) {
	result, err := s.inner.AcceptInvite(ctx, viewerID, token)
	if err != nil {
		return nil, err
	}
	if result.Group != nil {
		s.bumpGroupVersions(ctx, result.Group.ID, viewerID)
	} else {
		_ = s.cache.BumpVersions(ctx, s.groupsVersionKey(), s.userGroupsVersionKey(viewerID))
	}
	return result, nil
}

func (s *cachedStore) ListJoinRequests(ctx context.Context, viewerID, groupID uuid.UUID) ([]GroupJoinRequest, error) {
	version, err := s.cache.GetVersion(ctx, s.groupVersionKey(groupID))
	if err != nil {
		return s.inner.ListJoinRequests(ctx, viewerID, groupID)
	}
	key := s.cache.Key(
		"groups", "join_requests",
		"id", groupID.String(),
		"viewer", viewerID.String(),
		"v", strconv.FormatInt(version, 10),
	)
	var requests []GroupJoinRequest
	if err := s.cache.ReadThrough(ctx, key, groupJoinRequestTTL, &requests, func(ctx context.Context, dest any) error {
		loaded, err := s.inner.ListJoinRequests(ctx, viewerID, groupID)
		if err != nil {
			return err
		}
		*dest.(*[]GroupJoinRequest) = loaded
		return nil
	}); err != nil {
		return nil, err
	}
	return requests, nil
}

func (s *cachedStore) ReviewJoinRequest(ctx context.Context, viewerID, groupID, requestID uuid.UUID, approve bool) (*GroupJoinRequest, error) {
	request, err := s.inner.ReviewJoinRequest(ctx, viewerID, groupID, requestID, approve)
	if err != nil {
		return nil, err
	}
	s.bumpGroupVersions(ctx, groupID, viewerID, request.UserID)
	return request, nil
}

func (s *cachedStore) ContactAdmins(ctx context.Context, viewerID, groupID uuid.UUID, subject, body string) (*GroupAdminThread, error) {
	thread, err := s.inner.ContactAdmins(ctx, viewerID, groupID, subject, body)
	if err != nil {
		return nil, err
	}
	s.bumpGroupVersions(ctx, groupID, viewerID)
	return thread, nil
}

func (s *cachedStore) ListAdminThreads(ctx context.Context, viewerID, groupID uuid.UUID, before *time.Time, limit int) ([]GroupAdminThread, error) {
	version, err := s.cache.GetVersion(ctx, s.groupVersionKey(groupID))
	if err != nil {
		return s.inner.ListAdminThreads(ctx, viewerID, groupID, before, limit)
	}
	key := s.cache.Key(
		"groups", "admin_threads",
		"id", groupID.String(),
		"viewer", viewerID.String(),
		"v", strconv.FormatInt(version, 10),
		"before", encodeGroupOptionalTime(before),
		"limit", strconv.Itoa(limit),
	)
	var threads []GroupAdminThread
	if err := s.cache.ReadThrough(ctx, key, groupAdminListTTL, &threads, func(ctx context.Context, dest any) error {
		loaded, err := s.inner.ListAdminThreads(ctx, viewerID, groupID, before, limit)
		if err != nil {
			return err
		}
		*dest.(*[]GroupAdminThread) = loaded
		return nil
	}); err != nil {
		return nil, err
	}
	return threads, nil
}

func (s *cachedStore) ReplyAdminThread(ctx context.Context, viewerID, groupID, threadID uuid.UUID, body string) (*GroupAdminMessage, error) {
	message, err := s.inner.ReplyAdminThread(ctx, viewerID, groupID, threadID, body)
	if err != nil {
		return nil, err
	}
	s.bumpGroupVersions(ctx, groupID, viewerID)
	return message, nil
}

func (s *cachedStore) ResolveAdminThread(ctx context.Context, viewerID, groupID, threadID uuid.UUID) (*GroupAdminThread, error) {
	thread, err := s.inner.ResolveAdminThread(ctx, viewerID, groupID, threadID)
	if err != nil {
		return nil, err
	}
	s.bumpGroupVersions(ctx, groupID, viewerID)
	return thread, nil
}

func (s *cachedStore) ReportTarget(ctx context.Context, viewerID, groupID uuid.UUID, targetType string, targetID *uuid.UUID, reason string, details *string) (*GroupReport, error) {
	report, err := s.inner.ReportTarget(ctx, viewerID, groupID, targetType, targetID, reason, details)
	if err != nil {
		return nil, err
	}
	s.bumpGroupVersions(ctx, groupID, viewerID)
	return report, nil
}

func (s *cachedStore) groupAndUserVersions(ctx context.Context, groupID, viewerID uuid.UUID) (int64, int64, error) {
	groupVersion, err := s.cache.GetVersion(ctx, s.groupVersionKey(groupID))
	if err != nil {
		return 0, 0, err
	}
	userVersion, err := s.cache.GetVersion(ctx, s.userGroupsVersionKey(viewerID))
	if err != nil {
		return 0, 0, err
	}
	return groupVersion, userVersion, nil
}

func (s *cachedStore) bumpGroupVersions(ctx context.Context, groupID uuid.UUID, userIDs ...uuid.UUID) {
	keys := []string{s.groupsVersionKey(), s.groupVersionKey(groupID)}
	for _, userID := range userIDs {
		if userID == uuid.Nil {
			continue
		}
		keys = append(keys, s.userGroupsVersionKey(userID))
	}
	_ = s.cache.BumpVersions(ctx, keys...)
}

func (s *cachedStore) groupsVersionKey() string {
	return s.cache.Key("ver", "groups")
}

func (s *cachedStore) groupVersionKey(groupID uuid.UUID) string {
	return s.cache.Key("ver", "group", groupID.String())
}

func (s *cachedStore) userGroupsVersionKey(userID uuid.UUID) string {
	return s.cache.Key("ver", "groups", "user", userID.String())
}

func encodeGroupCachePart(value string) string {
	if value == "" {
		return "none"
	}
	return url.QueryEscape(value)
}

func encodeGroupOptionalTime(value *time.Time) string {
	if value == nil {
		return "none"
	}
	return value.UTC().Format(time.RFC3339Nano)
}
