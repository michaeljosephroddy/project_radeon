package feed

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"image"
	"image/color"
	"image/png"
	"io"
	"mime/multipart"
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
	listFeed            func(ctx context.Context, before *time.Time, limit int) ([]Post, error)
	listUserPosts       func(ctx context.Context, userID uuid.UUID, before *time.Time, limit int) ([]Post, error)
	createPost          func(ctx context.Context, userID uuid.UUID, body string, images []PostImage) (uuid.UUID, error)
	deletePost          func(ctx context.Context, postID, userID uuid.UUID) error
	listReactions       func(ctx context.Context, postID uuid.UUID, limit, offset int) ([]Reaction, error)
	toggleReaction      func(ctx context.Context, postID, userID uuid.UUID, reactionType string) (bool, error)
	resolveMentionUsers func(ctx context.Context, userIDs []uuid.UUID) ([]MentionedUser, error)
	addComment          func(ctx context.Context, postID, userID uuid.UUID, body string, mentions []CommentMention) (*Comment, error)
	listComments        func(ctx context.Context, postID uuid.UUID, after *time.Time, limit int) ([]Comment, error)
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
func (m *mockQuerier) CreatePost(ctx context.Context, userID uuid.UUID, body string, images []PostImage) (uuid.UUID, error) {
	if m.createPost != nil {
		return m.createPost(ctx, userID, body, images)
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
func (m *mockQuerier) ResolveMentionUsers(ctx context.Context, userIDs []uuid.UUID) ([]MentionedUser, error) {
	if m.resolveMentionUsers != nil {
		return m.resolveMentionUsers(ctx, userIDs)
	}
	return nil, nil
}
func (m *mockQuerier) AddComment(ctx context.Context, postID, userID uuid.UUID, body string, mentions []CommentMention) (*Comment, error) {
	if m.addComment != nil {
		return m.addComment(ctx, postID, userID, body, mentions)
	}
	return &Comment{ID: uuid.New(), UserID: userID, Body: body, Mentions: mentions, CreatedAt: time.Now()}, nil
}
func (m *mockQuerier) ListComments(ctx context.Context, postID uuid.UUID, after *time.Time, limit int) ([]Comment, error) {
	if m.listComments != nil {
		return m.listComments(ctx, postID, after, limit)
	}
	return nil, nil
}

type mockUploader struct {
	upload func(ctx context.Context, key, contentType string, body io.Reader) (string, error)
}

func (m *mockUploader) Upload(ctx context.Context, key, contentType string, body io.Reader) (string, error) {
	if m.upload != nil {
		return m.upload(ctx, key, contentType, body)
	}
	return "https://example.com/post.jpg", nil
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
	}, &mockUploader{})
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
	}, &mockUploader{})
	rec := httptest.NewRecorder()
	h.GetFeed(rec, httptest.NewRequest(http.MethodGet, "/feed", nil))
	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusInternalServerError)
	}
}

// ── GetUserPosts ──────────────────────────────────────────────────────────────

func TestGetUserPostsRejectsInvalidUUID(t *testing.T) {
	h := NewHandler(&mockQuerier{}, &mockUploader{})
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req = withURLParam(req, "id", "not-a-uuid")
	rec := httptest.NewRecorder()

	h.GetUserPosts(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusBadRequest)
	}
}

func TestGetUserPostsSuccess(t *testing.T) {
	h := NewHandler(&mockQuerier{}, &mockUploader{})
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
	h := NewHandler(&mockQuerier{}, &mockUploader{})
	req := httptest.NewRequest(http.MethodPost, "/posts", strings.NewReader(`{"body":""}`))
	req = withUserID(req, fixedUser)
	rec := httptest.NewRecorder()

	h.CreatePost(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusBadRequest)
	}
}

func TestCreatePostAllowsImageOnly(t *testing.T) {
	var gotImages []PostImage
	h := NewHandler(&mockQuerier{
		createPost: func(_ context.Context, _ uuid.UUID, body string, images []PostImage) (uuid.UUID, error) {
			if body != "" {
				t.Fatalf("body = %q, want empty", body)
			}
			gotImages = images
			return uuid.New(), nil
		},
	}, &mockUploader{})
	req := httptest.NewRequest(http.MethodPost, "/posts", strings.NewReader(`{"images":[{"image_url":"https://example.com/post.jpg","width":1200,"height":900}]}`))
	req = withUserID(req, fixedUser)
	rec := httptest.NewRecorder()

	h.CreatePost(rec, req)

	if rec.Code != http.StatusCreated {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusCreated)
	}
	if len(gotImages) != 1 {
		t.Fatalf("images length = %d, want 1", len(gotImages))
	}
}

func TestCreatePostSuccess(t *testing.T) {
	h := NewHandler(&mockQuerier{}, &mockUploader{})
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
		createPost: func(_ context.Context, _ uuid.UUID, _ string, _ []PostImage) (uuid.UUID, error) {
			return uuid.Nil, errors.New("db error")
		},
	}, &mockUploader{})
	req := httptest.NewRequest(http.MethodPost, "/posts", strings.NewReader(`{"body":"hello"}`))
	req = withUserID(req, fixedUser)
	rec := httptest.NewRecorder()

	h.CreatePost(rec, req)

	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusInternalServerError)
	}
}

func TestUploadPostImageSuccess(t *testing.T) {
	var body bytes.Buffer
	writer := multipart.NewWriter(&body)
	part, err := writer.CreateFormFile("image", "post.png")
	if err != nil {
		t.Fatalf("CreateFormFile error = %v", err)
	}

	img := image.NewRGBA(image.Rect(0, 0, 8, 8))
	img.Set(0, 0, color.RGBA{R: 255, A: 255})
	if err := png.Encode(part, img); err != nil {
		t.Fatalf("png.Encode error = %v", err)
	}
	if err := writer.Close(); err != nil {
		t.Fatalf("writer.Close error = %v", err)
	}

	h := NewHandler(&mockQuerier{}, &mockUploader{})
	req := httptest.NewRequest(http.MethodPost, "/posts/images", &body)
	req = withUserID(req, fixedUser)
	req.Header.Set("Content-Type", writer.FormDataContentType())
	rec := httptest.NewRecorder()

	h.UploadPostImage(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusOK)
	}
}

// ── DeletePost ────────────────────────────────────────────────────────────────

func TestDeletePostRejectsInvalidUUID(t *testing.T) {
	h := NewHandler(&mockQuerier{}, &mockUploader{})
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
	}, &mockUploader{})
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
	h := NewHandler(&mockQuerier{}, &mockUploader{})
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
	h := NewHandler(&mockQuerier{}, &mockUploader{})
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req = withURLParam(req, "id", "bad")
	rec := httptest.NewRecorder()

	h.GetReactions(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusBadRequest)
	}
}

func TestGetReactionsSuccess(t *testing.T) {
	h := NewHandler(&mockQuerier{}, &mockUploader{})
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
	h := NewHandler(&mockQuerier{}, &mockUploader{})
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
	}, &mockUploader{})
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
	}, &mockUploader{})
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
	h := NewHandler(&mockQuerier{}, &mockUploader{})
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
	h := NewHandler(&mockQuerier{}, &mockUploader{})
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
	h := NewHandler(&mockQuerier{}, &mockUploader{})
	req := httptest.NewRequest(http.MethodPost, "/", strings.NewReader(`{"body":"great post"}`))
	req = withUserID(req, fixedUser)
	req = withURLParam(req, "id", fixedPost.String())
	rec := httptest.NewRecorder()

	h.AddComment(rec, req)

	if rec.Code != http.StatusCreated {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusCreated)
	}
}

func TestAddCommentResolvesAndReturnsMentions(t *testing.T) {
	mentionedUser := uuid.MustParse("00000000-0000-0000-0000-000000000099")
	h := NewHandler(&mockQuerier{
		resolveMentionUsers: func(_ context.Context, userIDs []uuid.UUID) ([]MentionedUser, error) {
			if len(userIDs) != 1 || userIDs[0] != mentionedUser {
				t.Fatalf("unexpected mention ids: %#v", userIDs)
			}
			return []MentionedUser{{UserID: mentionedUser, Username: "alex"}}, nil
		},
		addComment: func(_ context.Context, _, _ uuid.UUID, body string, mentions []CommentMention) (*Comment, error) {
			if body != "hi @alex and @alex again" {
				t.Fatalf("body = %q", body)
			}
			if len(mentions) != 1 || mentions[0].UserID != mentionedUser || mentions[0].Username != "alex" {
				t.Fatalf("mentions = %#v", mentions)
			}
			return &Comment{
				ID:        uuid.New(),
				UserID:    fixedUser,
				Username:  "michael",
				Body:      body,
				CreatedAt: time.Now(),
				Mentions:  mentions,
			}, nil
		},
	}, &mockUploader{})

	req := httptest.NewRequest(http.MethodPost, "/", strings.NewReader(`{"body":"hi @alex and @alex again","mention_user_ids":["00000000-0000-0000-0000-000000000099"]}`))
	req = withUserID(req, fixedUser)
	req = withURLParam(req, "id", fixedPost.String())
	rec := httptest.NewRecorder()

	h.AddComment(rec, req)

	if rec.Code != http.StatusCreated {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusCreated)
	}

	var payload struct {
		Data Comment `json:"data"`
	}
	if err := json.NewDecoder(rec.Body).Decode(&payload); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if len(payload.Data.Mentions) != 1 || payload.Data.Mentions[0].Username != "alex" {
		t.Fatalf("response mentions = %#v", payload.Data.Mentions)
	}
}

// ── GetComments ───────────────────────────────────────────────────────────────

func TestGetCommentsRejectsInvalidUUID(t *testing.T) {
	h := NewHandler(&mockQuerier{}, &mockUploader{})
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req = withURLParam(req, "id", "bad")
	rec := httptest.NewRecorder()

	h.GetComments(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusBadRequest)
	}
}

func TestGetCommentsSuccess(t *testing.T) {
	h := NewHandler(&mockQuerier{}, &mockUploader{})
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req = withURLParam(req, "id", fixedPost.String())
	rec := httptest.NewRecorder()

	h.GetComments(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusOK)
	}
}

func TestExtractMentionHandles(t *testing.T) {
	handles := extractMentionHandles("hello @alex, meet @sam_1 and @alex again. not-an-email test@example.com")
	if len(handles) != 2 || handles[0] != "alex" || handles[1] != "sam_1" {
		t.Fatalf("handles = %#v", handles)
	}
}
