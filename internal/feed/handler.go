package feed

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"image"
	"io"
	"log"
	"net/http"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/project_radeon/api/pkg/middleware"
	"github.com/project_radeon/api/pkg/pagination"
	"github.com/project_radeon/api/pkg/response"
	_ "image/jpeg"
	_ "image/png"
)

// Querier is the database interface required by the feed handler.
type Querier interface {
	ListHomeFeed(ctx context.Context, viewerID uuid.UUID, before *time.Time, limit int) ([]FeedItem, error)
	ListUserPosts(ctx context.Context, userID uuid.UUID, before *time.Time, limit int) ([]Post, error)
	CreatePost(ctx context.Context, userID uuid.UUID, body string, images []PostImage) (uuid.UUID, error)
	SharePost(ctx context.Context, userID, postID uuid.UUID, commentary string) (uuid.UUID, error)
	DeletePost(ctx context.Context, postID, userID uuid.UUID) error
	HideFeedItem(ctx context.Context, userID, itemID uuid.UUID, itemKind FeedItemKind) error
	UnhideFeedItem(ctx context.Context, userID, itemID uuid.UUID, itemKind FeedItemKind) error
	ListHiddenFeedItems(ctx context.Context, userID uuid.UUID, before *time.Time, limit int) ([]HiddenFeedItem, error)
	MuteFeedAuthor(ctx context.Context, userID, authorID uuid.UUID) error
	LogFeedImpressions(ctx context.Context, userID uuid.UUID, impressions []FeedImpressionInput) error
	LogFeedEvents(ctx context.Context, userID uuid.UUID, events []FeedEventInput) error
	ListReactions(ctx context.Context, postID uuid.UUID, limit, offset int) ([]Reaction, error)
	ToggleFeedItemReaction(ctx context.Context, itemID, userID uuid.UUID, itemKind FeedItemKind, reactionType string) (reacted bool, err error)
	AddFeedItemComment(ctx context.Context, itemID, userID uuid.UUID, itemKind FeedItemKind, body string, mentions []CommentMention) (*Comment, error)
	ListFeedItemComments(ctx context.Context, itemID uuid.UUID, itemKind FeedItemKind, after *time.Time, limit int) ([]Comment, error)
	ToggleReaction(ctx context.Context, postID, userID uuid.UUID, reactionType string) (reacted bool, err error)
	ResolveMentionUsers(ctx context.Context, userIDs []uuid.UUID) ([]MentionedUser, error)
	AddComment(ctx context.Context, postID, userID uuid.UUID, body string, mentions []CommentMention) (*Comment, error)
	ListComments(ctx context.Context, postID uuid.UUID, after *time.Time, limit int) ([]Comment, error)
}

type Uploader interface {
	Upload(ctx context.Context, key, contentType string, body io.Reader) (string, error)
}

type MentionNotifier interface {
	NotifyCommentMentions(ctx context.Context, postID, commentID, authorID uuid.UUID, mentionedUserIDs []uuid.UUID, body string) error
}

type Handler struct {
	db       Querier
	notifier MentionNotifier
	uploader Uploader
}

// NewHandler builds a feed handler. Pass feed.NewPgStore(pool) for production.
func NewHandler(db Querier, uploader Uploader) *Handler {
	return &Handler{db: db, uploader: uploader}
}

func NewHandlerWithNotifier(db Querier, notifier MentionNotifier, uploader Uploader) *Handler {
	return &Handler{db: db, notifier: notifier, uploader: uploader}
}

type Post struct {
	ID           uuid.UUID   `json:"id"`
	UserID       uuid.UUID   `json:"user_id"`
	Username     string      `json:"username"`
	AvatarURL    *string     `json:"avatar_url"`
	Body         string      `json:"body"`
	CreatedAt    time.Time   `json:"created_at"`
	CommentCount int         `json:"comment_count"`
	LikeCount    int         `json:"like_count"`
	Images       []PostImage `json:"images"`
}

type PostImage struct {
	ID        uuid.UUID `json:"id"`
	ImageURL  string    `json:"image_url"`
	Width     int       `json:"width"`
	Height    int       `json:"height"`
	SortOrder int       `json:"sort_order"`
}

type Reaction struct {
	ID        uuid.UUID `json:"id"`
	UserID    uuid.UUID `json:"user_id"`
	Username  string    `json:"username"`
	AvatarURL *string   `json:"avatar_url"`
	Type      string    `json:"type"`
}

type Comment struct {
	ID        uuid.UUID        `json:"id"`
	UserID    uuid.UUID        `json:"user_id"`
	Username  string           `json:"username"`
	AvatarURL *string          `json:"avatar_url"`
	Body      string           `json:"body"`
	CreatedAt time.Time        `json:"created_at"`
	Mentions  []CommentMention `json:"mentions"`
}

type CommentMention struct {
	UserID   uuid.UUID `json:"user_id"`
	Username string    `json:"username"`
}

type MentionedUser struct {
	UserID   uuid.UUID
	Username string
}

func isFeedValidationError(err error) bool {
	return errors.Is(err, ErrInvalidFeedMode) ||
		errors.Is(err, ErrInvalidFeedItemKind) ||
		errors.Is(err, ErrInvalidFeedEvent)
}

func parseFeedItemKind(raw string) (FeedItemKind, error) {
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" {
		return FeedItemKindPost, nil
	}
	itemKind := FeedItemKind(trimmed)
	if !itemKind.Valid() {
		return "", ErrInvalidFeedItemKind
	}
	return itemKind, nil
}

// GetHomeFeed returns the unified ranked home feed.
func (h *Handler) GetHomeFeed(w http.ResponseWriter, r *http.Request) {
	userID := middleware.CurrentUserID(r)
	params := pagination.ParseCursor(r, 20, 50)

	items, err := h.db.ListHomeFeed(r.Context(), userID, params.Before, params.Limit+1)
	if err != nil {
		log.Printf("list home feed failed for %s: %v", userID, err)
		response.Error(w, http.StatusInternalServerError, "could not fetch home feed")
		return
	}

	response.Success(w, http.StatusOK, pagination.CursorSlice(items, params.Limit, func(item FeedItem) time.Time { return item.CreatedAt }))
}

// GetUserPosts returns a single user's posts with the same shape as the main feed.
// Paginate with ?before=<next_cursor> from the previous response.
func (h *Handler) GetUserPosts(w http.ResponseWriter, r *http.Request) {
	targetID, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		response.Error(w, http.StatusBadRequest, "invalid user id")
		return
	}

	params := pagination.ParseCursor(r, 20, 50)

	posts, err := h.db.ListUserPosts(r.Context(), targetID, params.Before, params.Limit+1)
	if err != nil {
		log.Printf("list user posts failed for %s: %v", targetID, err)
		response.Error(w, http.StatusInternalServerError, "could not fetch posts")
		return
	}

	response.Success(w, http.StatusOK, pagination.CursorSlice(posts, params.Limit, func(p Post) time.Time { return p.CreatedAt }))
}

// CreatePost validates and inserts a new post for the authenticated user.
func (h *Handler) CreatePost(w http.ResponseWriter, r *http.Request) {
	userID := middleware.CurrentUserID(r)

	var input struct {
		Body   string      `json:"body"`
		Images []PostImage `json:"images"`
	}
	if err := json.NewDecoder(r.Body).Decode(&input); err != nil {
		response.Error(w, http.StatusBadRequest, "body is required")
		return
	}

	input.Body = strings.TrimSpace(input.Body)
	if input.Body == "" && len(input.Images) == 0 {
		response.Error(w, http.StatusBadRequest, "body or image is required")
		return
	}
	if len(input.Images) > 1 {
		response.Error(w, http.StatusBadRequest, "only one image is supported")
		return
	}
	for index := range input.Images {
		input.Images[index].ImageURL = strings.TrimSpace(input.Images[index].ImageURL)
		input.Images[index].SortOrder = index
		if input.Images[index].ImageURL == "" {
			response.Error(w, http.StatusBadRequest, "image_url is required")
			return
		}
		if input.Images[index].Width <= 0 || input.Images[index].Height <= 0 {
			response.Error(w, http.StatusBadRequest, "image dimensions are required")
			return
		}
	}

	postID, err := h.db.CreatePost(r.Context(), userID, input.Body, input.Images)
	if err != nil {
		response.Error(w, http.StatusInternalServerError, "could not create post")
		return
	}

	response.Success(w, http.StatusCreated, map[string]any{"id": postID})
}

// SharePost creates a new reshare record for an existing post.
func (h *Handler) SharePost(w http.ResponseWriter, r *http.Request) {
	userID := middleware.CurrentUserID(r)
	postID, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		response.Error(w, http.StatusBadRequest, "invalid post id")
		return
	}

	var input struct {
		Commentary string `json:"commentary"`
	}
	if err := json.NewDecoder(r.Body).Decode(&input); err != nil && !errors.Is(err, io.EOF) {
		response.Error(w, http.StatusBadRequest, "invalid request body")
		return
	}

	shareID, err := h.db.SharePost(r.Context(), userID, postID, input.Commentary)
	if err != nil {
		if errors.Is(err, ErrFeedFeatureDisabled) {
			response.Error(w, http.StatusServiceUnavailable, "post sharing is temporarily unavailable")
			return
		}
		if errors.Is(err, ErrNotFound) {
			response.Error(w, http.StatusNotFound, "post not found")
			return
		}
		response.Error(w, http.StatusInternalServerError, "could not share post")
		return
	}

	response.Success(w, http.StatusCreated, map[string]any{"id": shareID})
}

// UploadPostImage validates and uploads the original image without modifying it.
func (h *Handler) UploadPostImage(w http.ResponseWriter, r *http.Request) {
	if h.uploader == nil {
		response.Error(w, http.StatusInternalServerError, "image uploads are not configured")
		return
	}

	userID := middleware.CurrentUserID(r)
	if err := r.ParseMultipartForm(24 << 20); err != nil {
		response.Error(w, http.StatusBadRequest, "file too large or invalid form data")
		return
	}

	imageFile, err := readUploadedImage(r, "image")
	if err != nil {
		response.Error(w, http.StatusBadRequest, err.Error())
		return
	}

	// DecodeConfig reads image dimensions without re-encoding the upload.
	imageConfig, _, err := image.DecodeConfig(bytes.NewReader(imageFile.body))
	if err != nil {
		response.Error(w, http.StatusBadRequest, "could not decode image")
		return
	}
	if imageConfig.Width <= 0 || imageConfig.Height <= 0 {
		response.Error(w, http.StatusBadRequest, "image dimensions are required")
		return
	}

	key := fmt.Sprintf("posts/%s/%s%s", userID, uuid.New(), imageFile.extension)
	imageURL, err := h.uploader.Upload(r.Context(), key, imageFile.contentType, bytes.NewReader(imageFile.body))
	if err != nil {
		response.Error(w, http.StatusInternalServerError, "could not upload image")
		return
	}

	response.Success(w, http.StatusOK, PostImage{
		ID:       uuid.New(),
		ImageURL: imageURL,
		Width:    imageConfig.Width,
		Height:   imageConfig.Height,
	})
}

type uploadedImage struct {
	body        []byte
	contentType string
	extension   string
}

func readUploadedImage(r *http.Request, fieldName string) (*uploadedImage, error) {
	file, header, err := r.FormFile(fieldName)
	if err != nil {
		if errors.Is(err, http.ErrMissingFile) {
			return nil, fmt.Errorf("%s field is required", fieldName)
		}
		return nil, fmt.Errorf("%s field is invalid", fieldName)
	}
	defer file.Close()

	fileBytes, err := io.ReadAll(file)
	if err != nil {
		return nil, errors.New("could not read image")
	}

	contentType := header.Header.Get("Content-Type")
	if contentType == "" || contentType == "application/octet-stream" {
		contentType = http.DetectContentType(fileBytes)
	}
	extension, ok := imageExtension(contentType, header.Filename)
	if !ok {
		return nil, errors.New("image must be a JPEG or PNG image")
	}

	return &uploadedImage{
		body:        fileBytes,
		contentType: contentType,
		extension:   extension,
	}, nil
}

func imageExtension(contentType, filename string) (string, bool) {
	switch contentType {
	case "image/jpeg":
		return ".jpg", true
	case "image/png":
		return ".png", true
	default:
		extension := strings.ToLower(filepath.Ext(filename))
		switch extension {
		case ".jpg", ".jpeg":
			return ".jpg", true
		case ".png":
			return ".png", true
		default:
			return "", false
		}
	}
}

// DeletePost removes a post only when it belongs to the authenticated user.
func (h *Handler) DeletePost(w http.ResponseWriter, r *http.Request) {
	userID := middleware.CurrentUserID(r)
	postID, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		response.Error(w, http.StatusBadRequest, "invalid post id")
		return
	}

	if err := h.db.DeletePost(r.Context(), postID, userID); err != nil {
		if errors.Is(err, ErrNotFound) {
			response.Error(w, http.StatusNotFound, "post not found or not yours")
			return
		}
		response.Error(w, http.StatusInternalServerError, "could not delete post")
		return
	}

	response.Success(w, http.StatusOK, map[string]bool{"deleted": true})
}

// HideFeedItem suppresses a single feed item for the authenticated user.
func (h *Handler) HideFeedItem(w http.ResponseWriter, r *http.Request) {
	userID := middleware.CurrentUserID(r)
	itemID, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		response.Error(w, http.StatusBadRequest, "invalid feed item id")
		return
	}

	var input struct {
		ItemKind FeedItemKind `json:"item_kind"`
	}
	if err := json.NewDecoder(r.Body).Decode(&input); err != nil {
		response.Error(w, http.StatusBadRequest, "invalid request body")
		return
	}
	if input.ItemKind == "" {
		input.ItemKind = FeedItemKindPost
	}

	if err := h.db.HideFeedItem(r.Context(), userID, itemID, input.ItemKind); err != nil {
		if isFeedValidationError(err) {
			response.Error(w, http.StatusBadRequest, err.Error())
			return
		}
		response.Error(w, http.StatusInternalServerError, "could not hide feed item")
		return
	}

	response.Success(w, http.StatusOK, map[string]bool{"hidden": true})
}

// UnhideFeedItem restores a previously hidden feed item for the authenticated user.
func (h *Handler) UnhideFeedItem(w http.ResponseWriter, r *http.Request) {
	userID := middleware.CurrentUserID(r)
	itemID, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		response.Error(w, http.StatusBadRequest, "invalid feed item id")
		return
	}

	itemKind, err := parseFeedItemKind(r.URL.Query().Get("item_kind"))
	if err != nil {
		response.Error(w, http.StatusBadRequest, "invalid item kind")
		return
	}

	if err := h.db.UnhideFeedItem(r.Context(), userID, itemID, itemKind); err != nil {
		if isFeedValidationError(err) {
			response.Error(w, http.StatusBadRequest, err.Error())
			return
		}
		response.Error(w, http.StatusInternalServerError, "could not restore feed item")
		return
	}

	response.Success(w, http.StatusOK, map[string]bool{"hidden": false})
}

// GetHiddenFeedItems returns the caller's hidden feed items in reverse hidden order.
func (h *Handler) GetHiddenFeedItems(w http.ResponseWriter, r *http.Request) {
	userID := middleware.CurrentUserID(r)
	params := pagination.ParseCursor(r, 20, 50)

	items, err := h.db.ListHiddenFeedItems(r.Context(), userID, params.Before, params.Limit+1)
	if err != nil {
		response.Error(w, http.StatusInternalServerError, "could not fetch hidden feed items")
		return
	}

	response.Success(w, http.StatusOK, pagination.CursorSlice(items, params.Limit, func(item HiddenFeedItem) time.Time { return item.HiddenAt }))
}

// MuteFeedAuthor suppresses future content from a specific author for the caller.
func (h *Handler) MuteFeedAuthor(w http.ResponseWriter, r *http.Request) {
	userID := middleware.CurrentUserID(r)
	authorID, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		response.Error(w, http.StatusBadRequest, "invalid author id")
		return
	}

	if err := h.db.MuteFeedAuthor(r.Context(), userID, authorID); err != nil {
		response.Error(w, http.StatusInternalServerError, "could not mute author")
		return
	}

	response.Success(w, http.StatusOK, map[string]bool{"muted": true})
}

// LogFeedImpressions records items that were actually visible in the feed UI.
func (h *Handler) LogFeedImpressions(w http.ResponseWriter, r *http.Request) {
	userID := middleware.CurrentUserID(r)

	var input struct {
		Impressions []FeedImpressionInput `json:"impressions"`
	}
	if err := json.NewDecoder(r.Body).Decode(&input); err != nil {
		response.Error(w, http.StatusBadRequest, "invalid request body")
		return
	}

	if err := h.db.LogFeedImpressions(r.Context(), userID, input.Impressions); err != nil {
		if isFeedValidationError(err) {
			response.Error(w, http.StatusBadRequest, err.Error())
			return
		}
		response.Error(w, http.StatusInternalServerError, "could not log feed impressions")
		return
	}

	response.Success(w, http.StatusOK, map[string]any{"logged": len(input.Impressions)})
}

// LogFeedEvents records feed interaction events such as likes, shares, hides, and comment opens.
func (h *Handler) LogFeedEvents(w http.ResponseWriter, r *http.Request) {
	userID := middleware.CurrentUserID(r)

	var input struct {
		Events []FeedEventInput `json:"events"`
	}
	if err := json.NewDecoder(r.Body).Decode(&input); err != nil {
		response.Error(w, http.StatusBadRequest, "invalid request body")
		return
	}

	if err := h.db.LogFeedEvents(r.Context(), userID, input.Events); err != nil {
		if isFeedValidationError(err) {
			response.Error(w, http.StatusBadRequest, err.Error())
			return
		}
		response.Error(w, http.StatusInternalServerError, "could not log feed events")
		return
	}

	response.Success(w, http.StatusOK, map[string]any{"logged": len(input.Events)})
}

// ReactToFeedItem toggles a reaction on either a post item or a reshare item.
func (h *Handler) ReactToFeedItem(w http.ResponseWriter, r *http.Request) {
	userID := middleware.CurrentUserID(r)
	itemID, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		response.Error(w, http.StatusBadRequest, "invalid item id")
		return
	}

	var input struct {
		Type     string `json:"type"`
		ItemKind string `json:"item_kind"`
	}
	if err := json.NewDecoder(r.Body).Decode(&input); err != nil && !errors.Is(err, io.EOF) {
		response.Error(w, http.StatusBadRequest, "invalid request body")
		return
	}
	itemKind, err := parseFeedItemKind(input.ItemKind)
	if err != nil {
		response.Error(w, http.StatusBadRequest, "invalid item kind")
		return
	}
	if strings.TrimSpace(input.Type) == "" {
		input.Type = "like"
	}

	reacted, err := h.db.ToggleFeedItemReaction(r.Context(), itemID, userID, itemKind, input.Type)
	if err != nil {
		if errors.Is(err, ErrInvalidFeedItemKind) {
			response.Error(w, http.StatusBadRequest, "invalid item kind")
			return
		}
		response.Error(w, http.StatusInternalServerError, "could not update reaction")
		return
	}

	response.Success(w, http.StatusOK, map[string]bool{"reacted": reacted})
}

// AddFeedItemComment validates and inserts a new comment on a post or reshare thread.
func (h *Handler) AddFeedItemComment(w http.ResponseWriter, r *http.Request) {
	userID := middleware.CurrentUserID(r)
	itemID, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		response.Error(w, http.StatusBadRequest, "invalid item id")
		return
	}

	var input struct {
		Body           string      `json:"body"`
		ItemKind       string      `json:"item_kind"`
		MentionUserIDs []uuid.UUID `json:"mention_user_ids"`
	}
	if err := json.NewDecoder(r.Body).Decode(&input); err != nil {
		response.Error(w, http.StatusBadRequest, "body is required")
		return
	}
	input.Body = strings.TrimSpace(input.Body)
	if input.Body == "" {
		response.Error(w, http.StatusBadRequest, "body is required")
		return
	}

	itemKind, err := parseFeedItemKind(input.ItemKind)
	if err != nil {
		response.Error(w, http.StatusBadRequest, "invalid item kind")
		return
	}

	resolvedMentions, err := h.resolveCommentMentions(r.Context(), input.Body, input.MentionUserIDs, userID)
	if err != nil {
		response.Error(w, http.StatusInternalServerError, "could not validate mentions")
		return
	}

	comment, err := h.db.AddFeedItemComment(r.Context(), itemID, userID, itemKind, input.Body, resolvedMentions)
	if err != nil {
		if errors.Is(err, ErrInvalidFeedItemKind) {
			response.Error(w, http.StatusBadRequest, "invalid item kind")
			return
		}
		response.Error(w, http.StatusInternalServerError, "could not add comment")
		return
	}

	if h.notifier != nil && len(resolvedMentions) > 0 && itemKind == FeedItemKindPost {
		mentionedUserIDs := make([]uuid.UUID, 0, len(resolvedMentions))
		for _, mention := range resolvedMentions {
			mentionedUserIDs = append(mentionedUserIDs, mention.UserID)
		}
		_ = h.notifier.NotifyCommentMentions(r.Context(), itemID, comment.ID, userID, mentionedUserIDs, input.Body)
	}

	response.Success(w, http.StatusCreated, comment)
}

// GetFeedItemComments returns a page of comments for a post or reshare thread.
func (h *Handler) GetFeedItemComments(w http.ResponseWriter, r *http.Request) {
	itemID, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		response.Error(w, http.StatusBadRequest, "invalid item id")
		return
	}
	itemKind, err := parseFeedItemKind(r.URL.Query().Get("item_kind"))
	if err != nil {
		response.Error(w, http.StatusBadRequest, "invalid item kind")
		return
	}

	params := pagination.ParseCursor(r, 20, 50)
	comments, err := h.db.ListFeedItemComments(r.Context(), itemID, itemKind, params.After, params.Limit+1)
	if err != nil {
		if errors.Is(err, ErrInvalidFeedItemKind) {
			response.Error(w, http.StatusBadRequest, "invalid item kind")
			return
		}
		response.Error(w, http.StatusInternalServerError, "could not fetch comments")
		return
	}

	response.Success(w, http.StatusOK, pagination.CursorSlice(comments, params.Limit, func(c Comment) time.Time { return c.CreatedAt }))
}

// GetReactions returns a paginated list of reactions for a post with reacting user details.
func (h *Handler) GetReactions(w http.ResponseWriter, r *http.Request) {
	postID, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		response.Error(w, http.StatusBadRequest, "invalid post id")
		return
	}

	params := pagination.Parse(r, 50, 100)

	reactions, err := h.db.ListReactions(r.Context(), postID, params.Limit+1, params.Offset)
	if err != nil {
		response.Error(w, http.StatusInternalServerError, "could not fetch reactions")
		return
	}

	response.Success(w, http.StatusOK, pagination.Slice(reactions, params))
}

// ReactToPost toggles a specific reaction type for the authenticated user on a post.
func (h *Handler) ReactToPost(w http.ResponseWriter, r *http.Request) {
	userID := middleware.CurrentUserID(r)
	postID, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		response.Error(w, http.StatusBadRequest, "invalid post id")
		return
	}

	var input struct {
		Type string `json:"type"`
	}
	if err := json.NewDecoder(r.Body).Decode(&input); err != nil {
		response.Error(w, http.StatusBadRequest, "invalid request body")
		return
	}
	if input.Type == "" {
		input.Type = "like"
	}

	// Re-sending the same reaction toggles it off. Different reaction types are
	// stored as separate rows, so the check stays scoped to post/user/type.
	reacted, err := h.db.ToggleReaction(r.Context(), postID, userID, input.Type)
	if err != nil {
		response.Error(w, http.StatusInternalServerError, "could not update reaction")
		return
	}

	response.Success(w, http.StatusOK, map[string]bool{"reacted": reacted})
}

// AddComment validates and inserts a new comment on the requested post.
func (h *Handler) AddComment(w http.ResponseWriter, r *http.Request) {
	userID := middleware.CurrentUserID(r)
	postID, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		response.Error(w, http.StatusBadRequest, "invalid post id")
		return
	}

	var input struct {
		Body           string      `json:"body"`
		MentionUserIDs []uuid.UUID `json:"mention_user_ids"`
	}
	if err := json.NewDecoder(r.Body).Decode(&input); err != nil {
		response.Error(w, http.StatusBadRequest, "body is required")
		return
	}
	input.Body = strings.TrimSpace(input.Body)
	if input.Body == "" {
		response.Error(w, http.StatusBadRequest, "body is required")
		return
	}

	resolvedMentions, err := h.resolveCommentMentions(r.Context(), input.Body, input.MentionUserIDs, userID)
	if err != nil {
		response.Error(w, http.StatusInternalServerError, "could not validate mentions")
		return
	}

	comment, err := h.db.AddComment(r.Context(), postID, userID, input.Body, resolvedMentions)
	if err != nil {
		response.Error(w, http.StatusInternalServerError, "could not add comment")
		return
	}

	if h.notifier != nil && len(resolvedMentions) > 0 {
		mentionedUserIDs := make([]uuid.UUID, 0, len(resolvedMentions))
		for _, mention := range resolvedMentions {
			mentionedUserIDs = append(mentionedUserIDs, mention.UserID)
		}
		// Comment creation is the primary action; best-effort notification enqueue
		// avoids retrying the comment after the write already succeeded.
		_ = h.notifier.NotifyCommentMentions(r.Context(), postID, comment.ID, userID, mentionedUserIDs, input.Body)
	}

	response.Success(w, http.StatusCreated, comment)
}

// GetComments returns a page of a post's comments in conversation order.
// Paginate with ?after=<next_cursor> from the previous response.
func (h *Handler) GetComments(w http.ResponseWriter, r *http.Request) {
	postID, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		response.Error(w, http.StatusBadRequest, "invalid post id")
		return
	}

	params := pagination.ParseCursor(r, 20, 50)

	comments, err := h.db.ListComments(r.Context(), postID, params.After, params.Limit+1)
	if err != nil {
		response.Error(w, http.StatusInternalServerError, "could not fetch comments")
		return
	}

	response.Success(w, http.StatusOK, pagination.CursorSlice(comments, params.Limit, func(c Comment) time.Time { return c.CreatedAt }))
}

func (h *Handler) resolveCommentMentions(ctx context.Context, body string, mentionUserIDs []uuid.UUID, authorID uuid.UUID) ([]CommentMention, error) {
	if len(mentionUserIDs) == 0 {
		return nil, nil
	}

	uniqueIDs := make([]uuid.UUID, 0, len(mentionUserIDs))
	seenIDs := make(map[uuid.UUID]struct{}, len(mentionUserIDs))
	for _, id := range mentionUserIDs {
		if id == uuid.Nil || id == authorID {
			continue
		}
		if _, seen := seenIDs[id]; seen {
			continue
		}
		seenIDs[id] = struct{}{}
		uniqueIDs = append(uniqueIDs, id)
	}
	if len(uniqueIDs) == 0 {
		return nil, nil
	}

	users, err := h.db.ResolveMentionUsers(ctx, uniqueIDs)
	if err != nil {
		return nil, err
	}
	if len(users) == 0 {
		return nil, nil
	}

	userByUsername := make(map[string]MentionedUser, len(users))
	for _, user := range users {
		userByUsername[strings.ToLower(user.Username)] = user
	}

	resolved := make([]CommentMention, 0, len(users))
	added := make(map[uuid.UUID]struct{}, len(users))
	for _, username := range extractMentionHandles(body) {
		user, ok := userByUsername[username]
		if !ok {
			continue
		}
		if _, seen := added[user.UserID]; seen {
			continue
		}
		added[user.UserID] = struct{}{}
		resolved = append(resolved, CommentMention{UserID: user.UserID, Username: user.Username})
	}

	sort.Slice(resolved, func(i, j int) bool {
		return resolved[i].Username < resolved[j].Username
	})
	return resolved, nil
}

func extractMentionHandles(body string) []string {
	mentions := []string{}
	seen := map[string]struct{}{}

	for i := 0; i < len(body); i++ {
		if body[i] != '@' {
			continue
		}
		if i > 0 && isMentionChar(body[i-1]) {
			continue
		}

		j := i + 1
		for j < len(body) && isMentionChar(body[j]) {
			j++
		}
		if j == i+1 {
			continue
		}

		handle := strings.ToLower(body[i+1 : j])
		if _, exists := seen[handle]; exists {
			continue
		}
		seen[handle] = struct{}{}
		mentions = append(mentions, handle)
		i = j - 1
	}

	return mentions
}

func isMentionChar(ch byte) bool {
	return (ch >= 'a' && ch <= 'z') ||
		(ch >= 'A' && ch <= 'Z') ||
		(ch >= '0' && ch <= '9') ||
		ch == '.' || ch == '_'
}
