package groups

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/project_radeon/api/pkg/middleware"
)

type mockStore struct {
	create       func(ctx context.Context, ownerID uuid.UUID, input CreateGroupInput) (*Group, error)
	list         func(ctx context.Context, viewerID uuid.UUID, params ListGroupsParams) ([]Group, error)
	get          func(ctx context.Context, viewerID, groupID uuid.UUID) (*Group, error)
	join         func(ctx context.Context, viewerID, groupID uuid.UUID, message string) (*JoinGroupResult, error)
	leave        func(ctx context.Context, viewerID, groupID uuid.UUID) error
	listMembers  func(ctx context.Context, viewerID, groupID uuid.UUID, before *time.Time, limit int) ([]GroupMember, error)
	createPost   func(ctx context.Context, viewerID, groupID uuid.UUID, input CreateGroupPostInput) (*GroupPost, error)
	listPosts    func(ctx context.Context, viewerID, groupID uuid.UUID, before *time.Time, limit int) ([]GroupPost, error)
	createInvite func(ctx context.Context, viewerID, groupID uuid.UUID, input CreateGroupInviteInput) (*GroupInvite, error)
}

func (m *mockStore) CreateGroup(ctx context.Context, ownerID uuid.UUID, input CreateGroupInput) (*Group, error) {
	if m.create != nil {
		return m.create(ctx, ownerID, input)
	}
	return testGroup(ownerID, input.Name), nil
}

func (m *mockStore) ListGroups(ctx context.Context, viewerID uuid.UUID, params ListGroupsParams) ([]Group, error) {
	if m.list != nil {
		return m.list(ctx, viewerID, params)
	}
	return []Group{*testGroup(viewerID, "Dublin Recovery")}, nil
}

func (m *mockStore) GetGroup(ctx context.Context, viewerID, groupID uuid.UUID) (*Group, error) {
	if m.get != nil {
		return m.get(ctx, viewerID, groupID)
	}
	group := testGroup(viewerID, "Dublin Recovery")
	group.ID = groupID
	return group, nil
}

func (m *mockStore) JoinGroup(ctx context.Context, viewerID, groupID uuid.UUID, message string) (*JoinGroupResult, error) {
	if m.join != nil {
		return m.join(ctx, viewerID, groupID, message)
	}
	return &JoinGroupResult{State: "member", Group: testGroup(viewerID, "Dublin Recovery")}, nil
}

func (m *mockStore) LeaveGroup(ctx context.Context, viewerID, groupID uuid.UUID) error {
	if m.leave != nil {
		return m.leave(ctx, viewerID, groupID)
	}
	return nil
}

func (m *mockStore) ListMembers(ctx context.Context, viewerID, groupID uuid.UUID, before *time.Time, limit int) ([]GroupMember, error) {
	if m.listMembers != nil {
		return m.listMembers(ctx, viewerID, groupID, before, limit)
	}
	now := time.Now().UTC()
	return []GroupMember{{
		UserID:    viewerID,
		Username:  "alex",
		Role:      GroupRoleOwner,
		Status:    MembershipStatusActive,
		JoinedAt:  &now,
		CreatedAt: now,
		UpdatedAt: now,
	}}, nil
}

func (m *mockStore) CreatePost(ctx context.Context, viewerID, groupID uuid.UUID, input CreateGroupPostInput) (*GroupPost, error) {
	if m.createPost != nil {
		return m.createPost(ctx, viewerID, groupID, input)
	}
	return testPost(viewerID, groupID, input.Body), nil
}

func (m *mockStore) ListPosts(ctx context.Context, viewerID, groupID uuid.UUID, before *time.Time, limit int) ([]GroupPost, error) {
	if m.listPosts != nil {
		return m.listPosts(ctx, viewerID, groupID, before, limit)
	}
	return []GroupPost{*testPost(viewerID, groupID, "steady today")}, nil
}

func (m *mockStore) ListMedia(ctx context.Context, viewerID, groupID uuid.UUID, before *time.Time, limit int) ([]GroupMediaItem, error) {
	now := time.Now().UTC()
	return []GroupMediaItem{{
		ID:        uuid.New(),
		GroupID:   groupID,
		PostID:    uuid.New(),
		ImageURL:  "https://example.com/image.jpg",
		Width:     800,
		Height:    600,
		CreatedAt: now,
	}}, nil
}

func (m *mockStore) CreateComment(ctx context.Context, viewerID, groupID, postID uuid.UUID, body string) (*GroupComment, error) {
	now := time.Now().UTC()
	return &GroupComment{
		ID:        uuid.New(),
		GroupID:   groupID,
		PostID:    postID,
		UserID:    viewerID,
		Username:  "alex",
		Body:      body,
		CreatedAt: now,
		UpdatedAt: now,
	}, nil
}

func (m *mockStore) ListComments(ctx context.Context, viewerID, groupID, postID uuid.UUID, after *time.Time, limit int) ([]GroupComment, error) {
	comment, err := m.CreateComment(ctx, viewerID, groupID, postID, "same here")
	if err != nil {
		return nil, err
	}
	return []GroupComment{*comment}, nil
}

func (m *mockStore) ToggleReaction(ctx context.Context, viewerID, groupID, postID uuid.UUID) (*GroupPost, error) {
	post := testPost(viewerID, groupID, "steady today")
	post.ID = postID
	post.ViewerHasReacted = true
	post.ReactionCount = 1
	return post, nil
}

func (m *mockStore) PinPost(ctx context.Context, viewerID, groupID, postID uuid.UUID, pinned bool) (*GroupPost, error) {
	post := testPost(viewerID, groupID, "steady today")
	post.ID = postID
	now := time.Now().UTC()
	if pinned {
		post.PinnedAt = &now
		post.PinnedBy = &viewerID
	}
	return post, nil
}

func (m *mockStore) DeletePost(ctx context.Context, viewerID, groupID, postID uuid.UUID) error {
	return nil
}

func (m *mockStore) CreateInvite(ctx context.Context, viewerID, groupID uuid.UUID, input CreateGroupInviteInput) (*GroupInvite, error) {
	if m.createInvite != nil {
		return m.createInvite(ctx, viewerID, groupID, input)
	}
	return &GroupInvite{ID: uuid.New(), GroupID: groupID, Token: "token", CreatedAt: time.Now().UTC()}, nil
}

func (m *mockStore) AcceptInvite(ctx context.Context, viewerID uuid.UUID, token string) (*JoinGroupResult, error) {
	return &JoinGroupResult{State: "member"}, nil
}

func (m *mockStore) ListJoinRequests(ctx context.Context, viewerID, groupID uuid.UUID) ([]GroupJoinRequest, error) {
	return []GroupJoinRequest{}, nil
}

func (m *mockStore) ReviewJoinRequest(ctx context.Context, viewerID, groupID, requestID uuid.UUID, approve bool) (*GroupJoinRequest, error) {
	now := time.Now().UTC()
	return &GroupJoinRequest{
		ID:        requestID,
		GroupID:   groupID,
		UserID:    uuid.New(),
		Username:  "sam",
		Status:    "approved",
		CreatedAt: now,
		UpdatedAt: now,
	}, nil
}

func (m *mockStore) ContactAdmins(ctx context.Context, viewerID, groupID uuid.UUID, subject, body string) (*GroupAdminThread, error) {
	now := time.Now().UTC()
	return &GroupAdminThread{
		ID:        uuid.New(),
		GroupID:   groupID,
		UserID:    viewerID,
		Username:  "alex",
		Status:    "open",
		CreatedAt: now,
		UpdatedAt: now,
	}, nil
}

func (m *mockStore) ListAdminThreads(ctx context.Context, viewerID, groupID uuid.UUID, before *time.Time, limit int) ([]GroupAdminThread, error) {
	return []GroupAdminThread{}, nil
}

func (m *mockStore) ReplyAdminThread(ctx context.Context, viewerID, groupID, threadID uuid.UUID, body string) (*GroupAdminMessage, error) {
	return &GroupAdminMessage{ID: uuid.New(), ThreadID: threadID, SenderID: viewerID, Username: "alex", Body: body, CreatedAt: time.Now().UTC()}, nil
}

func (m *mockStore) ResolveAdminThread(ctx context.Context, viewerID, groupID, threadID uuid.UUID) (*GroupAdminThread, error) {
	now := time.Now().UTC()
	return &GroupAdminThread{ID: threadID, GroupID: groupID, UserID: viewerID, Username: "alex", Status: "resolved", CreatedAt: now, UpdatedAt: now}, nil
}

func (m *mockStore) ReportTarget(ctx context.Context, viewerID, groupID uuid.UUID, targetType string, targetID *uuid.UUID, reason string, details *string) (*GroupReport, error) {
	return &GroupReport{ID: uuid.New(), GroupID: groupID, ReporterID: viewerID, TargetType: targetType, TargetID: targetID, Reason: reason, Status: "open", CreatedAt: time.Now().UTC()}, nil
}

func TestCreateGroupDefaultsVisibilityAndPostingPermission(t *testing.T) {
	userID := uuid.New()
	h := NewHandler(&mockStore{
		create: func(_ context.Context, ownerID uuid.UUID, input CreateGroupInput) (*Group, error) {
			if ownerID != userID {
				t.Fatalf("ownerID = %s, want %s", ownerID, userID)
			}
			if input.Name != "Dublin Recovery" {
				t.Fatalf("name = %q", input.Name)
			}
			if input.Visibility != GroupVisibilityPublic {
				t.Fatalf("visibility = %q", input.Visibility)
			}
			if input.PostingPermission != PostingPermissionMembers {
				t.Fatalf("posting permission = %q", input.PostingPermission)
			}
			return testGroup(ownerID, input.Name), nil
		},
	})

	req := withUserID(httptest.NewRequest(http.MethodPost, "/groups", strings.NewReader(`{"name":" Dublin Recovery "}`)), userID)
	rec := httptest.NewRecorder()
	h.CreateGroup(rec, req)

	if rec.Code != http.StatusCreated {
		t.Fatalf("status = %d, body = %s", rec.Code, rec.Body.String())
	}
}

func TestCreateGroupRejectsInvalidVisibility(t *testing.T) {
	h := NewHandler(&mockStore{})
	req := withUserID(httptest.NewRequest(http.MethodPost, "/groups", strings.NewReader(`{"name":"Dublin Recovery","visibility":"secret"}`)), uuid.New())
	rec := httptest.NewRecorder()

	h.CreateGroup(rec, req)

	if rec.Code != http.StatusUnprocessableEntity {
		t.Fatalf("status = %d, body = %s", rec.Code, rec.Body.String())
	}
}

func TestJoinGroupReturnsInviteRequired(t *testing.T) {
	groupID := uuid.New()
	h := NewHandler(&mockStore{
		join: func(_ context.Context, _ uuid.UUID, gotGroupID uuid.UUID, _ string) (*JoinGroupResult, error) {
			if gotGroupID != groupID {
				t.Fatalf("groupID = %s, want %s", gotGroupID, groupID)
			}
			return nil, ErrInviteRequired
		},
	})
	req := withURLParam(withUserID(httptest.NewRequest(http.MethodPost, "/groups/"+groupID.String()+"/join", nil), uuid.New()), "id", groupID.String())
	rec := httptest.NewRecorder()

	h.JoinGroup(rec, req)

	if rec.Code != http.StatusForbidden {
		t.Fatalf("status = %d, body = %s", rec.Code, rec.Body.String())
	}
}

func TestListGroupsParsesSearchAndFilters(t *testing.T) {
	userID := uuid.New()
	h := NewHandler(&mockStore{
		list: func(_ context.Context, viewerID uuid.UUID, params ListGroupsParams) ([]Group, error) {
			if viewerID != userID {
				t.Fatalf("viewerID = %s, want %s", viewerID, userID)
			}
			if params.Query != "local" || params.Tag != "women" || params.RecoveryPathway != "aa" || params.MemberScope != "joined" {
				t.Fatalf("unexpected params: %+v", params)
			}
			return []Group{*testGroup(viewerID, "Local Women")}, nil
		},
	})
	req := withUserID(httptest.NewRequest(http.MethodGet, "/groups?q=local&tag=women&recovery_pathway=aa&member_scope=joined", nil), userID)
	rec := httptest.NewRecorder()

	h.ListGroups(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", rec.Code, rec.Body.String())
	}
	var body struct {
		Data struct {
			Items []Group `json:"items"`
		} `json:"data"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if len(body.Data.Items) != 1 {
		t.Fatalf("items = %d", len(body.Data.Items))
	}
}

func TestCreatePostRejectsInvalidPostType(t *testing.T) {
	groupID := uuid.New()
	h := NewHandler(&mockStore{})
	req := withURLParam(
		withUserID(httptest.NewRequest(http.MethodPost, "/groups/"+groupID.String()+"/posts", strings.NewReader(`{"body":"hello","post_type":"chat"}`)), uuid.New()),
		"id",
		groupID.String(),
	)
	rec := httptest.NewRecorder()

	h.CreatePost(rec, req)

	if rec.Code != http.StatusUnprocessableEntity {
		t.Fatalf("status = %d, body = %s", rec.Code, rec.Body.String())
	}
}

func TestCreatePostAcceptsImages(t *testing.T) {
	userID := uuid.New()
	groupID := uuid.New()
	h := NewHandler(&mockStore{
		createPost: func(_ context.Context, viewerID, gotGroupID uuid.UUID, input CreateGroupPostInput) (*GroupPost, error) {
			if viewerID != userID || gotGroupID != groupID {
				t.Fatalf("viewer/group mismatch")
			}
			if input.PostType != GroupPostTypeNeedSupport {
				t.Fatalf("post type = %q", input.PostType)
			}
			if len(input.Images) != 1 || input.Images[0].Width != 800 {
				t.Fatalf("images = %+v", input.Images)
			}
			return testPost(viewerID, gotGroupID, input.Body), nil
		},
	})
	req := withURLParam(
		withUserID(httptest.NewRequest(http.MethodPost, "/groups/"+groupID.String()+"/posts", strings.NewReader(`{"body":"could use support","post_type":"need_support","images":[{"image_url":"https://example.com/a.jpg","width":800,"height":600}]}`)), userID),
		"id",
		groupID.String(),
	)
	rec := httptest.NewRecorder()

	h.CreatePost(rec, req)

	if rec.Code != http.StatusCreated {
		t.Fatalf("status = %d, body = %s", rec.Code, rec.Body.String())
	}
}

func testGroup(ownerID uuid.UUID, name string) *Group {
	now := time.Now().UTC()
	role := GroupRoleOwner
	status := MembershipStatusActive
	group := &Group{
		ID:                uuid.New(),
		OwnerID:           ownerID,
		Name:              name,
		Slug:              "dublin-recovery",
		Visibility:        GroupVisibilityPublic,
		PostingPermission: PostingPermissionMembers,
		Tags:              []string{},
		RecoveryPathways:  []string{},
		MemberCount:       1,
		ViewerRole:        &role,
		ViewerStatus:      &status,
		CreatedAt:         now,
		UpdatedAt:         now,
	}
	applyViewerPermissions(group)
	return group
}

func testPost(userID, groupID uuid.UUID, body string) *GroupPost {
	now := time.Now().UTC()
	return &GroupPost{
		ID:        uuid.New(),
		GroupID:   groupID,
		UserID:    userID,
		Username:  "alex",
		PostType:  GroupPostTypeStandard,
		Body:      body,
		Images:    []GroupPostImage{},
		CreatedAt: now,
		UpdatedAt: now,
	}
}

func withUserID(req *http.Request, userID uuid.UUID) *http.Request {
	ctx := context.WithValue(req.Context(), middleware.UserIDKey, userID)
	return req.WithContext(ctx)
}

func withURLParam(req *http.Request, key, value string) *http.Request {
	rctx := chi.NewRouteContext()
	rctx.URLParams.Add(key, value)
	ctx := context.WithValue(req.Context(), chi.RouteCtxKey, rctx)
	return req.WithContext(ctx)
}
