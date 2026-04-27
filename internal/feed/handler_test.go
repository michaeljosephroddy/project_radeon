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
	listHomeFeed           func(ctx context.Context, viewerID uuid.UUID, before *time.Time, limit int) ([]FeedItem, error)
	listHiddenFeedItems    func(ctx context.Context, userID uuid.UUID, before *time.Time, limit int) ([]HiddenFeedItem, error)
	listUserPosts          func(ctx context.Context, userID uuid.UUID, before *time.Time, limit int) ([]Post, error)
	createPost             func(ctx context.Context, userID uuid.UUID, body string, images []PostImage) (uuid.UUID, error)
	sharePost              func(ctx context.Context, userID, postID uuid.UUID, commentary string) (uuid.UUID, error)
	deletePost             func(ctx context.Context, postID, userID uuid.UUID) error
	hideFeedItem           func(ctx context.Context, userID, itemID uuid.UUID, itemKind FeedItemKind) error
	unhideFeedItem         func(ctx context.Context, userID, itemID uuid.UUID, itemKind FeedItemKind) error
	muteFeedAuthor         func(ctx context.Context, userID, authorID uuid.UUID) error
	logFeedImpressions     func(ctx context.Context, userID uuid.UUID, impressions []FeedImpressionInput) error
	logFeedEvents          func(ctx context.Context, userID uuid.UUID, events []FeedEventInput) error
	listReactions          func(ctx context.Context, postID uuid.UUID, limit, offset int) ([]Reaction, error)
	toggleFeedItemReaction func(ctx context.Context, itemID, userID uuid.UUID, itemKind FeedItemKind, reactionType string) (bool, error)
	toggleReaction         func(ctx context.Context, postID, userID uuid.UUID, reactionType string) (bool, error)
	resolveMentionUsers    func(ctx context.Context, userIDs []uuid.UUID) ([]MentionedUser, error)
	addFeedItemComment     func(ctx context.Context, itemID, userID uuid.UUID, itemKind FeedItemKind, body string, mentions []CommentMention) (*Comment, error)
	addComment             func(ctx context.Context, postID, userID uuid.UUID, body string, mentions []CommentMention) (*Comment, error)
	listFeedItemComments   func(ctx context.Context, itemID uuid.UUID, itemKind FeedItemKind, after *time.Time, limit int) ([]Comment, error)
	listComments           func(ctx context.Context, postID uuid.UUID, after *time.Time, limit int) ([]Comment, error)
}

func (m *mockQuerier) ListHomeFeed(ctx context.Context, viewerID uuid.UUID, before *time.Time, limit int) ([]FeedItem, error) {
	if m.listHomeFeed != nil {
		return m.listHomeFeed(ctx, viewerID, before, limit)
	}
	return nil, nil
}
func (m *mockQuerier) ListHiddenFeedItems(ctx context.Context, userID uuid.UUID, before *time.Time, limit int) ([]HiddenFeedItem, error) {
	if m.listHiddenFeedItems != nil {
		return m.listHiddenFeedItems(ctx, userID, before, limit)
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
func (m *mockQuerier) SharePost(ctx context.Context, userID, postID uuid.UUID, commentary string) (uuid.UUID, error) {
	if m.sharePost != nil {
		return m.sharePost(ctx, userID, postID, commentary)
	}
	return uuid.New(), nil
}
func (m *mockQuerier) DeletePost(ctx context.Context, postID, userID uuid.UUID) error {
	if m.deletePost != nil {
		return m.deletePost(ctx, postID, userID)
	}
	return nil
}
func (m *mockQuerier) HideFeedItem(ctx context.Context, userID, itemID uuid.UUID, itemKind FeedItemKind) error {
	if m.hideFeedItem != nil {
		return m.hideFeedItem(ctx, userID, itemID, itemKind)
	}
	return nil
}
func (m *mockQuerier) UnhideFeedItem(ctx context.Context, userID, itemID uuid.UUID, itemKind FeedItemKind) error {
	if m.unhideFeedItem != nil {
		return m.unhideFeedItem(ctx, userID, itemID, itemKind)
	}
	return nil
}
func (m *mockQuerier) MuteFeedAuthor(ctx context.Context, userID, authorID uuid.UUID) error {
	if m.muteFeedAuthor != nil {
		return m.muteFeedAuthor(ctx, userID, authorID)
	}
	return nil
}
func (m *mockQuerier) LogFeedImpressions(ctx context.Context, userID uuid.UUID, impressions []FeedImpressionInput) error {
	if m.logFeedImpressions != nil {
		return m.logFeedImpressions(ctx, userID, impressions)
	}
	return nil
}
func (m *mockQuerier) LogFeedEvents(ctx context.Context, userID uuid.UUID, events []FeedEventInput) error {
	if m.logFeedEvents != nil {
		return m.logFeedEvents(ctx, userID, events)
	}
	return nil
}
func (m *mockQuerier) ListReactions(ctx context.Context, postID uuid.UUID, limit, offset int) ([]Reaction, error) {
	if m.listReactions != nil {
		return m.listReactions(ctx, postID, limit, offset)
	}
	return nil, nil
}
func (m *mockQuerier) ToggleFeedItemReaction(ctx context.Context, itemID, userID uuid.UUID, itemKind FeedItemKind, reactionType string) (bool, error) {
	if m.toggleFeedItemReaction != nil {
		return m.toggleFeedItemReaction(ctx, itemID, userID, itemKind, reactionType)
	}
	return true, nil
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
func (m *mockQuerier) AddFeedItemComment(ctx context.Context, itemID, userID uuid.UUID, itemKind FeedItemKind, body string, mentions []CommentMention) (*Comment, error) {
	if m.addFeedItemComment != nil {
		return m.addFeedItemComment(ctx, itemID, userID, itemKind, body, mentions)
	}
	return &Comment{ID: uuid.New(), UserID: userID, Body: body, Mentions: mentions, CreatedAt: time.Now()}, nil
}
func (m *mockQuerier) AddComment(ctx context.Context, postID, userID uuid.UUID, body string, mentions []CommentMention) (*Comment, error) {
	if m.addComment != nil {
		return m.addComment(ctx, postID, userID, body, mentions)
	}
	return &Comment{ID: uuid.New(), UserID: userID, Body: body, Mentions: mentions, CreatedAt: time.Now()}, nil
}
func (m *mockQuerier) ListFeedItemComments(ctx context.Context, itemID uuid.UUID, itemKind FeedItemKind, after *time.Time, limit int) ([]Comment, error) {
	if m.listFeedItemComments != nil {
		return m.listFeedItemComments(ctx, itemID, itemKind, after, limit)
	}
	return nil, nil
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

func TestGetHomeFeedSuccess(t *testing.T) {
	h := NewHandler(&mockQuerier{
		listHomeFeed: func(_ context.Context, viewerID uuid.UUID, _ *time.Time, _ int) ([]FeedItem, error) {
			if viewerID != fixedUser {
				t.Fatalf("viewerID = %s, want %s", viewerID, fixedUser)
			}
			return []FeedItem{{ID: fixedPost, Kind: FeedItemKindPost, CreatedAt: time.Now()}}, nil
		},
	}, &mockUploader{})
	req := httptest.NewRequest(http.MethodGet, "/feed/home", nil)
	req = withUserID(req, fixedUser)
	rec := httptest.NewRecorder()

	h.GetHomeFeed(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusOK)
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
	if gotImages[0].ImageURL != "https://example.com/post.jpg" {
		t.Fatalf("image_url = %q, want %q", gotImages[0].ImageURL, "https://example.com/post.jpg")
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

func TestSharePostRejectsInvalidUUID(t *testing.T) {
	h := NewHandler(&mockQuerier{}, &mockUploader{})
	req := httptest.NewRequest(http.MethodPost, "/posts/bad/share", strings.NewReader(`{"commentary":"worth reading"}`))
	req = withUserID(req, fixedUser)
	req = withURLParam(req, "id", "bad")
	rec := httptest.NewRecorder()

	h.SharePost(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusBadRequest)
	}
}

func TestSharePostSuccess(t *testing.T) {
	var gotCommentary string
	h := NewHandler(&mockQuerier{
		sharePost: func(_ context.Context, userID, postID uuid.UUID, commentary string) (uuid.UUID, error) {
			if userID != fixedUser {
				t.Fatalf("userID = %s, want %s", userID, fixedUser)
			}
			if postID != fixedPost {
				t.Fatalf("postID = %s, want %s", postID, fixedPost)
			}
			gotCommentary = commentary
			return uuid.New(), nil
		},
	}, &mockUploader{})
	req := httptest.NewRequest(http.MethodPost, "/posts/share", strings.NewReader(`{"commentary":" worth reading "}`))
	req = withUserID(req, fixedUser)
	req = withURLParam(req, "id", fixedPost.String())
	rec := httptest.NewRecorder()

	h.SharePost(rec, req)

	if rec.Code != http.StatusCreated {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusCreated)
	}
	if gotCommentary != " worth reading " {
		t.Fatalf("commentary = %q, want preserved input", gotCommentary)
	}
}

func TestSharePostNotFound(t *testing.T) {
	h := NewHandler(&mockQuerier{
		sharePost: func(_ context.Context, _, _ uuid.UUID, _ string) (uuid.UUID, error) {
			return uuid.Nil, ErrNotFound
		},
	}, &mockUploader{})
	req := httptest.NewRequest(http.MethodPost, "/posts/share", strings.NewReader(`{}`))
	req = withUserID(req, fixedUser)
	req = withURLParam(req, "id", fixedPost.String())
	rec := httptest.NewRecorder()

	h.SharePost(rec, req)

	if rec.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusNotFound)
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

	var uploadKeys []string
	h := NewHandler(&mockQuerier{}, &mockUploader{
		upload: func(_ context.Context, key, _ string, _ io.Reader) (string, error) {
			uploadKeys = append(uploadKeys, key)
			return "https://example.com/" + key, nil
		},
	})
	req := httptest.NewRequest(http.MethodPost, "/posts/images", &body)
	req = withUserID(req, fixedUser)
	req.Header.Set("Content-Type", writer.FormDataContentType())
	rec := httptest.NewRecorder()

	h.UploadPostImage(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusOK)
	}
	if len(uploadKeys) != 1 {
		t.Fatalf("upload count = %d, want 1", len(uploadKeys))
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

func TestHideFeedItemRejectsInvalidKind(t *testing.T) {
	h := NewHandler(&mockQuerier{
		hideFeedItem: func(_ context.Context, _, _ uuid.UUID, itemKind FeedItemKind) error {
			if itemKind != FeedItemKind("mystery") {
				t.Fatalf("itemKind = %q, want mystery", itemKind)
			}
			return ErrInvalidFeedItemKind
		},
	}, &mockUploader{})
	req := httptest.NewRequest(http.MethodPost, "/feed/items/hide", strings.NewReader(`{"item_kind":"mystery"}`))
	req = withUserID(req, fixedUser)
	req = withURLParam(req, "id", fixedPost.String())
	rec := httptest.NewRecorder()

	h.HideFeedItem(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusBadRequest)
	}
}

func TestMuteFeedAuthorSuccess(t *testing.T) {
	var gotAuthorID uuid.UUID
	h := NewHandler(&mockQuerier{
		muteFeedAuthor: func(_ context.Context, userID, authorID uuid.UUID) error {
			if userID != fixedUser {
				t.Fatalf("userID = %s, want %s", userID, fixedUser)
			}
			gotAuthorID = authorID
			return nil
		},
	}, &mockUploader{})
	req := httptest.NewRequest(http.MethodPost, "/feed/authors/mute", nil)
	req = withUserID(req, fixedUser)
	req = withURLParam(req, "id", fixedPost.String())
	rec := httptest.NewRecorder()

	h.MuteFeedAuthor(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusOK)
	}
	if gotAuthorID != fixedPost {
		t.Fatalf("authorID = %s, want %s", gotAuthorID, fixedPost)
	}
}

func TestLogFeedImpressionsSuccess(t *testing.T) {
	var gotCount int
	h := NewHandler(&mockQuerier{
		logFeedImpressions: func(_ context.Context, userID uuid.UUID, impressions []FeedImpressionInput) error {
			if userID != fixedUser {
				t.Fatalf("userID = %s, want %s", userID, fixedUser)
			}
			gotCount = len(impressions)
			return nil
		},
	}, &mockUploader{})
	req := httptest.NewRequest(http.MethodPost, "/feed/impressions", strings.NewReader(`{"impressions":[{"item_id":"`+fixedPost.String()+`","item_kind":"post","feed_mode":"home","session_id":"abc","position":0,"served_at":"2026-04-27T12:00:00Z","viewed_at":"2026-04-27T12:00:01Z","view_ms":1200}]}`))
	req = withUserID(req, fixedUser)
	rec := httptest.NewRecorder()

	h.LogFeedImpressions(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusOK)
	}
	if gotCount != 1 {
		t.Fatalf("logged count = %d, want 1", gotCount)
	}
}

func TestLogFeedEventsRejectsInvalidEvent(t *testing.T) {
	h := NewHandler(&mockQuerier{
		logFeedEvents: func(_ context.Context, _ uuid.UUID, _ []FeedEventInput) error {
			return ErrInvalidFeedEvent
		},
	}, &mockUploader{})
	req := httptest.NewRequest(http.MethodPost, "/feed/events", strings.NewReader(`{"events":[{"item_id":"`+fixedPost.String()+`","item_kind":"post","feed_mode":"home","event_type":"mystery"}]}`))
	req = withUserID(req, fixedUser)
	rec := httptest.NewRecorder()

	h.LogFeedEvents(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusBadRequest)
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
