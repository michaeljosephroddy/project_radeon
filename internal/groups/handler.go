package groups

import (
	"context"
	"encoding/json"
	"errors"
	"log"
	"net/http"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/project_radeon/api/pkg/middleware"
	"github.com/project_radeon/api/pkg/pagination"
	"github.com/project_radeon/api/pkg/response"
)

type Handler struct {
	store    Store
	notifier Notifier
}

func NewHandler(store Store) *Handler {
	return &Handler{store: store}
}

func NewHandlerWithNotifier(store Store, notifier Notifier) *Handler {
	return &Handler{store: store, notifier: notifier}
}

type Notifier interface {
	NotifyGroupJoinRequest(ctx context.Context, groupID, requesterID uuid.UUID) error
	NotifyGroupJoinApproved(ctx context.Context, groupID, reviewerID, approvedUserID uuid.UUID) error
	NotifyGroupPost(ctx context.Context, groupID, postID, authorID uuid.UUID, postType, body string) error
	NotifyGroupComment(ctx context.Context, groupID, postID, commentID, authorID uuid.UUID, body string) error
	NotifyGroupAdminContact(ctx context.Context, groupID, threadID, senderID uuid.UUID, body string) error
	NotifyGroupAdminReply(ctx context.Context, groupID, threadID, messageID, senderID uuid.UUID, body string) error
	NotifyGroupReport(ctx context.Context, groupID, reportID, reporterID uuid.UUID, targetType, reason string) error
}

type groupRequest struct {
	Name                string   `json:"name"`
	Description         *string  `json:"description"`
	Rules               *string  `json:"rules"`
	AvatarURL           *string  `json:"avatar_url"`
	CoverURL            *string  `json:"cover_url"`
	Visibility          string   `json:"visibility"`
	PostingPermission   string   `json:"posting_permission"`
	AllowAnonymousPosts bool     `json:"allow_anonymous_posts"`
	City                *string  `json:"city"`
	Country             *string  `json:"country"`
	Tags                []string `json:"tags"`
	RecoveryPathways    []string `json:"recovery_pathways"`
}

type groupPostRequest struct {
	PostType  string                  `json:"post_type"`
	Body      string                  `json:"body"`
	Anonymous bool                    `json:"anonymous"`
	Images    []groupPostImageRequest `json:"images"`
}

type groupPostImageRequest struct {
	ImageURL string  `json:"image_url"`
	ThumbURL *string `json:"thumb_url"`
	Width    int     `json:"width"`
	Height   int     `json:"height"`
}

type inviteRequest struct {
	ExpiresAt        *time.Time `json:"expires_at"`
	MaxUses          *int       `json:"max_uses"`
	RequiresApproval bool       `json:"requires_approval"`
}

type adminContactRequest struct {
	Subject string `json:"subject"`
	Body    string `json:"body"`
}

type reportRequest struct {
	TargetType string  `json:"target_type"`
	TargetID   *string `json:"target_id"`
	Reason     string  `json:"reason"`
	Details    *string `json:"details"`
}

func (h *Handler) ListGroups(w http.ResponseWriter, r *http.Request) {
	userID := middleware.CurrentUserID(r)
	params := pagination.ParseCursor(r, 20, 50)
	groups, err := h.store.ListGroups(r.Context(), userID, ListGroupsParams{
		Query:           strings.TrimSpace(r.URL.Query().Get("q")),
		City:            strings.TrimSpace(r.URL.Query().Get("city")),
		Country:         strings.TrimSpace(r.URL.Query().Get("country")),
		Tag:             strings.TrimSpace(r.URL.Query().Get("tag")),
		RecoveryPathway: strings.TrimSpace(r.URL.Query().Get("recovery_pathway")),
		MemberScope:     strings.TrimSpace(r.URL.Query().Get("member_scope")),
		Before:          params.Before,
		Limit:           params.Limit + 1,
	})
	if err != nil {
		log.Printf("list groups failed for %s: %v", userID, err)
		response.Error(w, http.StatusInternalServerError, "could not fetch groups")
		return
	}
	response.Success(w, http.StatusOK, pagination.CursorSlice(groups, params.Limit, func(group Group) time.Time {
		return group.CreatedAt
	}))
}

func (h *Handler) CreateGroup(w http.ResponseWriter, r *http.Request) {
	userID := middleware.CurrentUserID(r)
	input, ok := decodeCreateGroupInput(w, r)
	if !ok {
		return
	}
	group, err := h.store.CreateGroup(r.Context(), userID, input)
	if err != nil {
		log.Printf("create group failed for %s: %v", userID, err)
		response.Error(w, http.StatusInternalServerError, "could not create group")
		return
	}
	response.Success(w, http.StatusCreated, group)
}

func (h *Handler) GetGroup(w http.ResponseWriter, r *http.Request) {
	userID := middleware.CurrentUserID(r)
	groupID, ok := parseGroupID(w, r)
	if !ok {
		return
	}
	group, err := h.store.GetGroup(r.Context(), userID, groupID)
	if errors.Is(err, ErrNotFound) {
		response.Error(w, http.StatusNotFound, "group not found")
		return
	}
	if err != nil {
		log.Printf("get group failed for %s: %v", groupID, err)
		response.Error(w, http.StatusInternalServerError, "could not fetch group")
		return
	}
	response.Success(w, http.StatusOK, group)
}

func (h *Handler) JoinGroup(w http.ResponseWriter, r *http.Request) {
	userID := middleware.CurrentUserID(r)
	groupID, ok := parseGroupID(w, r)
	if !ok {
		return
	}
	var req struct {
		Message string `json:"message"`
	}
	if r.Body != nil {
		_ = json.NewDecoder(r.Body).Decode(&req)
	}
	result, err := h.store.JoinGroup(r.Context(), userID, groupID, req.Message)
	if errors.Is(err, ErrNotFound) {
		response.Error(w, http.StatusNotFound, "group not found")
		return
	}
	if errors.Is(err, ErrForbidden) {
		response.Error(w, http.StatusForbidden, "you cannot join this group")
		return
	}
	if errors.Is(err, ErrInviteRequired) {
		response.Error(w, http.StatusForbidden, "invite required")
		return
	}
	if err != nil {
		log.Printf("join group failed for %s by %s: %v", groupID, userID, err)
		response.Error(w, http.StatusInternalServerError, "could not join group")
		return
	}
	if result.State == "pending" && h.notifier != nil {
		_ = h.notifier.NotifyGroupJoinRequest(r.Context(), groupID, userID)
	}
	response.Success(w, http.StatusOK, result)
}

func (h *Handler) LeaveGroup(w http.ResponseWriter, r *http.Request) {
	userID := middleware.CurrentUserID(r)
	groupID, ok := parseGroupID(w, r)
	if !ok {
		return
	}
	err := h.store.LeaveGroup(r.Context(), userID, groupID)
	if errors.Is(err, ErrNotFound) {
		response.Error(w, http.StatusNotFound, "membership not found")
		return
	}
	if errors.Is(err, ErrOwnerCannotLeave) {
		response.Error(w, http.StatusConflict, "transfer ownership before leaving")
		return
	}
	if err != nil {
		log.Printf("leave group failed for %s by %s: %v", groupID, userID, err)
		response.Error(w, http.StatusInternalServerError, "could not leave group")
		return
	}
	response.Success(w, http.StatusOK, map[string]bool{"left": true})
}

func (h *Handler) ListMembers(w http.ResponseWriter, r *http.Request) {
	userID := middleware.CurrentUserID(r)
	groupID, ok := parseGroupID(w, r)
	if !ok {
		return
	}
	params := pagination.ParseCursor(r, 20, 50)
	members, err := h.store.ListMembers(r.Context(), userID, groupID, params.Before, params.Limit+1)
	if errors.Is(err, ErrNotFound) {
		response.Error(w, http.StatusNotFound, "group not found")
		return
	}
	if err != nil {
		log.Printf("list group members failed for %s: %v", groupID, err)
		response.Error(w, http.StatusInternalServerError, "could not fetch members")
		return
	}
	response.Success(w, http.StatusOK, pagination.CursorSlice(members, params.Limit, func(member GroupMember) time.Time {
		if member.JoinedAt == nil {
			return member.CreatedAt
		}
		return *member.JoinedAt
	}))
}

func (h *Handler) ListPosts(w http.ResponseWriter, r *http.Request) {
	userID := middleware.CurrentUserID(r)
	groupID, ok := parseGroupID(w, r)
	if !ok {
		return
	}
	params := pagination.ParseCursor(r, 20, 50)
	posts, err := h.store.ListPosts(r.Context(), userID, groupID, params.Before, params.Limit+1)
	if errors.Is(err, ErrNotFound) || errors.Is(err, ErrForbidden) {
		response.Error(w, http.StatusNotFound, "group not found")
		return
	}
	if err != nil {
		log.Printf("list group posts failed for %s: %v", groupID, err)
		response.Error(w, http.StatusInternalServerError, "could not fetch group posts")
		return
	}
	response.Success(w, http.StatusOK, pagination.CursorSlice(posts, params.Limit, func(post GroupPost) time.Time {
		return post.CreatedAt
	}))
}

func (h *Handler) CreatePost(w http.ResponseWriter, r *http.Request) {
	userID := middleware.CurrentUserID(r)
	groupID, ok := parseGroupID(w, r)
	if !ok {
		return
	}
	input, ok := decodeCreatePostInput(w, r)
	if !ok {
		return
	}
	post, err := h.store.CreatePost(r.Context(), userID, groupID, input)
	if errors.Is(err, ErrNotFound) {
		response.Error(w, http.StatusNotFound, "group not found")
		return
	}
	if errors.Is(err, ErrForbidden) {
		response.Error(w, http.StatusForbidden, "you cannot post in this group")
		return
	}
	if err != nil {
		log.Printf("create group post failed for %s: %v", groupID, err)
		response.Error(w, http.StatusInternalServerError, "could not create group post")
		return
	}
	if h.notifier != nil {
		_ = h.notifier.NotifyGroupPost(r.Context(), groupID, post.ID, userID, string(post.PostType), post.Body)
	}
	response.Success(w, http.StatusCreated, post)
}

func (h *Handler) ListMedia(w http.ResponseWriter, r *http.Request) {
	userID := middleware.CurrentUserID(r)
	groupID, ok := parseGroupID(w, r)
	if !ok {
		return
	}
	params := pagination.ParseCursor(r, 30, 80)
	items, err := h.store.ListMedia(r.Context(), userID, groupID, params.Before, params.Limit+1)
	if errors.Is(err, ErrNotFound) || errors.Is(err, ErrForbidden) {
		response.Error(w, http.StatusNotFound, "group not found")
		return
	}
	if err != nil {
		log.Printf("list group media failed for %s: %v", groupID, err)
		response.Error(w, http.StatusInternalServerError, "could not fetch group media")
		return
	}
	response.Success(w, http.StatusOK, pagination.CursorSlice(items, params.Limit, func(item GroupMediaItem) time.Time {
		return item.CreatedAt
	}))
}

func (h *Handler) ListComments(w http.ResponseWriter, r *http.Request) {
	userID := middleware.CurrentUserID(r)
	groupID, postID, ok := parseGroupAndPostID(w, r)
	if !ok {
		return
	}
	params := pagination.ParseCursor(r, 20, 50)
	comments, err := h.store.ListComments(r.Context(), userID, groupID, postID, params.After, params.Limit+1)
	if errors.Is(err, ErrNotFound) || errors.Is(err, ErrForbidden) {
		response.Error(w, http.StatusNotFound, "post not found")
		return
	}
	if err != nil {
		log.Printf("list group comments failed for %s: %v", postID, err)
		response.Error(w, http.StatusInternalServerError, "could not fetch comments")
		return
	}
	response.Success(w, http.StatusOK, pagination.CursorSlice(comments, params.Limit, func(comment GroupComment) time.Time {
		return comment.CreatedAt
	}))
}

func (h *Handler) CreateComment(w http.ResponseWriter, r *http.Request) {
	userID := middleware.CurrentUserID(r)
	groupID, postID, ok := parseGroupAndPostID(w, r)
	if !ok {
		return
	}
	var req struct {
		Body string `json:"body"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		response.Error(w, http.StatusBadRequest, "invalid request body")
		return
	}
	body := strings.TrimSpace(req.Body)
	if body == "" || len(body) > 2000 {
		response.ValidationError(w, map[string]string{"body": "comment must be 1 to 2000 characters"})
		return
	}
	comment, err := h.store.CreateComment(r.Context(), userID, groupID, postID, body)
	if errors.Is(err, ErrNotFound) {
		response.Error(w, http.StatusNotFound, "post not found")
		return
	}
	if errors.Is(err, ErrForbidden) {
		response.Error(w, http.StatusForbidden, "you cannot comment in this group")
		return
	}
	if err != nil {
		log.Printf("create group comment failed for %s: %v", postID, err)
		response.Error(w, http.StatusInternalServerError, "could not create comment")
		return
	}
	if h.notifier != nil {
		_ = h.notifier.NotifyGroupComment(r.Context(), groupID, postID, comment.ID, userID, comment.Body)
	}
	response.Success(w, http.StatusCreated, comment)
}

func (h *Handler) ToggleReaction(w http.ResponseWriter, r *http.Request) {
	userID := middleware.CurrentUserID(r)
	groupID, postID, ok := parseGroupAndPostID(w, r)
	if !ok {
		return
	}
	post, err := h.store.ToggleReaction(r.Context(), userID, groupID, postID)
	if errors.Is(err, ErrNotFound) {
		response.Error(w, http.StatusNotFound, "post not found")
		return
	}
	if errors.Is(err, ErrForbidden) {
		response.Error(w, http.StatusForbidden, "you cannot react in this group")
		return
	}
	if err != nil {
		log.Printf("toggle group reaction failed for %s: %v", postID, err)
		response.Error(w, http.StatusInternalServerError, "could not update reaction")
		return
	}
	response.Success(w, http.StatusOK, post)
}

func (h *Handler) PinPost(w http.ResponseWriter, r *http.Request) {
	h.setPostPinned(w, r, true)
}

func (h *Handler) UnpinPost(w http.ResponseWriter, r *http.Request) {
	h.setPostPinned(w, r, false)
}

func (h *Handler) setPostPinned(w http.ResponseWriter, r *http.Request, pinned bool) {
	userID := middleware.CurrentUserID(r)
	groupID, postID, ok := parseGroupAndPostID(w, r)
	if !ok {
		return
	}
	post, err := h.store.PinPost(r.Context(), userID, groupID, postID, pinned)
	if errors.Is(err, ErrNotFound) {
		response.Error(w, http.StatusNotFound, "post not found")
		return
	}
	if errors.Is(err, ErrForbidden) {
		response.Error(w, http.StatusForbidden, "you cannot moderate this group")
		return
	}
	if err != nil {
		log.Printf("pin group post failed for %s: %v", postID, err)
		response.Error(w, http.StatusInternalServerError, "could not update post")
		return
	}
	response.Success(w, http.StatusOK, post)
}

func (h *Handler) DeletePost(w http.ResponseWriter, r *http.Request) {
	userID := middleware.CurrentUserID(r)
	groupID, postID, ok := parseGroupAndPostID(w, r)
	if !ok {
		return
	}
	err := h.store.DeletePost(r.Context(), userID, groupID, postID)
	if errors.Is(err, ErrNotFound) {
		response.Error(w, http.StatusNotFound, "post not found")
		return
	}
	if errors.Is(err, ErrForbidden) {
		response.Error(w, http.StatusForbidden, "you cannot remove this post")
		return
	}
	if err != nil {
		log.Printf("delete group post failed for %s: %v", postID, err)
		response.Error(w, http.StatusInternalServerError, "could not remove post")
		return
	}
	response.Success(w, http.StatusOK, map[string]bool{"deleted": true})
}

func (h *Handler) CreateInvite(w http.ResponseWriter, r *http.Request) {
	userID := middleware.CurrentUserID(r)
	groupID, ok := parseGroupID(w, r)
	if !ok {
		return
	}
	var req inviteRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil && err.Error() != "EOF" {
		response.Error(w, http.StatusBadRequest, "invalid request body")
		return
	}
	if req.MaxUses != nil && *req.MaxUses < 1 {
		response.ValidationError(w, map[string]string{"max_uses": "max uses must be greater than zero"})
		return
	}
	invite, err := h.store.CreateInvite(r.Context(), userID, groupID, CreateGroupInviteInput{
		ExpiresAt:        req.ExpiresAt,
		MaxUses:          req.MaxUses,
		RequiresApproval: req.RequiresApproval,
	})
	if errors.Is(err, ErrNotFound) {
		response.Error(w, http.StatusNotFound, "group not found")
		return
	}
	if errors.Is(err, ErrForbidden) {
		response.Error(w, http.StatusForbidden, "you cannot invite to this group")
		return
	}
	if err != nil {
		log.Printf("create group invite failed for %s: %v", groupID, err)
		response.Error(w, http.StatusInternalServerError, "could not create invite")
		return
	}
	response.Success(w, http.StatusCreated, invite)
}

func (h *Handler) AcceptInvite(w http.ResponseWriter, r *http.Request) {
	userID := middleware.CurrentUserID(r)
	token := strings.TrimSpace(chi.URLParam(r, "token"))
	if token == "" {
		response.Error(w, http.StatusBadRequest, "invalid invite token")
		return
	}
	result, err := h.store.AcceptInvite(r.Context(), userID, token)
	if errors.Is(err, ErrNotFound) {
		response.Error(w, http.StatusNotFound, "invite not found")
		return
	}
	if errors.Is(err, ErrForbidden) {
		response.Error(w, http.StatusForbidden, "you cannot accept this invite")
		return
	}
	if err != nil {
		log.Printf("accept group invite failed for %s: %v", userID, err)
		response.Error(w, http.StatusInternalServerError, "could not accept invite")
		return
	}
	response.Success(w, http.StatusOK, result)
}

func (h *Handler) ListJoinRequests(w http.ResponseWriter, r *http.Request) {
	userID := middleware.CurrentUserID(r)
	groupID, ok := parseGroupID(w, r)
	if !ok {
		return
	}
	requests, err := h.store.ListJoinRequests(r.Context(), userID, groupID)
	if errors.Is(err, ErrNotFound) {
		response.Error(w, http.StatusNotFound, "group not found")
		return
	}
	if errors.Is(err, ErrForbidden) {
		response.Error(w, http.StatusForbidden, "you cannot review requests")
		return
	}
	if err != nil {
		log.Printf("list join requests failed for %s: %v", groupID, err)
		response.Error(w, http.StatusInternalServerError, "could not fetch join requests")
		return
	}
	response.Success(w, http.StatusOK, map[string]any{"items": requests})
}

func (h *Handler) ApproveJoinRequest(w http.ResponseWriter, r *http.Request) {
	h.reviewJoinRequest(w, r, true)
}

func (h *Handler) RejectJoinRequest(w http.ResponseWriter, r *http.Request) {
	h.reviewJoinRequest(w, r, false)
}

func (h *Handler) reviewJoinRequest(w http.ResponseWriter, r *http.Request, approve bool) {
	userID := middleware.CurrentUserID(r)
	groupID, requestID, ok := parseGroupAndRequestID(w, r)
	if !ok {
		return
	}
	request, err := h.store.ReviewJoinRequest(r.Context(), userID, groupID, requestID, approve)
	if errors.Is(err, ErrNotFound) {
		response.Error(w, http.StatusNotFound, "join request not found")
		return
	}
	if errors.Is(err, ErrForbidden) {
		response.Error(w, http.StatusForbidden, "you cannot review requests")
		return
	}
	if err != nil {
		log.Printf("review join request failed for %s: %v", requestID, err)
		response.Error(w, http.StatusInternalServerError, "could not review join request")
		return
	}
	if approve && h.notifier != nil {
		_ = h.notifier.NotifyGroupJoinApproved(r.Context(), groupID, userID, request.UserID)
	}
	response.Success(w, http.StatusOK, request)
}

func (h *Handler) ContactAdmins(w http.ResponseWriter, r *http.Request) {
	userID := middleware.CurrentUserID(r)
	groupID, ok := parseGroupID(w, r)
	if !ok {
		return
	}
	var req adminContactRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		response.Error(w, http.StatusBadRequest, "invalid request body")
		return
	}
	body := strings.TrimSpace(req.Body)
	if body == "" || len(body) > 2000 {
		response.ValidationError(w, map[string]string{"body": "message must be 1 to 2000 characters"})
		return
	}
	thread, err := h.store.ContactAdmins(r.Context(), userID, groupID, req.Subject, body)
	if errors.Is(err, ErrNotFound) {
		response.Error(w, http.StatusNotFound, "group not found")
		return
	}
	if err != nil {
		log.Printf("contact group admins failed for %s: %v", groupID, err)
		response.Error(w, http.StatusInternalServerError, "could not contact admins")
		return
	}
	if h.notifier != nil {
		_ = h.notifier.NotifyGroupAdminContact(r.Context(), groupID, thread.ID, userID, body)
	}
	response.Success(w, http.StatusCreated, thread)
}

func (h *Handler) ListAdminThreads(w http.ResponseWriter, r *http.Request) {
	userID := middleware.CurrentUserID(r)
	groupID, ok := parseGroupID(w, r)
	if !ok {
		return
	}
	params := pagination.ParseCursor(r, 20, 50)
	threads, err := h.store.ListAdminThreads(r.Context(), userID, groupID, params.Before, params.Limit+1)
	if errors.Is(err, ErrNotFound) {
		response.Error(w, http.StatusNotFound, "group not found")
		return
	}
	if errors.Is(err, ErrForbidden) {
		response.Error(w, http.StatusForbidden, "you cannot view admin inbox")
		return
	}
	if err != nil {
		log.Printf("list admin threads failed for %s: %v", groupID, err)
		response.Error(w, http.StatusInternalServerError, "could not fetch admin inbox")
		return
	}
	response.Success(w, http.StatusOK, pagination.CursorSlice(threads, params.Limit, func(thread GroupAdminThread) time.Time {
		return thread.UpdatedAt
	}))
}

func (h *Handler) ReplyAdminThread(w http.ResponseWriter, r *http.Request) {
	userID := middleware.CurrentUserID(r)
	groupID, threadID, ok := parseGroupAndThreadID(w, r)
	if !ok {
		return
	}
	var req adminContactRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		response.Error(w, http.StatusBadRequest, "invalid request body")
		return
	}
	body := strings.TrimSpace(req.Body)
	if body == "" || len(body) > 2000 {
		response.ValidationError(w, map[string]string{"body": "message must be 1 to 2000 characters"})
		return
	}
	message, err := h.store.ReplyAdminThread(r.Context(), userID, groupID, threadID, body)
	if errors.Is(err, ErrNotFound) {
		response.Error(w, http.StatusNotFound, "thread not found")
		return
	}
	if errors.Is(err, ErrForbidden) {
		response.Error(w, http.StatusForbidden, "you cannot reply to admin inbox")
		return
	}
	if err != nil {
		log.Printf("reply admin thread failed for %s: %v", threadID, err)
		response.Error(w, http.StatusInternalServerError, "could not reply")
		return
	}
	if h.notifier != nil {
		_ = h.notifier.NotifyGroupAdminReply(r.Context(), groupID, threadID, message.ID, userID, message.Body)
	}
	response.Success(w, http.StatusCreated, message)
}

func (h *Handler) ResolveAdminThread(w http.ResponseWriter, r *http.Request) {
	userID := middleware.CurrentUserID(r)
	groupID, threadID, ok := parseGroupAndThreadID(w, r)
	if !ok {
		return
	}
	thread, err := h.store.ResolveAdminThread(r.Context(), userID, groupID, threadID)
	if errors.Is(err, ErrNotFound) {
		response.Error(w, http.StatusNotFound, "thread not found")
		return
	}
	if errors.Is(err, ErrForbidden) {
		response.Error(w, http.StatusForbidden, "you cannot resolve admin inbox")
		return
	}
	if err != nil {
		log.Printf("resolve admin thread failed for %s: %v", threadID, err)
		response.Error(w, http.StatusInternalServerError, "could not resolve thread")
		return
	}
	response.Success(w, http.StatusOK, thread)
}

func (h *Handler) ReportTarget(w http.ResponseWriter, r *http.Request) {
	userID := middleware.CurrentUserID(r)
	groupID, ok := parseGroupID(w, r)
	if !ok {
		return
	}
	var req reportRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		response.Error(w, http.StatusBadRequest, "invalid request body")
		return
	}
	targetType := strings.TrimSpace(req.TargetType)
	if targetType != "group" && targetType != "member" && targetType != "post" && targetType != "comment" {
		response.ValidationError(w, map[string]string{"target_type": "invalid report target"})
		return
	}
	reason := strings.TrimSpace(req.Reason)
	if reason == "" || len(reason) > 80 {
		response.ValidationError(w, map[string]string{"reason": "reason is required"})
		return
	}
	var targetID *uuid.UUID
	if req.TargetID != nil && strings.TrimSpace(*req.TargetID) != "" {
		parsed, err := uuid.Parse(strings.TrimSpace(*req.TargetID))
		if err != nil {
			response.Error(w, http.StatusBadRequest, "invalid target id")
			return
		}
		targetID = &parsed
	}
	report, err := h.store.ReportTarget(r.Context(), userID, groupID, targetType, targetID, reason, trimOptional(req.Details))
	if errors.Is(err, ErrNotFound) {
		response.Error(w, http.StatusNotFound, "group not found")
		return
	}
	if err != nil {
		log.Printf("report group target failed for %s: %v", groupID, err)
		response.Error(w, http.StatusInternalServerError, "could not create report")
		return
	}
	if h.notifier != nil {
		_ = h.notifier.NotifyGroupReport(r.Context(), groupID, report.ID, userID, report.TargetType, report.Reason)
	}
	response.Success(w, http.StatusCreated, report)
}

func decodeCreateGroupInput(w http.ResponseWriter, r *http.Request) (CreateGroupInput, bool) {
	var req groupRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		response.Error(w, http.StatusBadRequest, "invalid request body")
		return CreateGroupInput{}, false
	}
	name := strings.TrimSpace(req.Name)
	if len(name) < 3 || len(name) > 80 {
		response.ValidationError(w, map[string]string{"name": "name must be 3 to 80 characters"})
		return CreateGroupInput{}, false
	}
	visibility := GroupVisibility(strings.TrimSpace(req.Visibility))
	if visibility == "" {
		visibility = GroupVisibilityPublic
	}
	if !visibility.Valid() {
		response.ValidationError(w, map[string]string{"visibility": "invalid group visibility"})
		return CreateGroupInput{}, false
	}
	postingPermission := PostingPermission(strings.TrimSpace(req.PostingPermission))
	if postingPermission == "" {
		postingPermission = PostingPermissionMembers
	}
	if !postingPermission.Valid() {
		response.ValidationError(w, map[string]string{"posting_permission": "invalid posting permission"})
		return CreateGroupInput{}, false
	}
	return CreateGroupInput{
		Name:                name,
		Description:         trimOptional(req.Description),
		Rules:               trimOptional(req.Rules),
		AvatarURL:           trimOptional(req.AvatarURL),
		CoverURL:            trimOptional(req.CoverURL),
		Visibility:          visibility,
		PostingPermission:   postingPermission,
		AllowAnonymousPosts: req.AllowAnonymousPosts,
		City:                trimOptional(req.City),
		Country:             trimOptional(req.Country),
		Tags:                normalizeLabels(req.Tags, 12),
		RecoveryPathways:    normalizeLabels(req.RecoveryPathways, 8),
	}, true
}

func decodeCreatePostInput(w http.ResponseWriter, r *http.Request) (CreateGroupPostInput, bool) {
	var req groupPostRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		response.Error(w, http.StatusBadRequest, "invalid request body")
		return CreateGroupPostInput{}, false
	}
	body := strings.TrimSpace(req.Body)
	if body == "" || len(body) > 4000 {
		response.ValidationError(w, map[string]string{"body": "post must be 1 to 4000 characters"})
		return CreateGroupPostInput{}, false
	}
	postType := GroupPostType(strings.TrimSpace(req.PostType))
	if postType == "" {
		postType = GroupPostTypeStandard
	}
	if !postType.Valid() {
		response.ValidationError(w, map[string]string{"post_type": "invalid post type"})
		return CreateGroupPostInput{}, false
	}
	if len(req.Images) > 6 {
		response.ValidationError(w, map[string]string{"images": "add up to 6 images"})
		return CreateGroupPostInput{}, false
	}
	images := make([]CreateGroupPostImageInput, 0, len(req.Images))
	for _, image := range req.Images {
		imageURL := strings.TrimSpace(image.ImageURL)
		if imageURL == "" || image.Width <= 0 || image.Height <= 0 {
			response.ValidationError(w, map[string]string{"images": "image url and dimensions are required"})
			return CreateGroupPostInput{}, false
		}
		images = append(images, CreateGroupPostImageInput{
			ImageURL: imageURL,
			ThumbURL: trimOptional(image.ThumbURL),
			Width:    image.Width,
			Height:   image.Height,
		})
	}
	return CreateGroupPostInput{
		PostType:  postType,
		Body:      body,
		Anonymous: req.Anonymous,
		Images:    images,
	}, true
}

func parseGroupID(w http.ResponseWriter, r *http.Request) (uuid.UUID, bool) {
	groupID, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		response.Error(w, http.StatusBadRequest, "invalid group id")
		return uuid.Nil, false
	}
	return groupID, true
}

func parseGroupAndPostID(w http.ResponseWriter, r *http.Request) (uuid.UUID, uuid.UUID, bool) {
	groupID, ok := parseGroupID(w, r)
	if !ok {
		return uuid.Nil, uuid.Nil, false
	}
	postID, err := uuid.Parse(chi.URLParam(r, "postId"))
	if err != nil {
		response.Error(w, http.StatusBadRequest, "invalid post id")
		return uuid.Nil, uuid.Nil, false
	}
	return groupID, postID, true
}

func parseGroupAndRequestID(w http.ResponseWriter, r *http.Request) (uuid.UUID, uuid.UUID, bool) {
	groupID, ok := parseGroupID(w, r)
	if !ok {
		return uuid.Nil, uuid.Nil, false
	}
	requestID, err := uuid.Parse(chi.URLParam(r, "requestId"))
	if err != nil {
		response.Error(w, http.StatusBadRequest, "invalid request id")
		return uuid.Nil, uuid.Nil, false
	}
	return groupID, requestID, true
}

func parseGroupAndThreadID(w http.ResponseWriter, r *http.Request) (uuid.UUID, uuid.UUID, bool) {
	groupID, ok := parseGroupID(w, r)
	if !ok {
		return uuid.Nil, uuid.Nil, false
	}
	threadID, err := uuid.Parse(chi.URLParam(r, "threadId"))
	if err != nil {
		response.Error(w, http.StatusBadRequest, "invalid thread id")
		return uuid.Nil, uuid.Nil, false
	}
	return groupID, threadID, true
}

func trimOptional(value *string) *string {
	if value == nil {
		return nil
	}
	trimmed := strings.TrimSpace(*value)
	if trimmed == "" {
		return nil
	}
	return &trimmed
}

func normalizeLabels(values []string, limit int) []string {
	seen := make(map[string]struct{}, len(values))
	out := make([]string, 0, len(values))
	for _, value := range values {
		normalized := strings.ToLower(strings.TrimSpace(value))
		if normalized == "" {
			continue
		}
		if _, ok := seen[normalized]; ok {
			continue
		}
		seen[normalized] = struct{}{}
		out = append(out, normalized)
		if len(out) >= limit {
			break
		}
	}
	return out
}
