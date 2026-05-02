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

func (s *Service) GetSummary(ctx context.Context, userID uuid.UUID) (*NotificationSummary, error) {
	return s.store.GetSummary(ctx, userID)
}

func (s *Service) MarkNotificationRead(ctx context.Context, userID, notificationID uuid.UUID) error {
	return s.store.MarkNotificationRead(ctx, userID, notificationID, s.now().UTC())
}

func (s *Service) MarkNotificationsRead(ctx context.Context, userID uuid.UUID, notificationIDs []uuid.UUID) (*BulkReadResult, error) {
	updated, err := s.store.MarkNotificationsRead(ctx, userID, notificationIDs, s.now().UTC())
	if err != nil {
		return nil, err
	}
	return &BulkReadResult{Read: true, Updated: updated}, nil
}

func (s *Service) MarkAllNotificationsRead(ctx context.Context, userID uuid.UUID) (*BulkReadResult, error) {
	updated, err := s.store.MarkAllNotificationsRead(ctx, userID, s.now().UTC())
	if err != nil {
		return nil, err
	}
	return &BulkReadResult{Read: true, Updated: updated}, nil
}

func (s *Service) MarkChatRead(ctx context.Context, chatID, userID uuid.UUID, lastReadMessageID *uuid.UUID, readAt time.Time) error {
	return s.store.MarkChatRead(ctx, chatID, userID, lastReadMessageID, readAt.UTC())
}

func (s *Service) NotifyChatMessage(ctx context.Context, chatID, messageID, senderID uuid.UUID, body string) error {
	go func() {
		notifyCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		if err := s.store.CreateChatMessageNotifications(notifyCtx, chatID, messageID, senderID, body); err != nil {
			log.Printf("create chat message notifications: %v", err)
		}
	}()
	return nil
}

func (s *Service) NotifyCommentMentions(ctx context.Context, postID, commentID, authorID uuid.UUID, mentionedUserIDs []uuid.UUID, body string) error {
	return s.store.CreateCommentMentionNotifications(ctx, postID, commentID, authorID, mentionedUserIDs, body)
}

func (s *Service) NotifyGroupJoinRequest(ctx context.Context, groupID, requesterID uuid.UUID) error {
	go s.runGroupNotification("create group join request notifications", func(ctx context.Context) error {
		return s.store.CreateGroupJoinRequestNotifications(ctx, groupID, requesterID)
	})
	return nil
}

func (s *Service) NotifyGroupJoinApproved(ctx context.Context, groupID, reviewerID, approvedUserID uuid.UUID) error {
	go s.runGroupNotification("create group join approved notification", func(ctx context.Context) error {
		return s.store.CreateGroupJoinApprovedNotification(ctx, groupID, reviewerID, approvedUserID)
	})
	return nil
}

func (s *Service) NotifyGroupPost(ctx context.Context, groupID, postID, authorID uuid.UUID, postType, body string) error {
	go s.runGroupNotification("create group post notifications", func(ctx context.Context) error {
		return s.store.CreateGroupPostNotifications(ctx, groupID, postID, authorID, postType, body)
	})
	return nil
}

func (s *Service) NotifyGroupComment(ctx context.Context, groupID, postID, commentID, authorID uuid.UUID, body string) error {
	go s.runGroupNotification("create group comment notifications", func(ctx context.Context) error {
		return s.store.CreateGroupCommentNotifications(ctx, groupID, postID, commentID, authorID, body)
	})
	return nil
}

func (s *Service) NotifyGroupAdminContact(ctx context.Context, groupID, threadID, senderID uuid.UUID, body string) error {
	go s.runGroupNotification("create group admin contact notifications", func(ctx context.Context) error {
		return s.store.CreateGroupAdminContactNotifications(ctx, groupID, threadID, senderID, body)
	})
	return nil
}

func (s *Service) NotifyGroupAdminReply(ctx context.Context, groupID, threadID, messageID, senderID uuid.UUID, body string) error {
	go s.runGroupNotification("create group admin reply notification", func(ctx context.Context) error {
		return s.store.CreateGroupAdminReplyNotification(ctx, groupID, threadID, messageID, senderID, body)
	})
	return nil
}

func (s *Service) NotifyGroupReport(ctx context.Context, groupID, reportID, reporterID uuid.UUID, targetType, reason string) error {
	go s.runGroupNotification("create group report notifications", func(ctx context.Context) error {
		return s.store.CreateGroupReportNotifications(ctx, groupID, reportID, reporterID, targetType, reason)
	})
	return nil
}

func (s *Service) runGroupNotification(label string, create func(context.Context) error) {
	notifyCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := create(notifyCtx); err != nil {
		log.Printf("%s: %v", label, err)
	}
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
