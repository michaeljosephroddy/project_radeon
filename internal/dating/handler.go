package dating

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"image"
	_ "image/jpeg"
	_ "image/png"
	"io"
	"log"
	"mime/multipart"
	"net/http"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/project_radeon/api/pkg/middleware"
	"github.com/project_radeon/api/pkg/pagination"
	"github.com/project_radeon/api/pkg/response"
)

const datingCursorVersion = 1

type datingDiscoverCursor struct {
	Mode      string `json:"mode"`
	Version   int    `json:"version"`
	RequestID string `json:"request_id"`
	Offset    int    `json:"offset"`
}

type Querier interface {
	GetMyProfile(ctx context.Context, userID uuid.UUID) (*DatingProfile, error)
	GetProfile(ctx context.Context, viewerID, profileID uuid.UUID) (*DatingProfile, error)
	UpdateMyProfile(ctx context.Context, userID uuid.UUID, input UpdateProfileInput) (*DatingProfile, error)
	ListInterests(ctx context.Context) ([]string, error)
	AddPhoto(ctx context.Context, userID uuid.UUID, imageURL string, width, height int) (*DatingProfile, error)
	DeletePhoto(ctx context.Context, userID, photoID uuid.UUID) (*DatingProfile, error)
	ReorderPhotos(ctx context.Context, userID uuid.UUID, photoIDs []uuid.UUID) (*DatingProfile, error)
	Discover(ctx context.Context, params DiscoverParams) ([]DatingProfile, error)
	CountDiscover(ctx context.Context, params DiscoverParams) (int, error)
	ListLikes(ctx context.Context, userID uuid.UUID, before *string, limit int) ([]DatingLike, error)
	CountLikes(ctx context.Context, userID uuid.UUID) (int, error)
	RecordAction(ctx context.Context, actorID, targetID uuid.UUID, action string) (*ActionResult, error)
	ListMatches(ctx context.Context, userID uuid.UUID, before *string, limit int) ([]DatingMatch, error)
	CountUnseenMatches(ctx context.Context, userID uuid.UUID) (int, error)
	MarkMatchesSeen(ctx context.Context, userID uuid.UUID) (time.Time, error)
	GetMatch(ctx context.Context, userID, matchID uuid.UUID) (*DatingMatch, error)
	Unmatch(ctx context.Context, userID, matchID uuid.UUID) (*DatingMatch, error)
	LogEvents(ctx context.Context, userID uuid.UUID, events []DatingEventInput) error
}

type Notifier interface {
	NotifyDatingMatch(ctx context.Context, matchID, chatID, actorID, recipientID uuid.UUID) error
}

type Uploader interface {
	Upload(ctx context.Context, key, contentType string, body io.Reader) (string, error)
}

type Handler struct {
	db       Querier
	notifier Notifier
	uploader Uploader
}

func NewHandler(db Querier, notifier Notifier, uploaders ...Uploader) *Handler {
	var uploader Uploader
	if len(uploaders) > 0 {
		uploader = uploaders[0]
	}
	return &Handler{db: db, notifier: notifier, uploader: uploader}
}

func (h *Handler) GetMyProfile(w http.ResponseWriter, r *http.Request) {
	profile, err := h.db.GetMyProfile(r.Context(), middleware.CurrentUserID(r))
	if err != nil {
		writeDatingError(w, err)
		return
	}
	response.Success(w, http.StatusOK, profile)
}

func (h *Handler) GetProfile(w http.ResponseWriter, r *http.Request) {
	profileID, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		response.Error(w, http.StatusBadRequest, "invalid dating profile id")
		return
	}
	profile, err := h.db.GetProfile(r.Context(), middleware.CurrentUserID(r), profileID)
	if err != nil {
		writeDatingError(w, err)
		return
	}
	response.Success(w, http.StatusOK, publicDatingProfile(*profile))
}

func (h *Handler) UpdateMyProfile(w http.ResponseWriter, r *http.Request) {
	body, err := io.ReadAll(r.Body)
	if err != nil {
		response.Error(w, http.StatusBadRequest, "invalid request body")
		return
	}
	var input struct {
		Bio                 *string   `json:"bio"`
		RelationshipGoal    *string   `json:"relationship_goal"`
		InterestedInGenders *[]string `json:"interested_in_genders"`
		HeightCm            *int      `json:"height_cm"`
		JobTitle            *string   `json:"job_title"`
		Company             *string   `json:"company"`
		Work                *string   `json:"work"`
		School              *string   `json:"school"`
		Course              *string   `json:"course"`
		Education           *string   `json:"education"`
		KidsStatus          *string   `json:"kids_status"`
		Interests           *[]string `json:"interests"`
		PromptAnswers       *[]struct {
			PromptKey string `json:"prompt_key"`
			Answer    string `json:"answer"`
		} `json:"prompt_answers"`
		AgeMin     *int  `json:"age_min"`
		AgeMax     *int  `json:"age_max"`
		DistanceKm *int  `json:"distance_km"`
		Paused     *bool `json:"paused"`
		Complete   bool  `json:"complete"`
	}
	if err := json.Unmarshal(body, &input); err != nil {
		response.Error(w, http.StatusBadRequest, "invalid request body")
		return
	}
	var rawFields map[string]json.RawMessage
	if err := json.Unmarshal(body, &rawFields); err != nil {
		response.Error(w, http.StatusBadRequest, "invalid request body")
		return
	}
	update := UpdateProfileInput{
		Bio:              trimOptionalString(input.Bio),
		RelationshipGoal: normalizeRelationshipGoal(input.RelationshipGoal),
		JobTitle:         trimOptionalString(input.JobTitle),
		Company:          trimOptionalString(input.Company),
		Work:             trimOptionalString(input.Work),
		School:           trimOptionalString(input.School),
		Course:           trimOptionalString(input.Course),
		Education:        trimOptionalString(input.Education),
		KidsStatus:       normalizeKidsStatus(input.KidsStatus),
		AgeMin:           input.AgeMin,
		AgeMax:           input.AgeMax,
		DistanceKm:       input.DistanceKm,
		Paused:           input.Paused,
		Complete:         input.Complete,
	}
	if update.JobTitle == nil {
		update.JobTitle = update.Work
	}
	if update.Course == nil {
		update.Course = update.Education
	}
	if _, ok := rawFields["height_cm"]; ok {
		update.HeightCm = input.HeightCm
		update.ReplaceHeight = true
	}
	if input.InterestedInGenders != nil {
		genders, ok := normalizeDatingGenders(*input.InterestedInGenders)
		if !ok {
			response.Error(w, http.StatusBadRequest, "interested_in_genders contains an unsupported gender")
			return
		}
		update.InterestedInGenders = genders
		update.ReplaceGenders = true
	}
	if update.RelationshipGoal != nil && !validRelationshipGoal(*update.RelationshipGoal) {
		response.Error(w, http.StatusBadRequest, "relationship_goal is invalid")
		return
	}
	if update.HeightCm != nil && (*update.HeightCm < 90 || *update.HeightCm > 230) {
		response.Error(w, http.StatusBadRequest, "height_cm must be between 90 and 230")
		return
	}
	if update.JobTitle != nil && len([]rune(*update.JobTitle)) > 80 {
		response.Error(w, http.StatusBadRequest, "job_title must be 80 characters or fewer")
		return
	}
	if update.Company != nil && len([]rune(*update.Company)) > 80 {
		response.Error(w, http.StatusBadRequest, "company must be 80 characters or fewer")
		return
	}
	if update.School != nil && len([]rune(*update.School)) > 80 {
		response.Error(w, http.StatusBadRequest, "school must be 80 characters or fewer")
		return
	}
	if update.Course != nil && len([]rune(*update.Course)) > 80 {
		response.Error(w, http.StatusBadRequest, "course must be 80 characters or fewer")
		return
	}
	if update.KidsStatus != nil && !validKidsStatus(*update.KidsStatus) {
		response.Error(w, http.StatusBadRequest, "kids_status is invalid")
		return
	}
	if input.Interests != nil {
		interests, ok, err := h.normalizeDatingInterests(r.Context(), *input.Interests)
		if err != nil {
			log.Printf("update dating profile interests lookup failed for user %s: %v", middleware.CurrentUserID(r), err)
			response.Error(w, http.StatusInternalServerError, "could not load interests")
			return
		}
		if !ok {
			response.Error(w, http.StatusBadRequest, "interests contains an invalid value")
			return
		}
		update.Interests = interests
		update.ReplaceInterests = true
	}
	if input.PromptAnswers != nil {
		promptAnswers, ok := normalizeDatingPromptAnswers(*input.PromptAnswers)
		if !ok {
			response.Error(w, http.StatusBadRequest, "prompt_answers contains an invalid prompt or answer")
			return
		}
		update.PromptAnswers = promptAnswers
		update.ReplacePromptAnswers = true
	}
	if input.AgeMin != nil && (*input.AgeMin < 18 || *input.AgeMin > 100) {
		response.Error(w, http.StatusBadRequest, "age_min must be between 18 and 100")
		return
	}
	if input.AgeMax != nil && (*input.AgeMax < 18 || *input.AgeMax > 100) {
		response.Error(w, http.StatusBadRequest, "age_max must be between 18 and 100")
		return
	}
	if input.AgeMin != nil && input.AgeMax != nil && *input.AgeMin > *input.AgeMax {
		response.Error(w, http.StatusBadRequest, "age_min cannot be greater than age_max")
		return
	}
	if input.DistanceKm != nil && (*input.DistanceKm < 0 || *input.DistanceKm > 500) {
		response.Error(w, http.StatusBadRequest, "distance_km must be between 0 and 500")
		return
	}

	profile, err := h.db.UpdateMyProfile(r.Context(), middleware.CurrentUserID(r), update)
	if err != nil {
		writeDatingError(w, err)
		return
	}
	response.Success(w, http.StatusOK, profile)
}

func (h *Handler) UploadProfilePhoto(w http.ResponseWriter, r *http.Request) {
	if h.uploader == nil {
		response.Error(w, http.StatusInternalServerError, "image uploads are not configured")
		return
	}
	userID := middleware.CurrentUserID(r)
	imageFile, err := readUploadedDatingImage(r, "photo")
	if err != nil {
		response.Error(w, http.StatusBadRequest, err.Error())
		return
	}
	imageConfig, _, err := image.DecodeConfig(bytes.NewReader(imageFile.body))
	if err != nil {
		response.Error(w, http.StatusBadRequest, "could not decode image")
		return
	}
	key := fmt.Sprintf("dating-profiles/%s/%s%s", userID, uuid.New(), imageFile.extension)
	imageURL, err := h.uploader.Upload(r.Context(), key, imageFile.contentType, bytes.NewReader(imageFile.body))
	if err != nil {
		log.Printf("dating profile photo upload failed for user %s: %v", userID, err)
		response.Error(w, http.StatusInternalServerError, "could not upload image")
		return
	}
	profile, err := h.db.AddPhoto(r.Context(), userID, imageURL, imageConfig.Width, imageConfig.Height)
	if err != nil {
		writeDatingError(w, err)
		return
	}
	response.Success(w, http.StatusOK, profile)
}

func (h *Handler) DeleteProfilePhoto(w http.ResponseWriter, r *http.Request) {
	photoID, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		response.Error(w, http.StatusBadRequest, "invalid photo id")
		return
	}
	profile, err := h.db.DeletePhoto(r.Context(), middleware.CurrentUserID(r), photoID)
	if err != nil {
		writeDatingError(w, err)
		return
	}
	response.Success(w, http.StatusOK, profile)
}

func (h *Handler) ReorderProfilePhotos(w http.ResponseWriter, r *http.Request) {
	var input struct {
		PhotoIDs []string `json:"photo_ids"`
	}
	if err := json.NewDecoder(r.Body).Decode(&input); err != nil {
		response.Error(w, http.StatusBadRequest, "invalid request body")
		return
	}
	photoIDs := make([]uuid.UUID, 0, len(input.PhotoIDs))
	for _, raw := range input.PhotoIDs {
		photoID, err := uuid.Parse(strings.TrimSpace(raw))
		if err != nil {
			response.Error(w, http.StatusBadRequest, "photo_ids must contain valid photo ids")
			return
		}
		photoIDs = append(photoIDs, photoID)
	}
	profile, err := h.db.ReorderPhotos(r.Context(), middleware.CurrentUserID(r), photoIDs)
	if err != nil {
		writeDatingError(w, err)
		return
	}
	response.Success(w, http.StatusOK, profile)
}

func (h *Handler) Discover(w http.ResponseWriter, r *http.Request) {
	params, err := parseDiscoverRequest(r)
	if err != nil {
		response.Error(w, http.StatusBadRequest, err.Error())
		return
	}
	params.CurrentUserID = middleware.CurrentUserID(r)

	profiles, err := h.db.Discover(r.Context(), params)
	if err != nil {
		writeDatingError(w, err)
		return
	}

	limit := params.Limit
	hasMore := len(profiles) > limit
	if hasMore {
		profiles = profiles[:limit]
	}

	var nextCursor *string
	if hasMore {
		next := encodeDatingCursor(params.CursorRequestID, params.CursorOffset+limit)
		nextCursor = &next
	}

	response.Success(w, http.StatusOK, pagination.CursorResponse[PublicDatingProfile]{
		Items:      publicDatingProfiles(profiles),
		Limit:      limit,
		HasMore:    hasMore,
		NextCursor: nextCursor,
	})
}

func (h *Handler) DiscoverPreview(w http.ResponseWriter, r *http.Request) {
	params, err := parseDiscoverRequest(r)
	if err != nil {
		response.Error(w, http.StatusBadRequest, err.Error())
		return
	}
	params.CurrentUserID = middleware.CurrentUserID(r)

	count, err := h.db.CountDiscover(r.Context(), params)
	if err != nil {
		writeDatingError(w, err)
		return
	}

	response.Success(w, http.StatusOK, PreviewResponse{ExactCount: count})
}

func (h *Handler) ListLikes(w http.ResponseWriter, r *http.Request) {
	userID := middleware.CurrentUserID(r)
	params := pagination.ParseCursor(r, 12, 50)
	var before *string
	if params.Before != nil {
		value := params.Before.UTC().Format(time.RFC3339Nano)
		before = &value
	}

	likes, err := h.db.ListLikes(r.Context(), userID, before, params.Limit+1)
	if err != nil {
		writeDatingError(w, err)
		return
	}

	profiles := make([]DatingProfile, 0, len(likes))
	for _, like := range likes {
		profiles = append(profiles, like.Profile)
	}

	hasMore := len(likes) > params.Limit
	if hasMore {
		profiles = profiles[:params.Limit]
		likes = likes[:params.Limit]
	}

	var nextCursor *string
	if hasMore && len(likes) > 0 {
		value := likes[len(likes)-1].LikedAt.UTC().Format(time.RFC3339Nano)
		nextCursor = &value
	}

	response.Success(w, http.StatusOK, pagination.CursorResponse[PublicDatingProfile]{
		Items:      publicDatingProfiles(profiles),
		Limit:      params.Limit,
		HasMore:    hasMore,
		NextCursor: nextCursor,
	})
}

func (h *Handler) LikesPreview(w http.ResponseWriter, r *http.Request) {
	userID := middleware.CurrentUserID(r)
	count, err := h.db.CountLikes(r.Context(), userID)
	if err != nil {
		writeDatingError(w, err)
		return
	}
	response.Success(w, http.StatusOK, PreviewResponse{ExactCount: count})
}

func (h *Handler) RecordAction(w http.ResponseWriter, r *http.Request) {
	userID := middleware.CurrentUserID(r)

	var input struct {
		TargetProfileID string `json:"target_profile_id"`
		TargetUserID    string `json:"target_user_id"`
		Action          string `json:"action"`
	}
	if err := json.NewDecoder(r.Body).Decode(&input); err != nil {
		response.Error(w, http.StatusBadRequest, "invalid request body")
		return
	}

	targetRaw := strings.TrimSpace(input.TargetProfileID)
	if targetRaw == "" {
		targetRaw = strings.TrimSpace(input.TargetUserID)
	}
	targetID, err := uuid.Parse(targetRaw)
	if err != nil {
		response.Error(w, http.StatusBadRequest, "target_profile_id must be a valid dating profile id")
		return
	}

	action := strings.TrimSpace(input.Action)
	if action != ActionLike && action != ActionPass {
		response.Error(w, http.StatusBadRequest, "action must be like or pass")
		return
	}

	result, err := h.db.RecordAction(r.Context(), userID, targetID, action)
	if err != nil {
		writeDatingError(w, err)
		return
	}

	if result.Matched && result.Match != nil && result.Match.ChatID != nil && h.notifier != nil {
		_ = h.notifier.NotifyDatingMatch(r.Context(), result.Match.ID, *result.Match.ChatID, userID, result.Match.Profile.UserID)
	}

	status := http.StatusOK
	if action == ActionLike || action == ActionPass {
		status = http.StatusCreated
	}
	response.Success(w, status, actionResultResponse(result))
}

func (h *Handler) ListMatches(w http.ResponseWriter, r *http.Request) {
	userID := middleware.CurrentUserID(r)
	params := pagination.ParseCursor(r, 20, 50)
	var before *string
	if params.Before != nil {
		value := params.Before.UTC().Format("2006-01-02T15:04:05.999999999Z07:00")
		before = &value
	}

	matches, err := h.db.ListMatches(r.Context(), userID, before, params.Limit+1)
	if err != nil {
		writeDatingError(w, err)
		return
	}
	unseenCount, err := h.db.CountUnseenMatches(r.Context(), userID)
	if err != nil {
		writeDatingError(w, err)
		return
	}

	page := pagination.CursorSlice(datingMatchResponses(matches), params.Limit, func(match DatingMatchResponse) time.Time {
		return match.MatchedAt
	})
	response.Success(w, http.StatusOK, DatingMatchesPage{
		Items:       page.Items,
		Limit:       page.Limit,
		HasMore:     page.HasMore,
		NextCursor:  page.NextCursor,
		UnseenCount: unseenCount,
	})
}

func (h *Handler) MarkMatchesSeen(w http.ResponseWriter, r *http.Request) {
	userID := middleware.CurrentUserID(r)
	seenAt, err := h.db.MarkMatchesSeen(r.Context(), userID)
	if err != nil {
		writeDatingError(w, err)
		return
	}
	response.Success(w, http.StatusOK, DatingMatchesSeenResponse{
		SeenAt:      seenAt,
		UnseenCount: 0,
	})
}

func (h *Handler) GetMatch(w http.ResponseWriter, r *http.Request) {
	userID := middleware.CurrentUserID(r)
	matchID, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		response.Error(w, http.StatusBadRequest, "invalid match id")
		return
	}

	match, err := h.db.GetMatch(r.Context(), userID, matchID)
	if err != nil {
		writeDatingError(w, err)
		return
	}
	response.Success(w, http.StatusOK, datingMatchResponse(*match))
}

func (h *Handler) Unmatch(w http.ResponseWriter, r *http.Request) {
	userID := middleware.CurrentUserID(r)
	matchID, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		response.Error(w, http.StatusBadRequest, "invalid match id")
		return
	}

	match, err := h.db.Unmatch(r.Context(), userID, matchID)
	if err != nil {
		writeDatingError(w, err)
		return
	}
	response.Success(w, http.StatusOK, datingMatchResponse(*match))
}

func (h *Handler) LogEvents(w http.ResponseWriter, r *http.Request) {
	userID := middleware.CurrentUserID(r)

	var input struct {
		Events []DatingEventInput `json:"events"`
	}
	if err := json.NewDecoder(r.Body).Decode(&input); err != nil {
		response.Error(w, http.StatusBadRequest, "invalid request body")
		return
	}
	if len(input.Events) > 25 {
		response.Error(w, http.StatusBadRequest, "events can contain at most 25 items")
		return
	}
	if err := h.db.LogEvents(r.Context(), userID, input.Events); err != nil {
		if errors.Is(err, ErrInvalidDatingEvent) {
			response.Error(w, http.StatusBadRequest, err.Error())
			return
		}
		response.Error(w, http.StatusInternalServerError, "could not log dating events")
		return
	}
	response.Success(w, http.StatusOK, map[string]any{"logged": len(input.Events)})
}

func publicDatingProfile(profile DatingProfile) PublicDatingProfile {
	return PublicDatingProfile{
		ID:               profile.ID,
		UserID:           profile.UserID,
		Username:         profile.Username,
		Age:              profile.Age,
		City:             profile.City,
		Country:          profile.Country,
		Bio:              profile.Bio,
		RelationshipGoal: profile.RelationshipGoal,
		HeightCm:         profile.HeightCm,
		JobTitle:         profile.JobTitle,
		Company:          profile.Company,
		Work:             profile.Work,
		School:           profile.School,
		Course:           profile.Course,
		Education:        profile.Education,
		KidsStatus:       profile.KidsStatus,
		Interests:        profile.Interests,
		Photos:           profile.Photos,
		PromptAnswers:    profile.PromptAnswers,
		CreatedAt:        profile.CreatedAt,
		UpdatedAt:        profile.UpdatedAt,
	}
}

func publicDatingProfiles(profiles []DatingProfile) []PublicDatingProfile {
	items := make([]PublicDatingProfile, 0, len(profiles))
	for _, profile := range profiles {
		items = append(items, publicDatingProfile(profile))
	}
	return items
}

func datingMatchResponse(match DatingMatch) DatingMatchResponse {
	return DatingMatchResponse{
		ID:          match.ID,
		Profile:     publicDatingProfile(match.Profile),
		ChatID:      match.ChatID,
		Status:      match.Status,
		MatchedAt:   match.MatchedAt,
		UnmatchedAt: match.UnmatchedAt,
	}
}

func datingMatchResponses(matches []DatingMatch) []DatingMatchResponse {
	items := make([]DatingMatchResponse, 0, len(matches))
	for _, match := range matches {
		items = append(items, datingMatchResponse(match))
	}
	return items
}

func actionResultResponse(result *ActionResult) ActionResultResponse {
	response := ActionResultResponse{
		Action:  result.Action,
		Matched: result.Matched,
	}
	if result.Match != nil {
		match := datingMatchResponse(*result.Match)
		response.Match = &match
	}
	return response
}

func writeDatingError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, ErrNotFound):
		response.Error(w, http.StatusNotFound, "dating resource not found")
	case errors.Is(err, ErrProfileIncomplete):
		response.Error(w, http.StatusUnprocessableEntity, "complete your Dating profile before using Dating")
	case errors.Is(err, ErrDatingDisabled):
		response.Error(w, http.StatusForbidden, "turn on Dating mode to use Dating")
	case errors.Is(err, ErrTargetUnavailable):
		response.Error(w, http.StatusForbidden, "this user is not available in Dating")
	case errors.Is(err, ErrForbidden):
		response.Error(w, http.StatusForbidden, "dating action is not allowed")
	case errors.Is(err, ErrConflict):
		response.Error(w, http.StatusConflict, "dating action already recorded")
	case errors.Is(err, ErrPlusRequired):
		response.Error(w, http.StatusPaymentRequired, "SoberSpace Plus is required for this Dating feature")
	default:
		log.Printf("dating request failed: %v", err)
		response.Error(w, http.StatusInternalServerError, "dating request failed")
	}
}

type uploadedDatingImage struct {
	body        []byte
	contentType string
	extension   string
}

func readUploadedDatingImage(r *http.Request, fieldName string) (*uploadedDatingImage, error) {
	if err := r.ParseMultipartForm(20 << 20); err != nil {
		return nil, errors.New("could not read image")
	}
	file, header, err := r.FormFile(fieldName)
	if err != nil {
		return nil, errors.New("photo field is required")
	}
	defer file.Close()

	if header.Size > 20<<20 {
		return nil, errors.New("image must be 20MB or smaller")
	}
	body, err := io.ReadAll(io.LimitReader(file, 20<<20+1))
	if err != nil {
		return nil, errors.New("could not read image")
	}
	if len(body) > 20<<20 {
		return nil, errors.New("image must be 20MB or smaller")
	}
	contentType := http.DetectContentType(body)
	extension, ok := datingImageExtension(contentType, header)
	if !ok {
		return nil, errors.New("image must be a JPEG or PNG image")
	}
	return &uploadedDatingImage{body: body, contentType: contentType, extension: extension}, nil
}

func datingImageExtension(contentType string, header *multipart.FileHeader) (string, bool) {
	switch contentType {
	case "image/jpeg":
		return ".jpg", true
	case "image/png":
		return ".png", true
	}
	ext := strings.ToLower(filepath.Ext(header.Filename))
	if ext == ".jpg" || ext == ".jpeg" {
		return ".jpg", contentType == "image/jpeg"
	}
	if ext == ".png" {
		return ".png", contentType == "image/png"
	}
	return "", false
}

func trimOptionalString(value *string) *string {
	if value == nil {
		return nil
	}
	trimmed := strings.TrimSpace(*value)
	return &trimmed
}

func normalizeRelationshipGoal(value *string) *string {
	if value == nil {
		return nil
	}
	normalized := strings.TrimSpace(strings.ToLower(*value))
	return &normalized
}

func validRelationshipGoal(value string) bool {
	switch value {
	case "", "long_term", "life_partner", "casual", "open_to_explore":
		return true
	default:
		return false
	}
}

func normalizeKidsStatus(value *string) *string {
	if value == nil {
		return nil
	}
	normalized := strings.TrimSpace(strings.ToLower(*value))
	return &normalized
}

func validKidsStatus(value string) bool {
	switch value {
	case "", "have_kids", "dont_have_kids", "prefer_not_to_say":
		return true
	default:
		return false
	}
}

func normalizeDatingPromptAnswers(values []struct {
	PromptKey string `json:"prompt_key"`
	Answer    string `json:"answer"`
}) ([]DatingPromptAnswerInput, bool) {
	if len(values) > 3 {
		return nil, false
	}
	seen := make(map[string]struct{}, len(values))
	answers := make([]DatingPromptAnswerInput, 0, len(values))
	for _, value := range values {
		promptKey := strings.TrimSpace(strings.ToLower(value.PromptKey))
		answer := strings.TrimSpace(value.Answer)
		if !validDatingPromptKey(promptKey) || len([]rune(answer)) == 0 || len([]rune(answer)) > 220 {
			return nil, false
		}
		if _, exists := seen[promptKey]; exists {
			return nil, false
		}
		seen[promptKey] = struct{}{}
		answers = append(answers, DatingPromptAnswerInput{PromptKey: promptKey, Answer: answer})
	}
	return answers, true
}

func validDatingPromptKey(value string) bool {
	switch value {
	case "small_thing_about_me",
		"friends_describe_me",
		"proud_of",
		"happiest_when",
		"simple_pleasure",
		"recovery_lifestyle",
		"best_part_sobriety",
		"ideal_sober_date",
		"sober_win",
		"how_i_reset",
		"looking_for",
		"green_flag",
		"great_first_date",
		"chemistry_when",
		"dating_intention",
		"make_time_for",
		"value_i_live_by",
		"matters_most",
		"feel_connected_when",
		"relationship_works_when",
		"perfect_sunday",
		"usually_find_me",
		"sober_weekend",
		"recharge",
		"next_adventure",
		"ask_me_about",
		"teach_me_about",
		"lets_debate",
		"make_me_laugh",
		"voice_note_includes":
		return true
	default:
		return false
	}
}

func (h *Handler) normalizeDatingInterests(ctx context.Context, values []string) ([]string, bool, error) {
	if len(values) > 5 {
		return nil, false, nil
	}
	allowedInterests, err := h.db.ListInterests(ctx)
	if err != nil {
		return nil, false, err
	}
	allowedSet := make(map[string]struct{}, len(allowedInterests))
	for _, interest := range allowedInterests {
		allowedSet[interest] = struct{}{}
	}

	seen := make(map[string]struct{}, len(values))
	normalized := make([]string, 0, len(values))
	for _, raw := range values {
		interest := strings.TrimSpace(raw)
		if interest == "" {
			return nil, false, nil
		}
		if _, exists := allowedSet[interest]; !exists {
			return nil, false, nil
		}
		if _, exists := seen[interest]; exists {
			return nil, false, nil
		}
		seen[interest] = struct{}{}
		normalized = append(normalized, interest)
	}
	sort.Strings(normalized)
	return normalized, true, nil
}

func normalizeDatingGenders(values []string) ([]string, bool) {
	seen := map[string]bool{}
	normalized := make([]string, 0, len(values))
	for _, value := range values {
		gender := strings.TrimSpace(strings.ToLower(value))
		if gender == "" || seen[gender] {
			continue
		}
		if gender != "woman" && gender != "man" && gender != "non_binary" {
			return nil, false
		}
		seen[gender] = true
		normalized = append(normalized, gender)
	}
	return normalized, true
}

func parseDiscoverRequest(r *http.Request) (DiscoverParams, error) {
	query := r.URL.Query()
	limit := parsePositiveInt(query.Get("limit"), 20)
	if limit > 50 {
		limit = 50
	}

	params := DiscoverParams{
		Gender:    strings.TrimSpace(query.Get("gender")),
		Sobriety:  strings.TrimSpace(query.Get("sobriety")),
		Interests: query["interest"],
		Cursor:    strings.TrimSpace(query.Get("cursor")),
		Limit:     limit,
	}
	decodedCursor := decodeDatingCursor(params.Cursor)
	params.CursorOffset = decodedCursor.Offset
	params.CursorRequestID = decodedCursor.RequestID
	if params.CursorRequestID == "" {
		params.CursorRequestID = uuid.NewString()
	}

	if params.Gender != "" && params.Gender != "woman" && params.Gender != "man" && params.Gender != "non_binary" {
		return DiscoverParams{}, fmt.Errorf("gender must be woman, man, or non_binary")
	}
	if params.Sobriety != "" && params.Sobriety != "days_30" && params.Sobriety != "days_90" && params.Sobriety != "years_1" && params.Sobriety != "years_5" {
		return DiscoverParams{}, fmt.Errorf("sobriety must be 30+ days, 90+ days, 1+ year, or 5+ years")
	}

	var err error
	if params.AgeMin, err = parseOptionalInt(query.Get("age_min"), "age_min"); err != nil {
		return DiscoverParams{}, err
	}
	if params.AgeMax, err = parseOptionalInt(query.Get("age_max"), "age_max"); err != nil {
		return DiscoverParams{}, err
	}
	if params.AgeMin != nil && params.AgeMax != nil && *params.AgeMin > *params.AgeMax {
		return DiscoverParams{}, fmt.Errorf("age_min cannot be greater than age_max")
	}
	if params.DistanceKm, err = parseOptionalInt(query.Get("distance_km"), "distance_km"); err != nil {
		return DiscoverParams{}, err
	}
	if params.Lat, err = parseOptionalFloat(query.Get("lat"), "lat"); err != nil {
		return DiscoverParams{}, err
	}
	if params.Lng, err = parseOptionalFloat(query.Get("lng"), "lng"); err != nil {
		return DiscoverParams{}, err
	}
	if params.DistanceKm != nil && *params.DistanceKm > 0 && (params.Lat == nil || params.Lng == nil) {
		return DiscoverParams{}, fmt.Errorf("lat and lng are required when distance_km is set")
	}
	return params, nil
}

func parsePositiveInt(raw string, fallback int) int {
	value, err := strconv.Atoi(strings.TrimSpace(raw))
	if err != nil || value < 1 {
		return fallback
	}
	return value
}

func parseOffset(raw string) int {
	return decodeDatingCursor(raw).Offset
}

func encodeDatingCursor(requestID string, offset int) string {
	if offset < 0 {
		offset = 0
	}
	payload, err := json.Marshal(datingDiscoverCursor{
		Mode:      "dating_ranked",
		Version:   datingCursorVersion,
		RequestID: strings.TrimSpace(requestID),
		Offset:    offset,
	})
	if err != nil {
		return strconv.Itoa(offset)
	}
	return base64.RawURLEncoding.EncodeToString(payload)
}

func decodeDatingCursor(raw string) datingDiscoverCursor {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return datingDiscoverCursor{}
	}
	if offset, err := strconv.Atoi(raw); err == nil {
		if offset < 0 {
			offset = 0
		}
		return datingDiscoverCursor{Offset: offset}
	}

	payload, err := base64.RawURLEncoding.DecodeString(raw)
	if err != nil {
		return datingDiscoverCursor{}
	}
	var cursor datingDiscoverCursor
	if err := json.Unmarshal(payload, &cursor); err != nil {
		return datingDiscoverCursor{}
	}
	if cursor.Mode != "dating_ranked" || cursor.Version != datingCursorVersion {
		return datingDiscoverCursor{}
	}
	if cursor.Offset < 0 {
		cursor.Offset = 0
	}
	cursor.RequestID = strings.TrimSpace(cursor.RequestID)
	return cursor
}

func parseOptionalInt(raw string, name string) (*int, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return nil, nil
	}
	value, err := strconv.Atoi(raw)
	if err != nil {
		return nil, fmt.Errorf("%s must be a valid integer", name)
	}
	return &value, nil
}

func parseOptionalFloat(raw string, name string) (*float64, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return nil, nil
	}
	value, err := strconv.ParseFloat(raw, 64)
	if err != nil {
		return nil, fmt.Errorf("%s must be a valid number", name)
	}
	return &value, nil
}
