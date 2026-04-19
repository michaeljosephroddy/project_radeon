package meetups

import (
	"context"
	"errors"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

var ErrNotFound = errors.New("not found")

type pgStore struct {
	pool *pgxpool.Pool
}

// NewPgStore wraps a pgxpool.Pool as the production Querier implementation.
func NewPgStore(pool *pgxpool.Pool) Querier {
	return &pgStore{pool: pool}
}

func (s *pgStore) ListMeetups(ctx context.Context, userID uuid.UUID, cityFilter, queryFilter string, limit, offset int) ([]Meetup, error) {
	rows, err := s.pool.Query(ctx,
		`SELECT
			m.id,
			m.organiser_id,
			m.title,
			m.description,
			m.city,
			m.starts_at,
			m.capacity,
			m.attendee_count,
			EXISTS(
				SELECT 1 FROM meetup_attendees
				WHERE meetup_id = m.id AND user_id = $1
			) AS is_attending
		FROM meetups m
		WHERE m.starts_at > NOW()
			AND ($2 = '' OR m.city ILIKE $2)
			AND (
				$3 = ''
				OR m.title ILIKE $3
				OR COALESCE(m.description, '') ILIKE $3
			)
		ORDER BY m.starts_at ASC
		LIMIT $4 OFFSET $5`,
		userID, cityFilter, queryFilter, limit, offset,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return scanMeetups(rows)
}

func (s *pgStore) ListMyMeetups(ctx context.Context, userID uuid.UUID, limit, offset int) ([]Meetup, error) {
	rows, err := s.pool.Query(ctx,
		`SELECT
			m.id,
			m.organiser_id,
			m.title,
			m.description,
			m.city,
			m.starts_at,
			m.capacity,
			m.attendee_count,
			EXISTS(
				SELECT 1 FROM meetup_attendees
				WHERE meetup_id = m.id AND user_id = $1
			) AS is_attending
		FROM meetups m
		WHERE m.organiser_id = $1
		ORDER BY m.starts_at ASC
		LIMIT $2 OFFSET $3`,
		userID, limit, offset,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return scanMeetups(rows)
}

func (s *pgStore) AttachAttendeePreviews(ctx context.Context, meetups []Meetup, previewLimit int) error {
	if len(meetups) == 0 {
		return nil
	}

	meetupIDs := make([]uuid.UUID, 0, len(meetups))
	meetupIndex := make(map[uuid.UUID]*Meetup, len(meetups))
	for i := range meetups {
		meetupIDs = append(meetupIDs, meetups[i].ID)
		meetupIndex[meetups[i].ID] = &meetups[i]
	}

	rows, err := s.pool.Query(ctx,
		`SELECT meetup_id, id, username, avatar_url
		FROM (
			SELECT
				ma.meetup_id,
				u.id,
				u.username,
				u.avatar_url,
				ROW_NUMBER() OVER (PARTITION BY ma.meetup_id ORDER BY ma.rsvp_at ASC) AS rn
			FROM meetup_attendees ma
			JOIN users u ON u.id = ma.user_id
			WHERE ma.meetup_id = ANY($1)
		) attendee_preview
		WHERE rn <= $2
		ORDER BY meetup_id, rn`,
		meetupIDs, previewLimit,
	)
	if err != nil {
		return err
	}
	defer rows.Close()

	for rows.Next() {
		var meetupID uuid.UUID
		var attendee AttendeePreview
		if err := rows.Scan(&meetupID, &attendee.ID, &attendee.Username, &attendee.AvatarURL); err != nil {
			return err
		}
		if m := meetupIndex[meetupID]; m != nil {
			m.Attendees = append(m.Attendees, attendee)
		}
	}
	return rows.Err()
}

func (s *pgStore) GetMeetup(ctx context.Context, meetupID, userID uuid.UUID) (*Meetup, error) {
	var m Meetup
	err := s.pool.QueryRow(ctx,
		`SELECT
			m.id,
			m.organiser_id,
			m.title,
			m.description,
			m.city,
			m.starts_at,
			m.capacity,
			m.attendee_count,
			EXISTS(
				SELECT 1 FROM meetup_attendees
				WHERE meetup_id = m.id AND user_id = $2
			) AS is_attending
		FROM meetups m
		WHERE m.id = $1`,
		meetupID, userID,
	).Scan(&m.ID, &m.OrganizerID, &m.Title, &m.Description, &m.City, &m.StartsAt, &m.Capacity, &m.AttendeeCt, &m.IsAttending)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	return &m, nil
}

func (s *pgStore) CreateMeetup(ctx context.Context, userID uuid.UUID, title string, description *string, city string, startsAt time.Time, capacity *int) (*Meetup, error) {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback(ctx)

	var m Meetup
	if err := tx.QueryRow(ctx,
		`INSERT INTO meetups (
			organiser_id,
			title,
			description,
			city,
			starts_at,
			capacity
		)
		VALUES ($1, $2, $3, $4, $5, $6)
		RETURNING
			id,
			organiser_id,
			title,
			description,
			city,
			starts_at,
			capacity`,
		userID, title, description, city, startsAt, capacity,
	).Scan(&m.ID, &m.OrganizerID, &m.Title, &m.Description, &m.City, &m.StartsAt, &m.Capacity); err != nil {
		return nil, err
	}

	if _, err := tx.Exec(ctx,
		`INSERT INTO meetup_attendees (meetup_id, user_id) VALUES ($1, $2)`,
		m.ID, userID,
	); err != nil {
		return nil, err
	}

	if _, err := tx.Exec(ctx,
		`UPDATE meetups SET attendee_count = 1 WHERE id = $1`,
		m.ID,
	); err != nil {
		return nil, err
	}

	if err := tx.Commit(ctx); err != nil {
		return nil, err
	}

	m.AttendeeCt = 1
	m.IsAttending = true
	return &m, nil
}

func (s *pgStore) GetAttendees(ctx context.Context, meetupID uuid.UUID, limit, offset int) ([]Attendee, error) {
	rows, err := s.pool.Query(ctx,
		`SELECT
			u.id,
			u.username,
			u.avatar_url,
			u.city,
			ma.rsvp_at
		FROM meetup_attendees ma
		JOIN users u ON u.id = ma.user_id
		WHERE ma.meetup_id = $1
		ORDER BY ma.rsvp_at ASC
		LIMIT $2 OFFSET $3`,
		meetupID, limit, offset,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var attendees []Attendee
	for rows.Next() {
		var a Attendee
		if err := rows.Scan(&a.ID, &a.Username, &a.AvatarURL, &a.City, &a.RSVPAt); err != nil {
			return nil, err
		}
		attendees = append(attendees, a)
	}
	return attendees, rows.Err()
}

func (s *pgStore) GetMeetupCapacity(ctx context.Context, meetupID uuid.UUID) (*int, int, error) {
	var capacity *int
	var attendeeCount int
	err := s.pool.QueryRow(ctx,
		`SELECT capacity, attendee_count FROM meetups WHERE id = $1`,
		meetupID,
	).Scan(&capacity, &attendeeCount)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, 0, ErrNotFound
	}
	return capacity, attendeeCount, err
}

func (s *pgStore) IsRSVPd(ctx context.Context, meetupID, userID uuid.UUID) (bool, error) {
	var exists bool
	err := s.pool.QueryRow(ctx,
		`SELECT EXISTS(
			SELECT 1 FROM meetup_attendees
			WHERE meetup_id = $1 AND user_id = $2
		)`,
		meetupID, userID,
	).Scan(&exists)
	return exists, err
}

func (s *pgStore) AddRSVP(ctx context.Context, meetupID, userID uuid.UUID) error {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)

	if _, err := tx.Exec(ctx,
		`INSERT INTO meetup_attendees (meetup_id, user_id) VALUES ($1, $2)`,
		meetupID, userID,
	); err != nil {
		return err
	}
	if _, err := tx.Exec(ctx,
		`UPDATE meetups SET attendee_count = attendee_count + 1 WHERE id = $1`,
		meetupID,
	); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

func (s *pgStore) RemoveRSVP(ctx context.Context, meetupID, userID uuid.UUID) error {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)

	if _, err := tx.Exec(ctx,
		`DELETE FROM meetup_attendees WHERE meetup_id = $1 AND user_id = $2`,
		meetupID, userID,
	); err != nil {
		return err
	}
	if _, err := tx.Exec(ctx,
		`UPDATE meetups SET attendee_count = GREATEST(attendee_count - 1, 0) WHERE id = $1`,
		meetupID,
	); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

func scanMeetups(rows interface {
	Next() bool
	Scan(...any) error
	Err() error
}) ([]Meetup, error) {
	var meetups []Meetup
	for rows.Next() {
		var m Meetup
		if err := rows.Scan(&m.ID, &m.OrganizerID, &m.Title, &m.Description, &m.City, &m.StartsAt, &m.Capacity, &m.AttendeeCt, &m.IsAttending); err != nil {
			return nil, err
		}
		meetups = append(meetups, m)
	}
	return meetups, rows.Err()
}
