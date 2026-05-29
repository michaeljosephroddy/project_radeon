package moderation

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

var ErrBlocked = errors.New("content was blocked by moderation")

func UserMessage(err error) string {
	if errors.Is(err, ErrBlocked) {
		return "This content cannot be posted. Please edit it and try again."
	}
	return "Content moderation is temporarily unavailable. Please try again later."
}

type Service interface {
	CheckText(ctx context.Context, surface, text string) error
	CheckImage(ctx context.Context, surface, contentType string, body []byte) error
}

type Config struct {
	Enabled bool
	APIKey  string
	Model   string
	Timeout time.Duration
}

type store struct {
	pool *pgxpool.Pool
}

type service struct {
	enabled bool
	apiKey  string
	model   string
	client  *http.Client
	store   *store
}

func New(config Config, pool *pgxpool.Pool) Service {
	if !config.Enabled {
		return disabledService{}
	}
	if config.Model == "" {
		config.Model = "omni-moderation-latest"
	}
	if config.Timeout <= 0 {
		config.Timeout = 5 * time.Second
	}
	return &service{
		enabled: true,
		apiKey:  strings.TrimSpace(config.APIKey),
		model:   config.Model,
		client:  &http.Client{Timeout: config.Timeout},
		store:   &store{pool: pool},
	}
}

func Disabled() Service {
	return disabledService{}
}

type disabledService struct{}

func (disabledService) CheckText(context.Context, string, string) error {
	return nil
}

func (disabledService) CheckImage(context.Context, string, string, []byte) error {
	return nil
}

func (s *service) CheckText(ctx context.Context, surface, text string) error {
	text = strings.TrimSpace(text)
	if text == "" {
		return nil
	}
	return s.check(ctx, surface, "text", []moderationInput{{Type: "text", Text: text}})
}

func (s *service) CheckImage(ctx context.Context, surface, contentType string, body []byte) error {
	if len(body) == 0 {
		return nil
	}
	if contentType == "" {
		contentType = http.DetectContentType(body)
	}
	dataURL := fmt.Sprintf("data:%s;base64,%s", contentType, base64.StdEncoding.EncodeToString(body))
	return s.check(ctx, surface, "image", []moderationInput{{
		Type: "image_url",
		ImageURL: &moderationImageURL{
			URL: dataURL,
		},
	}})
}

func (s *service) check(ctx context.Context, surface, contentKind string, input []moderationInput) error {
	if strings.TrimSpace(s.apiKey) == "" {
		return errors.New("content moderation is not configured")
	}

	payload := moderationRequest{
		Model: s.model,
		Input: input,
	}
	body, err := json.Marshal(payload)
	if err != nil {
		return err
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, "https://api.openai.com/v1/moderations", bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+s.apiKey)
	req.Header.Set("Content-Type", "application/json")

	res, err := s.client.Do(req)
	if err != nil {
		return errors.New("content moderation is temporarily unavailable")
	}
	defer res.Body.Close()

	resBody, err := io.ReadAll(io.LimitReader(res.Body, 4<<20))
	if err != nil {
		return errors.New("content moderation is temporarily unavailable")
	}
	if res.StatusCode < 200 || res.StatusCode >= 300 {
		_ = s.store.record(ctx, moderationEvent{
			Surface:     surface,
			ContentKind: contentKind,
			Model:       s.model,
			Action:      "provider_error",
			Flagged:     false,
		})
		return errors.New("content moderation is temporarily unavailable")
	}

	var parsed moderationResponse
	if err := json.Unmarshal(resBody, &parsed); err != nil {
		return errors.New("content moderation is temporarily unavailable")
	}

	flagged := false
	var categories json.RawMessage
	var scores json.RawMessage
	if len(parsed.Results) > 0 {
		flagged = parsed.Results[0].Flagged
		categories, _ = json.Marshal(parsed.Results[0].Categories)
		scores, _ = json.Marshal(parsed.Results[0].CategoryScores)
	}
	action := "allowed"
	if flagged {
		action = "blocked"
	}
	_ = s.store.record(ctx, moderationEvent{
		Surface:        surface,
		ContentKind:    contentKind,
		Model:          s.model,
		Action:         action,
		Flagged:        flagged,
		Categories:     categories,
		CategoryScores: scores,
	})

	if flagged {
		return ErrBlocked
	}
	return nil
}

type moderationRequest struct {
	Model string            `json:"model"`
	Input []moderationInput `json:"input"`
}

type moderationInput struct {
	Type     string              `json:"type"`
	Text     string              `json:"text,omitempty"`
	ImageURL *moderationImageURL `json:"image_url,omitempty"`
}

type moderationImageURL struct {
	URL string `json:"url"`
}

type moderationResponse struct {
	Results []struct {
		Flagged        bool               `json:"flagged"`
		Categories     map[string]bool    `json:"categories"`
		CategoryScores map[string]float64 `json:"category_scores"`
	} `json:"results"`
}

type moderationEvent struct {
	Surface        string
	ContentKind    string
	Model          string
	Action         string
	Flagged        bool
	Categories     json.RawMessage
	CategoryScores json.RawMessage
}

func (s *store) record(ctx context.Context, event moderationEvent) error {
	if s == nil || s.pool == nil {
		return nil
	}
	_, err := s.pool.Exec(ctx, `
		INSERT INTO content_moderation_events (
			surface,
			content_kind,
			provider,
			model,
			action,
			flagged,
			categories,
			category_scores
		)
		VALUES ($1, $2, 'openai', $3, $4, $5, COALESCE($6::jsonb, '{}'::jsonb), COALESCE($7::jsonb, '{}'::jsonb))`,
		event.Surface,
		event.ContentKind,
		event.Model,
		event.Action,
		event.Flagged,
		event.Categories,
		event.CategoryScores,
	)
	return err
}

type moderatingUploader struct {
	inner   Uploader
	service Service
}

type Uploader interface {
	Upload(ctx context.Context, key, contentType string, body io.Reader) (string, error)
}

func NewModeratingUploader(inner Uploader, service Service) Uploader {
	if inner == nil {
		return nil
	}
	if service == nil {
		service = Disabled()
	}
	return &moderatingUploader{inner: inner, service: service}
}

func (u *moderatingUploader) Upload(ctx context.Context, key, contentType string, body io.Reader) (string, error) {
	data, err := io.ReadAll(body)
	if err != nil {
		return "", err
	}
	if err := u.service.CheckImage(ctx, uploadSurfaceForKey(key), contentType, data); err != nil {
		return "", err
	}
	return u.inner.Upload(ctx, key, contentType, bytes.NewReader(data))
}

func uploadSurfaceForKey(key string) string {
	switch {
	case strings.HasPrefix(key, "posts/"):
		return "post_image"
	case strings.HasPrefix(key, "groups/"):
		return "group_image"
	case strings.HasPrefix(key, "meetups/"):
		return "meetup_image"
	case strings.HasPrefix(key, "avatars/"):
		return "avatar_image"
	default:
		return "image_upload"
	}
}
