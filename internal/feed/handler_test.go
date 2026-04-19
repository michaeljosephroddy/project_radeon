package feed

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/project_radeon/api/pkg/middleware"
)

type mockQuerier struct {
	listFeed       func(ctx context.Context, before *time.Time, limit int) ([]Post, error)
	listUserPosts  func(ctx context.Context, userID uuid.UUID, before *time.Time, limit int) ([]Post, error)
	createPost     func(ctx context.Context, userID uuid.UUID, body string) (uuid.UUID, error)
	deletePost     func(ctx context.Context, postID, userID uuid.UUID) error
	listReactions  func(ctx context.Context, postID uuid.UUID, limit, offset int) ([]Reaction, error)
	toggleReaction func(ctx context.Context, postID, userID uuid.UUID, reactionType string) (bool, error)
	addComment     func(ctx context.Context, postID, userID uuid.UUID, body string) (uuid.UUID, error)
	listComments   func(ctx context.Context, postID uuid.UUID, after *time.Time, limit int) ([]Comment, error)
}

func (m *mockQuerier) ListFeed(ctx context.Context, before *time.Time, limit int) ([]Post, error) {
	if m.listFeed != nil {
		return m.listFeed(ctx, before, limit)
	}
	return nil, nil
}
func (m *mockQuerier) ListUserPosts(ctx context.Context, userID uuid.UUID, before *time.Time, limit int) ([]Post, error) {
	if m.listUserPosts != nil {
		return m.listUserPosts(ctx, userID, before, limit)
	}
	return nil, nil
}
func (m *mockQuerier) CreatePost(ctx context.Context, userID uuid.UUID, body string) (uuid.UUID, error) {
	if m.createPost != nil {
		return m.createPost(ctx, userID, body)
	}
	return uuid.New(), nil
}
func (m *mockQuerier) DeletePost(ctx context.Context, postID, userID uuid.UUID) error {
	if m.deletePost != nil {
		return m.deletePost(ctx, postID, userID)
	}
	return nil
}
func (m *mockQuerier) ListReactions(ctx context.Context, postID uuid.UUID, limit, offset int) ([]Reaction, error) {
	if m.listReactions != nil {
		return m.listReactions(ctx, postID, limit, offset)
	}
	return nil, nil
}
func (m *mockQuerier) ToggleReaction(ctx context.Context, postID, userID uuid.UUID, reactionType string) (bool, error) {
	if m.toggleReaction != nil {
		return m.toggleReaction(ctx, postID, userID, reactionType)
	}
	return true, nil
}
func (m *mockQuerier) AddComment(ctx context.Context, postID, userID uuid.UUID, body string) (uuid.UUID, error) {
	if m.addComment != nil {
		return m.addComment(ctx, postID, userID, body)
	}
	return uuid.New(), nil
}
func (m *mockQuerier) ListComments(ctx context.Context, postID uuid.UUID, after *time.Time, limit int) ([]Comment, error) {
	if m.listComments != nil {
		return m.listComments(ctx, postID, after, limit)
	}
	return nil, nil
}

var (
	fixedUser = uuid.MustParse("00000000-0000-0000-0000-000000000001")
	fixedPost = uuid.MustParse("00000000-0000-0000-0000-000000000002")
)

func withUserID(r *http.Request, id uuid.UUID) *http.Request {
	return r.WithContext(context.WithValue(r.Context(), middleware.UserIDKey, id))
}

func withURLParam(r *http.Request, key, value string) *http.Request {
	rctx := chi.NewRouteContext()
	rctx.URLParams.Add(key, value)
	return r.WithContext(context.WithValue(r.Context(), chi.RouteCtxKey, rctx))
}

// ── GetFeed ───────────────────────────────────────────────────────────────────

func TestGetFeedSuccess(t *testing.T) {
	h := NewHandler(&mockQuerier{
		listFeed: func(_ context.Context, _ *time.Time, _ int) ([]Post, error) {
			return []Post{{ID: fixedPost, Body: "hello", CreatedAt: time.Now()}}, nil
		},
	})
	req := httptest.NewRequest(http.MethodGet, "/feed", nil)
	rec := httptest.NewRecorder()

	h.GetFeed(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusOK)
	}
}

func TestGetFeedDBError(t *testing.T) {
	h := NewHandler(&mockQuerier{
		listFeed: func(_ context.Context, _ *time.Time, _ int) ([]Post, error) {
			return nil, errors.New("db error")
		},
	})
	rec := httptest.NewRecorder()
	h.GetFeed(rec, httptest.NewRequest(http.MethodGet, "/feed", nil))
	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusInternalServerError)
	}
}

// ── GetUserPosts ──────────────────────────────────────────────────────────────

func TestGetUserPostsRejectsInvalidUUID(t *testing.T) {
	h := NewHandler(&mockQuerier{})
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req = withURLParam(req, "id", "not-a-uuid")
	rec := httptest.NewRecorder()

	h.GetUserPosts(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusBadRequest)
	}
}

func TestGetUserPostsSuccess(t *testing.T) {
	h := NewHandler(&mockQuerier{})
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req = withURLParam(req, "id", fixedUser.String())
	rec := httptest.NewRecorder()

	h.GetUserPosts(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusOK)
	}
}

// ── CreatePost ────────────────────────────────────────────────────────────────

func TestCreatePostRejectsEmptyBody(t *testing.T) {
	h := NewHandler(&mockQuerier{})
	req := httptest.NewRequest(http.MethodPost, "/posts", strings.NewReader(`{"body":""}`))
	req = withUserID(req, fixedUser)
	rec := httptest.NewRecorder()

	h.CreatePost(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusBadRequest)
	}
}

func TestCreatePostSuccess(t *testing.T) {
	h := NewHandler(&mockQuerier{})
	req := httptest.NewRequest(http.MethodPost, "/posts", strings.NewReader(`{"body":"hello world"}`))
	req = withUserID(req, fixedUser)
	rec := httptest.NewRecorder()

	h.CreatePost(rec, req)

	if rec.Code != http.StatusCreated {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusCreated)
	}
}

func TestCreatePostDBError(t *testing.T) {
	h := NewHandler(&mockQuerier{
		createPost: func(_ context.Context, _ uuid.UUID, _ string) (uuid.UUID, error) {
			return uuid.Nil, errors.New("db error")
		},
	})
	req := httptest.NewRequest(http.MethodPost, "/posts", strings.NewReader(`{"body":"hello"}`))
	req = withUserID(req, fixedUser)
	rec := httptest.NewRecorder()

	h.CreatePost(rec, req)

	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusInternalServerError)
	}
}

// ── DeletePost ────────────────────────────────────────────────────────────────

func TestDeletePostRejectsInvalidUUID(t *testing.T) {
	h := NewHandler(&mockQuerier{})
	req := httptest.NewRequest(http.MethodDelete, "/", nil)
	req = withUserID(req, fixedUser)
	req = withURLParam(req, "id", "bad")
	rec := httptest.NewRecorder()

	h.DeletePost(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusBadRequest)
	}
}

func TestDeletePostNotFound(t *testing.T) {
	h := NewHandler(&mockQuerier{
		deletePost: func(_ context.Context, _, _ uuid.UUID) error { return ErrNotFound },
	})
	req := httptest.NewRequest(http.MethodDelete, "/", nil)
	req = withUserID(req, fixedUser)
	req = withURLParam(req, "id", fixedPost.String())
	rec := httptest.NewRecorder()

	h.DeletePost(rec, req)

	if rec.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusNotFound)
	}
}

func TestDeletePostSuccess(t *testing.T) {
	h := NewHandler(&mockQuerier{})
	req := httptest.NewRequest(http.MethodDelete, "/", nil)
	req = withUserID(req, fixedUser)
	req = withURLParam(req, "id", fixedPost.String())
	rec := httptest.NewRecorder()

	h.DeletePost(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusOK)
	}
}

// ── GetReactions ──────────────────────────────────────────────────────────────

func TestGetReactionsRejectsInvalidUUID(t *testing.T) {
	h := NewHandler(&mockQuerier{})
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req = withURLParam(req, "id", "bad")
	rec := httptest.NewRecorder()

	h.GetReactions(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusBadRequest)
	}
}

func TestGetReactionsSuccess(t *testing.T) {
	h := NewHandler(&mockQuerier{})
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req = withURLParam(req, "id", fixedPost.String())
	rec := httptest.NewRecorder()

	h.GetReactions(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusOK)
	}
}

// ── ReactToPost ───────────────────────────────────────────────────────────────

func TestReactToPostRejectsInvalidUUID(t *testing.T) {
	h := NewHandler(&mockQuerier{})
	req := httptest.NewRequest(http.MethodPost, "/", strings.NewReader(`{"type":"like"}`))
	req = withUserID(req, fixedUser)
	req = withURLParam(req, "id", "bad")
	rec := httptest.NewRecorder()

	h.ReactToPost(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusBadRequest)
	}
}

func TestReactToPostTogglesOn(t *testing.T) {
	h := NewHandler(&mockQuerier{
		toggleReaction: func(_ context.Context, _, _ uuid.UUID, _ string) (bool, error) {
			return true, nil
		},
	})
	req := httptest.NewRequest(http.MethodPost, "/", strings.NewReader(`{"type":"like"}`))
	req = withUserID(req, fixedUser)
	req = withURLParam(req, "id", fixedPost.String())
	rec := httptest.NewRecorder()

	h.ReactToPost(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusOK)
	}
}

func TestReactToPostTogglesOff(t *testing.T) {
	h := NewHandler(&mockQuerier{
		toggleReaction: func(_ context.Context, _, _ uuid.UUID, _ string) (bool, error) {
			return false, nil
		},
	})
	req := httptest.NewRequest(http.MethodPost, "/", strings.NewReader(`{"type":"like"}`))
	req = withUserID(req, fixedUser)
	req = withURLParam(req, "id", fixedPost.String())
	rec := httptest.NewRecorder()

	h.ReactToPost(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusOK)
	}
}

// ── AddComment ────────────────────────────────────────────────────────────────

func TestAddCommentRejectsInvalidUUID(t *testing.T) {
	h := NewHandler(&mockQuerier{})
	req := httptest.NewRequest(http.MethodPost, "/", strings.NewReader(`{"body":"hi"}`))
	req = withUserID(req, fixedUser)
	req = withURLParam(req, "id", "bad")
	rec := httptest.NewRecorder()

	h.AddComment(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusBadRequest)
	}
}

func TestAddCommentRejectsEmptyBody(t *testing.T) {
	h := NewHandler(&mockQuerier{})
	req := httptest.NewRequest(http.MethodPost, "/", strings.NewReader(`{"body":""}`))
	req = withUserID(req, fixedUser)
	req = withURLParam(req, "id", fixedPost.String())
	rec := httptest.NewRecorder()

	h.AddComment(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusBadRequest)
	}
}

func TestAddCommentSuccess(t *testing.T) {
	h := NewHandler(&mockQuerier{})
	req := httptest.NewRequest(http.MethodPost, "/", strings.NewReader(`{"body":"great post"}`))
	req = withUserID(req, fixedUser)
	req = withURLParam(req, "id", fixedPost.String())
	rec := httptest.NewRecorder()

	h.AddComment(rec, req)

	if rec.Code != http.StatusCreated {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusCreated)
	}
}

// ── GetComments ───────────────────────────────────────────────────────────────

func TestGetCommentsRejectsInvalidUUID(t *testing.T) {
	h := NewHandler(&mockQuerier{})
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req = withURLParam(req, "id", "bad")
	rec := httptest.NewRecorder()

	h.GetComments(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusBadRequest)
	}
}

func TestGetCommentsSuccess(t *testing.T) {
	h := NewHandler(&mockQuerier{})
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req = withURLParam(req, "id", fixedPost.String())
	rec := httptest.NewRecorder()

	h.GetComments(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusOK)
	}
}
