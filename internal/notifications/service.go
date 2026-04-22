package notifications

import (
	"context"
	"errors"
	"log"
	"strings"
	"time"

	"github.com/google/uuid"
)

type Service struct {
	store    Store
	provider PushProvider
	now      func() time.Time
}

func NewService(store Store, provider PushProvider) *Service {
	return &Service{
		store:    store,
		provider: provider,
		now:      time.Now,
	}
}

func (s *Service) RegisterDevice(ctx context.Context, userID uuid.UUID, input RegisterDeviceInput) (*Device, error) {
	input.PushToken = strings.TrimSpace(input.PushToken)
	input.Platform = strings.TrimSpace(input.Platform)
	input.DeviceName = strings.TrimSpace(input.DeviceName)
	input.AppVersion = strings.TrimSpace(input.AppVersion)
	if input.PushToken == "" {
		return nil, errors.New("push token is required")
	}
	if input.Platform != "ios" && input.Platform != "android" {
		return nil, errors.New("platform must be ios or android")
	}
	return s.store.UpsertDevice(ctx, userID, input)
}

func (s *Service) DeleteDevice(ctx context.Context, userID, deviceID uuid.UUID) error {
	return s.store.DeleteDevice(ctx, userID, deviceID)
}

func (s *Service) GetPreferences(ctx context.Context, userID uuid.UUID) (*Preferences, error) {
	return s.store.GetPreferences(ctx, userID)
}

func (s *Service) UpdatePreferences(ctx context.Context, userID uuid.UUID, input Preferences) (*Preferences, error) {
	return s.store.UpdatePreferences(ctx, userID, input)
}

func (s *Service) ListNotifications(ctx context.Context, userID uuid.UUID, before *time.Time, limit int) ([]Notification, error) {
	return s.store.ListNotifications(ctx, userID, before, limit)
}

func (s *Service) MarkNotificationRead(ctx context.Context, userID, notificationID uuid.UUID) error {
	return s.store.MarkNotificationRead(ctx, userID, notificationID, s.now().UTC())
}

func (s *Service) MarkChatRead(ctx context.Context, chatID, userID uuid.UUID, lastReadMessageID *uuid.UUID, readAt time.Time) error {
	return s.store.MarkChatRead(ctx, chatID, userID, lastReadMessageID, readAt.UTC())
}

func (s *Service) NotifyChatMessage(ctx context.Context, chatID, messageID, senderID uuid.UUID, body string) error {
	return s.store.CreateChatMessageNotifications(ctx, chatID, messageID, senderID, body)
}

func (s *Service) NotifyCommentMentions(ctx context.Context, postID, commentID, authorID uuid.UUID, mentionedUserIDs []uuid.UUID, body string) error {
	return s.store.CreateCommentMentionNotifications(ctx, postID, commentID, authorID, mentionedUserIDs, body)
}

func (s *Service) ProcessPendingDeliveries(ctx context.Context, limit int) error {
	if s.provider == nil {
		return nil
	}

	jobs, err := s.store.ClaimPendingDeliveries(ctx, limit, s.now().UTC())
	if err != nil {
		return err
	}
	for _, job := range jobs {
		result, sendErr := s.provider.Send(ctx, PushMessage{
			To:    job.PushToken,
			Title: job.Title,
			Body:  job.Body,
			Data:  job.Payload,
		})
		if sendErr != nil {
			nextAttemptAt := s.now().UTC().Add(2 * time.Minute)
			if err := s.store.MarkDeliveryFailed(ctx, job.ID, true, sendErr.Error(), nextAttemptAt); err != nil {
				return err
			}
			continue
		}

		if result != nil && result.PermanentFailure {
			if err := s.store.MarkDeliveryFailed(ctx, job.ID, false, "permanent provider failure", s.now().UTC()); err != nil {
				return err
			}
			if result.DisableDevice && job.UserDeviceID != nil {
				if err := s.store.DisableDevice(ctx, *job.UserDeviceID, s.now().UTC()); err != nil {
					return err
				}
			}
			continue
		}

		providerMessageID := ""
		if result != nil {
			providerMessageID = result.ProviderMessageID
		}
		if err := s.store.MarkDeliverySent(ctx, job.ID, providerMessageID, s.now().UTC()); err != nil {
			return err
		}
	}
	return nil
}

func RunWorker(ctx context.Context, logger *log.Logger, service *Service, pollInterval time.Duration, batchSize int) {
	if service == nil || service.provider == nil {
		return
	}
	ticker := time.NewTicker(pollInterval)
	defer ticker.Stop()

	for {
		if err := service.ProcessPendingDeliveries(ctx, batchSize); err != nil && logger != nil {
			logger.Printf("notifications worker: %v", err)
		}

		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
	}
}
