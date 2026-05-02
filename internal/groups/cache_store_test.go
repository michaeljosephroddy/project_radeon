package groups

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/project_radeon/api/internal/cachetest"
)

type countingGroupStore struct {
	listGroupsCalls int
	getGroupCalls   int
	listPostsCalls  int
	joinGroupCalls  int
	createPostCalls int

	groupID uuid.UUID
	postID  uuid.UUID
}

func newCountingGroupStore() *countingGroupStore {
	return &countingGroupStore{
		groupID: uuid.New(),
		postID:  uuid.New(),
	}
}

func (s *countingGroupStore) group(viewerID uuid.UUID) Group {
	role := GroupRoleMember
	status := MembershipStatusActive
	return Group{
		ID:                s.groupID,
		OwnerID:           uuid.New(),
		Name:              "Cached Recovery Group",
		Slug:              "cached-recovery-group",
		Visibility:        GroupVisibilityPublic,
		PostingPermission: PostingPermissionMembers,
		Tags:              []string{"alcohol-free"},
		RecoveryPathways:  []string{"smart"},
		MemberCount:       12,
		PostCount:         3,
		ViewerRole:        &role,
		ViewerStatus:      &status,
		CreatedAt:         time.Now().UTC().Add(-24 * time.Hour),
		UpdatedAt:         time.Now().UTC(),
		HasPendingRequest: viewerID == uuid.Nil,
		CanPost:           true,
		CanInvite:         false,
	}
}

func (s *countingGroupStore) post(viewerID uuid.UUID) GroupPost {
	return GroupPost{
		ID:               s.postID,
		GroupID:          s.groupID,
		UserID:           viewerID,
		Username:         "seeduser",
		PostType:         GroupPostTypeStandard,
		Body:             "Checking in.",
		ReactionCount:    2,
		ViewerHasReacted: viewerID == uuid.Nil,
		Images:           []GroupPostImage{},
		CreatedAt:        time.Now().UTC().Add(-time.Hour),
		UpdatedAt:        time.Now().UTC(),
	}
}

func (s *countingGroupStore) CreateGroup(_ context.Context, ownerID uuid.UUID, _ CreateGroupInput) (*Group, error) {
	group := s.group(ownerID)
	return &group, nil
}

func (s *countingGroupStore) ListGroups(_ context.Context, viewerID uuid.UUID, _ ListGroupsParams) ([]Group, error) {
	s.listGroupsCalls++
	return []Group{s.group(viewerID)}, nil
}

func (s *countingGroupStore) GetGroup(_ context.Context, viewerID, _ uuid.UUID) (*Group, error) {
	s.getGroupCalls++
	group := s.group(viewerID)
	return &group, nil
}

func (s *countingGroupStore) JoinGroup(_ context.Context, viewerID, _ uuid.UUID, _ string) (*JoinGroupResult, error) {
	s.joinGroupCalls++
	group := s.group(viewerID)
	return &JoinGroupResult{State: "member", Group: &group}, nil
}

func (s *countingGroupStore) LeaveGroup(_ context.Context, _ uuid.UUID, _ uuid.UUID) error {
	return nil
}

func (s *countingGroupStore) ListMembers(_ context.Context, _ uuid.UUID, _ uuid.UUID, _ *time.Time, _ int) ([]GroupMember, error) {
	return []GroupMember{}, nil
}

func (s *countingGroupStore) CreatePost(_ context.Context, viewerID, _ uuid.UUID, _ CreateGroupPostInput) (*GroupPost, error) {
	s.createPostCalls++
	post := s.post(viewerID)
	return &post, nil
}

func (s *countingGroupStore) ListPosts(_ context.Context, viewerID, _ uuid.UUID, _ *time.Time, _ int) ([]GroupPost, error) {
	s.listPostsCalls++
	return []GroupPost{s.post(viewerID)}, nil
}

func (s *countingGroupStore) ListMedia(_ context.Context, _ uuid.UUID, _ uuid.UUID, _ *time.Time, _ int) ([]GroupMediaItem, error) {
	return []GroupMediaItem{}, nil
}

func (s *countingGroupStore) CreateComment(_ context.Context, viewerID, groupID, postID uuid.UUID, _ string) (*GroupComment, error) {
	return &GroupComment{ID: uuid.New(), GroupID: groupID, PostID: postID, UserID: viewerID, Body: "reply"}, nil
}

func (s *countingGroupStore) ListComments(_ context.Context, _ uuid.UUID, _ uuid.UUID, _ uuid.UUID, _ *time.Time, _ int) ([]GroupComment, error) {
	return []GroupComment{}, nil
}

func (s *countingGroupStore) ToggleReaction(_ context.Context, viewerID, _ uuid.UUID, _ uuid.UUID) (*GroupPost, error) {
	post := s.post(viewerID)
	post.ViewerHasReacted = !post.ViewerHasReacted
	return &post, nil
}

func (s *countingGroupStore) PinPost(_ context.Context, viewerID, _ uuid.UUID, _ uuid.UUID, _ bool) (*GroupPost, error) {
	post := s.post(viewerID)
	return &post, nil
}

func (s *countingGroupStore) DeletePost(_ context.Context, _ uuid.UUID, _ uuid.UUID, _ uuid.UUID) error {
	return nil
}

func (s *countingGroupStore) CreateInvite(_ context.Context, _ uuid.UUID, groupID uuid.UUID, _ CreateGroupInviteInput) (*GroupInvite, error) {
	return &GroupInvite{ID: uuid.New(), GroupID: groupID}, nil
}

func (s *countingGroupStore) AcceptInvite(_ context.Context, viewerID uuid.UUID, _ string) (*JoinGroupResult, error) {
	group := s.group(viewerID)
	return &JoinGroupResult{State: "member", Group: &group}, nil
}

func (s *countingGroupStore) ListJoinRequests(_ context.Context, _ uuid.UUID, _ uuid.UUID) ([]GroupJoinRequest, error) {
	return []GroupJoinRequest{}, nil
}

func (s *countingGroupStore) ReviewJoinRequest(_ context.Context, _ uuid.UUID, groupID, requestID uuid.UUID, _ bool) (*GroupJoinRequest, error) {
	return &GroupJoinRequest{ID: requestID, GroupID: groupID, UserID: uuid.New()}, nil
}

func (s *countingGroupStore) ContactAdmins(_ context.Context, viewerID, groupID uuid.UUID, subject, _ string) (*GroupAdminThread, error) {
	return &GroupAdminThread{ID: uuid.New(), GroupID: groupID, UserID: viewerID, Subject: &subject}, nil
}

func (s *countingGroupStore) ListAdminThreads(_ context.Context, _ uuid.UUID, _ uuid.UUID, _ *time.Time, _ int) ([]GroupAdminThread, error) {
	return []GroupAdminThread{}, nil
}

func (s *countingGroupStore) ReplyAdminThread(_ context.Context, viewerID, _ uuid.UUID, threadID uuid.UUID, body string) (*GroupAdminMessage, error) {
	return &GroupAdminMessage{ID: uuid.New(), ThreadID: threadID, SenderID: viewerID, Body: body}, nil
}

func (s *countingGroupStore) ResolveAdminThread(_ context.Context, viewerID, groupID, threadID uuid.UUID) (*GroupAdminThread, error) {
	return &GroupAdminThread{ID: threadID, GroupID: groupID, UserID: viewerID}, nil
}

func (s *countingGroupStore) ReportTarget(_ context.Context, viewerID, groupID uuid.UUID, targetType string, targetID *uuid.UUID, reason string, details *string) (*GroupReport, error) {
	return &GroupReport{ID: uuid.New(), GroupID: groupID, ReporterID: viewerID, TargetType: targetType, TargetID: targetID, Reason: reason, Details: details}, nil
}

func TestCachedStoreListGroupsCachesAndKeepsViewersSeparate(t *testing.T) {
	ctx := context.Background()
	inner := newCountingGroupStore()
	cached := NewCachedStore(inner, cachetest.NewStore())
	viewerA := uuid.New()
	viewerB := uuid.New()
	params := ListGroupsParams{MemberScope: "discover", Limit: 20}

	if _, err := cached.ListGroups(ctx, viewerA, params); err != nil {
		t.Fatalf("first viewer A list failed: %v", err)
	}
	if _, err := cached.ListGroups(ctx, viewerA, params); err != nil {
		t.Fatalf("second viewer A list failed: %v", err)
	}
	if inner.listGroupsCalls != 1 {
		t.Fatalf("expected viewer A second list to hit cache, got %d calls", inner.listGroupsCalls)
	}

	if _, err := cached.ListGroups(ctx, viewerB, params); err != nil {
		t.Fatalf("viewer B list failed: %v", err)
	}
	if inner.listGroupsCalls != 2 {
		t.Fatalf("expected viewer-specific groups cache key, got %d calls", inner.listGroupsCalls)
	}
}

func TestCachedStoreInvalidatesGroupListAfterJoin(t *testing.T) {
	ctx := context.Background()
	inner := newCountingGroupStore()
	cached := NewCachedStore(inner, cachetest.NewStore())
	viewerID := uuid.New()
	params := ListGroupsParams{MemberScope: "discover", Limit: 20}

	if _, err := cached.ListGroups(ctx, viewerID, params); err != nil {
		t.Fatalf("list failed: %v", err)
	}
	if _, err := cached.JoinGroup(ctx, viewerID, inner.groupID, "please"); err != nil {
		t.Fatalf("join failed: %v", err)
	}
	if _, err := cached.ListGroups(ctx, viewerID, params); err != nil {
		t.Fatalf("list after join failed: %v", err)
	}
	if inner.listGroupsCalls != 2 {
		t.Fatalf("expected list cache invalidation after join, got %d list calls", inner.listGroupsCalls)
	}
}

func TestCachedStoreListPostsInvalidatesAfterCreatePost(t *testing.T) {
	ctx := context.Background()
	inner := newCountingGroupStore()
	cached := NewCachedStore(inner, cachetest.NewStore())
	viewerID := uuid.New()

	if _, err := cached.ListPosts(ctx, viewerID, inner.groupID, nil, 20); err != nil {
		t.Fatalf("list posts failed: %v", err)
	}
	if _, err := cached.ListPosts(ctx, viewerID, inner.groupID, nil, 20); err != nil {
		t.Fatalf("second list posts failed: %v", err)
	}
	if inner.listPostsCalls != 1 {
		t.Fatalf("expected second posts list to hit cache, got %d calls", inner.listPostsCalls)
	}

	if _, err := cached.CreatePost(ctx, viewerID, inner.groupID, CreateGroupPostInput{PostType: GroupPostTypeStandard, Body: "new"}); err != nil {
		t.Fatalf("create post failed: %v", err)
	}
	if _, err := cached.ListPosts(ctx, viewerID, inner.groupID, nil, 20); err != nil {
		t.Fatalf("list posts after create failed: %v", err)
	}
	if inner.listPostsCalls != 2 {
		t.Fatalf("expected posts cache invalidation after create, got %d calls", inner.listPostsCalls)
	}
}
