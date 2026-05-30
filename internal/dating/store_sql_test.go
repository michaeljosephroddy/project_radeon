package dating

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

func TestDatingCandidateDistanceAliasIsUnambiguous(t *testing.T) {
	if !strings.Contains(datingCandidateColumns, " AS candidate_distance_km,") {
		t.Fatal("expected computed dating candidate distance to use candidate_distance_km alias")
	}
	if strings.Contains(datingCandidateColumns, " AS distance_km,") {
		t.Fatal("computed dating candidate distance must not reuse profile distance_km alias")
	}
}

func TestDatingDiscoveryDoesNotExcludeAcceptedSocialFriends(t *testing.T) {
	if strings.Contains(datingDiscoverWhereSQL, "friendships") {
		t.Fatal("dating discovery should not exclude accepted social friends")
	}
}

func TestDatingLikesDoesNotExcludeAcceptedSocialFriends(t *testing.T) {
	if strings.Contains(datingLikesWhereSQL, "friendships") {
		t.Fatal("dating likes should not exclude accepted social friends")
	}
}

func TestValidateDatingPairAllowsEligibleUsers(t *testing.T) {
	userID := uuid.MustParse("11111111-1111-1111-1111-111111111111")
	targetID := uuid.MustParse("22222222-2222-2222-2222-222222222222")
	q := staticQuerier{row: staticRow{values: []any{
		true, true, true, true, false, false, true, false,
	}}}

	if err := validateDatingPair(context.Background(), q, userID, targetID); err != nil {
		t.Fatalf("validateDatingPair returned %v, want nil", err)
	}
}

func TestValidateDatingPairBlocksBlockedUsers(t *testing.T) {
	userID := uuid.MustParse("11111111-1111-1111-1111-111111111111")
	targetID := uuid.MustParse("22222222-2222-2222-2222-222222222222")
	q := staticQuerier{row: staticRow{values: []any{
		true, true, true, true, false, false, true, true,
	}}}

	err := validateDatingPair(context.Background(), q, userID, targetID)
	if !errors.Is(err, ErrForbidden) {
		t.Fatalf("validateDatingPair returned %v, want ErrForbidden", err)
	}
}

type staticQuerier struct {
	row staticRow
}

func (q staticQuerier) Query(context.Context, string, ...any) (pgx.Rows, error) {
	return nil, errors.New("unexpected query")
}

func (q staticQuerier) QueryRow(context.Context, string, ...any) pgx.Row {
	return q.row
}

func (q staticQuerier) Exec(context.Context, string, ...any) (pgconn.CommandTag, error) {
	return pgconn.CommandTag{}, errors.New("unexpected exec")
}

type staticRow struct {
	values []any
}

func (r staticRow) Scan(dest ...any) error {
	if len(dest) != len(r.values) {
		return errors.New("scan destination count mismatch")
	}
	for index, value := range r.values {
		switch target := dest[index].(type) {
		case *bool:
			typed, ok := value.(bool)
			if !ok {
				return errors.New("expected bool scan value")
			}
			*target = typed
		default:
			return errors.New("unsupported scan destination")
		}
	}
	return nil
}
