package feed

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/project_radeon/api/internal/cachetest"
)

type stubQuerier struct {
	listUserPostsCalls int
	sharePostCalls     int
}

func (s *stubQuerier) ListHomeFeed(context.Context, uuid.UUID, *time.Time, int) ([]FeedItem, error) {
	return nil, nil
}

func (s *stubQuerier) ListHiddenFeedItems(context.Context, uuid.UUID, *time.Time, int) ([]HiddenFeedItem, error) {
	return nil, nil
}

func (s *stubQuerier) ListUserPosts(_ context.Context, userID uuid.UUID, before *time.Time, limit int) ([]Post, error) {
	s.listUserPostsCalls++
	return []Post{{
		ID:        uuid.New(),
		UserID:    userID,
		Username:  "author",
		CreatedAt: time.Unix(int64(s.listUserPostsCalls), 0).UTC(),
	}}, nil
}

func (s *stubQuerier) CreatePost(context.Context, uuid.UUID, string, []PostImage) (uuid.UUID, error) {
	return uuid.New(), nil
}

func (s *stubQuerier) SharePost(context.Context, uuid.UUID, uuid.UUID, string) (uuid.UUID, error) {
	s.sharePostCalls++
	return uuid.New(), nil
}

func (s *stubQuerier) DeletePost(context.Context, uuid.UUID, uuid.UUID) error {
	return nil
}

func (s *stubQuerier) HideFeedItem(context.Context, uuid.UUID, uuid.UUID, FeedItemKind) error {
	return nil
}

func (s *stubQuerier) UnhideFeedItem(context.Context, uuid.UUID, uuid.UUID, FeedItemKind) error {
	return nil
}

func (s *stubQuerier) MuteFeedAuthor(context.Context, uuid.UUID, uuid.UUID) error {
	return nil
}

func (s *stubQuerier) LogFeedImpressions(context.Context, uuid.UUID, []FeedImpressionInput) error {
	return nil
}

func (s *stubQuerier) LogFeedEvents(context.Context, uuid.UUID, []FeedEventInput) error {
	return nil
}

func (s *stubQuerier) ListReactions(context.Context, uuid.UUID, int, int) ([]Reaction, error) {
	return nil, nil
}

func (s *stubQuerier) ToggleFeedItemReaction(context.Context, uuid.UUID, uuid.UUID, FeedItemKind, string) (bool, error) {
	return true, nil
}

func (s *stubQuerier) ToggleReaction(context.Context, uuid.UUID, uuid.UUID, string) (bool, error) {
	return true, nil
}

func (s *stubQuerier) ResolveMentionUsers(context.Context, []uuid.UUID) ([]MentionedUser, error) {
	return nil, nil
}

func (s *stubQuerier) AddComment(context.Context, uuid.UUID, uuid.UUID, string, []CommentMention) (*Comment, error) {
	return &Comment{}, nil
}

func (s *stubQuerier) AddFeedItemComment(context.Context, uuid.UUID, uuid.UUID, FeedItemKind, string, []CommentMention) (*Comment, error) {
	return &Comment{}, nil
}

func (s *stubQuerier) ListComments(context.Context, uuid.UUID, *time.Time, int) ([]Comment, error) {
	return nil, nil
}

func (s *stubQuerier) ListFeedItemComments(context.Context, uuid.UUID, FeedItemKind, *time.Time, int) ([]Comment, error) {
	return nil, nil
}

func (s *stubQuerier) GetPostAuthorID(_ context.Context, _ uuid.UUID) (uuid.UUID, error) {
	return authorID, nil
}

var authorID = uuid.New()

func TestCachedStoreInvalidatesUserPostsAfterReaction(t *testing.T) {
	t.Parallel()

	inner := &stubQuerier{}
	store := cachetest.NewStore()
	cached := NewCachedStore(inner, store)

	first, err := cached.ListUserPosts(context.Background(), authorID, nil, 20)
	if err != nil {
		t.Fatalf("first ListUserPosts: %v", err)
	}
	second, err := cached.ListUserPosts(context.Background(), authorID, nil, 20)
	if err != nil {
		t.Fatalf("second ListUserPosts: %v", err)
	}
	if inner.listUserPostsCalls != 1 {
		t.Fatalf("expected one underlying ListUserPosts call after cache hit, got %d", inner.listUserPostsCalls)
	}
	if !first[0].CreatedAt.Equal(second[0].CreatedAt) {
		t.Fatalf("expected cached posts to be identical before invalidation")
	}

	if _, err := cached.ToggleReaction(context.Background(), uuid.New(), uuid.New(), "like"); err != nil {
		t.Fatalf("ToggleReaction: %v", err)
	}

	third, err := cached.ListUserPosts(context.Background(), authorID, nil, 20)
	if err != nil {
		t.Fatalf("third ListUserPosts: %v", err)
	}
	if inner.listUserPostsCalls != 2 {
		t.Fatalf("expected invalidation to force a fresh ListUserPosts call, got %d", inner.listUserPostsCalls)
	}
	if !third[0].CreatedAt.After(second[0].CreatedAt) {
		t.Fatalf("expected invalidated posts to be newer than cached posts")
	}
}

func TestCachedStoreInvalidatesUserPostsAfterShare(t *testing.T) {
	t.Parallel()

	inner := &stubQuerier{}
	store := cachetest.NewStore()
	cached := NewCachedStore(inner, store)

	first, err := cached.ListUserPosts(context.Background(), authorID, nil, 20)
	if err != nil {
		t.Fatalf("first ListUserPosts: %v", err)
	}
	if _, err := cached.SharePost(context.Background(), authorID, uuid.New(), "reshared"); err != nil {
		t.Fatalf("SharePost: %v", err)
	}
	second, err := cached.ListUserPosts(context.Background(), authorID, nil, 20)
	if err != nil {
		t.Fatalf("second ListUserPosts: %v", err)
	}
	if inner.sharePostCalls != 1 {
		t.Fatalf("expected SharePost to hit inner store once, got %d", inner.sharePostCalls)
	}
	if !second[0].CreatedAt.After(first[0].CreatedAt) {
		t.Fatalf("expected share invalidation to force a fresh ListUserPosts call")
	}
}
