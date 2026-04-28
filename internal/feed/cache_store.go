package feed

import (
	"context"
	"strconv"
	"time"

	"github.com/google/uuid"
	appcache "github.com/project_radeon/api/pkg/cache"
)

const (
	feedTTL          = 30 * time.Second
	userPostsTTL     = 60 * time.Second
	discoverGlobalID = "discover_global"
)

type cachedStore struct {
	inner Querier
	cache appcache.Store
}

type postAuthorLookup interface {
	GetPostAuthorID(ctx context.Context, postID uuid.UUID) (uuid.UUID, error)
}

func NewCachedStore(inner Querier, store appcache.Store) Querier {
	if store == nil {
		store = appcache.NewDisabled()
	}
	return &cachedStore{inner: inner, cache: store}
}

func (s *cachedStore) ListHomeFeed(ctx context.Context, viewerID uuid.UUID, before *time.Time, limit int) ([]FeedItem, error) {
	globalVersion, err := s.cache.GetVersion(ctx, s.feedVersionKey())
	if err != nil {
		return s.inner.ListHomeFeed(ctx, viewerID, before, limit)
	}
	viewerVersion, err := s.cache.GetVersion(ctx, s.viewerFeedVersionKey(viewerID))
	if err != nil {
		return s.inner.ListHomeFeed(ctx, viewerID, before, limit)
	}

	key := s.cache.Key(
		"feed",
		"home",
		"global_v", strconv.FormatInt(globalVersion, 10),
		"viewer_v", strconv.FormatInt(viewerVersion, 10),
		"viewer", viewerID.String(),
		"before", timePart(before),
		"limit", strconv.Itoa(limit),
	)

	var items []FeedItem
	if err := s.cache.ReadThrough(ctx, key, feedTTL, &items, func(ctx context.Context, dest any) error {
		loaded, err := s.inner.ListHomeFeed(ctx, viewerID, before, limit)
		if err != nil {
			return err
		}
		*dest.(*[]FeedItem) = loaded
		return nil
	}); err != nil {
		return nil, err
	}

	return items, nil
}

func (s *cachedStore) ListHiddenFeedItems(ctx context.Context, userID uuid.UUID, before *time.Time, limit int) ([]HiddenFeedItem, error) {
	return s.inner.ListHiddenFeedItems(ctx, userID, before, limit)
}

func (s *cachedStore) ListUserPosts(ctx context.Context, userID uuid.UUID, before *time.Time, limit int) ([]Post, error) {
	version, err := s.cache.GetVersion(ctx, s.userPostsVersionKey(userID))
	if err != nil {
		return s.inner.ListUserPosts(ctx, userID, before, limit)
	}

	key := s.cache.Key(
		"user_posts",
		"user", userID.String(),
		"v", strconv.FormatInt(version, 10),
		"before", timePart(before),
		"limit", strconv.Itoa(limit),
	)

	var posts []Post
	if err := s.cache.ReadThrough(ctx, key, userPostsTTL, &posts, func(ctx context.Context, dest any) error {
		loaded, err := s.inner.ListUserPosts(ctx, userID, before, limit)
		if err != nil {
			return err
		}
		*dest.(*[]Post) = loaded
		return nil
	}); err != nil {
		return nil, err
	}

	return posts, nil
}

func (s *cachedStore) CreatePost(ctx context.Context, userID uuid.UUID, body string, images []PostImage) (uuid.UUID, error) {
	postID, err := s.inner.CreatePost(ctx, userID, body, images)
	if err != nil {
		return uuid.Nil, err
	}

	_ = s.cache.BumpVersions(ctx,
		s.feedVersionKey(),
		s.userPostsVersionKey(userID),
		s.discoverGlobalVersionKey(),
	)
	return postID, nil
}

func (s *cachedStore) SharePost(ctx context.Context, userID, postID uuid.UUID, commentary string) (uuid.UUID, error) {
	shareID, err := s.inner.SharePost(ctx, userID, postID, commentary)
	if err != nil {
		return uuid.Nil, err
	}

	_ = s.cache.BumpVersions(ctx,
		s.feedVersionKey(),
		s.userPostsVersionKey(userID),
		s.discoverGlobalVersionKey(),
	)
	return shareID, nil
}

func (s *cachedStore) DeletePost(ctx context.Context, postID, userID uuid.UUID) error {
	if err := s.inner.DeletePost(ctx, postID, userID); err != nil {
		return err
	}

	_ = s.cache.BumpVersions(ctx,
		s.feedVersionKey(),
		s.userPostsVersionKey(userID),
		s.discoverGlobalVersionKey(),
	)
	return nil
}

func (s *cachedStore) ListReactions(ctx context.Context, postID uuid.UUID, limit, offset int) ([]Reaction, error) {
	return s.inner.ListReactions(ctx, postID, limit, offset)
}

func (s *cachedStore) ToggleFeedItemReaction(ctx context.Context, itemID, userID uuid.UUID, itemKind FeedItemKind, reactionType string) (bool, error) {
	reacted, err := s.inner.ToggleFeedItemReaction(ctx, itemID, userID, itemKind, reactionType)
	if err != nil {
		return false, err
	}
	if itemKind == FeedItemKindPost {
		s.bumpFeedVersionsForPost(ctx, itemID)
		return reacted, nil
	}
	_ = s.cache.BumpVersions(ctx, s.feedVersionKey())
	return reacted, nil
}

func (s *cachedStore) HideFeedItem(ctx context.Context, userID, itemID uuid.UUID, itemKind FeedItemKind) error {
	if err := s.inner.HideFeedItem(ctx, userID, itemID, itemKind); err != nil {
		return err
	}
	_ = s.cache.BumpVersions(ctx, s.viewerFeedVersionKey(userID))
	return nil
}

func (s *cachedStore) UnhideFeedItem(ctx context.Context, userID, itemID uuid.UUID, itemKind FeedItemKind) error {
	if err := s.inner.UnhideFeedItem(ctx, userID, itemID, itemKind); err != nil {
		return err
	}
	_ = s.cache.BumpVersions(ctx, s.viewerFeedVersionKey(userID))
	return nil
}

func (s *cachedStore) MuteFeedAuthor(ctx context.Context, userID, authorID uuid.UUID) error {
	if err := s.inner.MuteFeedAuthor(ctx, userID, authorID); err != nil {
		return err
	}
	_ = s.cache.BumpVersions(ctx, s.viewerFeedVersionKey(userID))
	return nil
}

func (s *cachedStore) LogFeedImpressions(ctx context.Context, userID uuid.UUID, impressions []FeedImpressionInput) error {
	return s.inner.LogFeedImpressions(ctx, userID, impressions)
}

func (s *cachedStore) LogFeedEvents(ctx context.Context, userID uuid.UUID, events []FeedEventInput) error {
	return s.inner.LogFeedEvents(ctx, userID, events)
}

func (s *cachedStore) ToggleReaction(ctx context.Context, postID, userID uuid.UUID, reactionType string) (bool, error) {
	reacted, err := s.inner.ToggleReaction(ctx, postID, userID, reactionType)
	if err != nil {
		return false, err
	}

	s.bumpFeedVersionsForPost(ctx, postID)
	return reacted, nil
}

func (s *cachedStore) ResolveMentionUsers(ctx context.Context, userIDs []uuid.UUID) ([]MentionedUser, error) {
	return s.inner.ResolveMentionUsers(ctx, userIDs)
}

func (s *cachedStore) AddFeedItemComment(ctx context.Context, itemID, userID uuid.UUID, itemKind FeedItemKind, body string, mentions []CommentMention) (*Comment, error) {
	comment, err := s.inner.AddFeedItemComment(ctx, itemID, userID, itemKind, body, mentions)
	if err != nil {
		return nil, err
	}
	if itemKind == FeedItemKindPost {
		s.bumpFeedVersionsForPost(ctx, itemID)
		return comment, nil
	}
	_ = s.cache.BumpVersions(ctx, s.feedVersionKey())
	return comment, nil
}

func (s *cachedStore) AddComment(ctx context.Context, postID, userID uuid.UUID, body string, mentions []CommentMention) (*Comment, error) {
	comment, err := s.inner.AddComment(ctx, postID, userID, body, mentions)
	if err != nil {
		return nil, err
	}

	s.bumpFeedVersionsForPost(ctx, postID)
	return comment, nil
}

func (s *cachedStore) ListComments(ctx context.Context, postID uuid.UUID, after *time.Time, limit int) ([]Comment, error) {
	return s.inner.ListComments(ctx, postID, after, limit)
}

func (s *cachedStore) ListFeedItemComments(ctx context.Context, itemID uuid.UUID, itemKind FeedItemKind, after *time.Time, limit int) ([]Comment, error) {
	return s.inner.ListFeedItemComments(ctx, itemID, itemKind, after, limit)
}

func (s *cachedStore) bumpFeedVersionsForPost(ctx context.Context, postID uuid.UUID) {
	keys := []string{s.feedVersionKey()}
	if authorID, ok := s.lookupPostAuthorID(ctx, postID); ok {
		keys = append(keys, s.userPostsVersionKey(authorID))
	}
	_ = s.cache.BumpVersions(ctx, keys...)
}

func (s *cachedStore) lookupPostAuthorID(ctx context.Context, postID uuid.UUID) (uuid.UUID, bool) {
	lookup, ok := s.inner.(postAuthorLookup)
	if !ok {
		return uuid.Nil, false
	}

	authorID, err := lookup.GetPostAuthorID(ctx, postID)
	if err != nil {
		return uuid.Nil, false
	}
	return authorID, true
}

func (s *cachedStore) feedVersionKey() string {
	return s.cache.Key("ver", "feed")
}

func (s *cachedStore) viewerFeedVersionKey(userID uuid.UUID) string {
	return s.cache.Key("ver", "discover", "viewer", userID.String())
}

func (s *cachedStore) userPostsVersionKey(userID uuid.UUID) string {
	return s.cache.Key("ver", "user_posts", userID.String())
}

func (s *cachedStore) discoverGlobalVersionKey() string {
	return s.cache.Key("ver", "discover", discoverGlobalID)
}

func timePart(value *time.Time) string {
	if value == nil {
		return "none"
	}
	return value.UTC().Format(time.RFC3339Nano)
}
