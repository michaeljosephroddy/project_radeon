# project_radeon

Go REST API for a sober social network and people discovery app.

## Stack
- Go + chi router
- PostgreSQL via pgx (raw SQL, no ORM)
- JWT (HS256) auth
- AWS S3 for avatar storage

## Running locally
- Create a `.env` file with the values listed below
- `make migrate` to apply DB schema
- `make run` to start the server

## .env values
- `PORT` — server port (e.g. `8080`)
- `ENV` — environment name (e.g. `development`)
- `DATABASE_URL` — postgres connection string
- `JWT_SECRET` — signing secret, make it long and random
- `JWT_EXPIRY_HOURS` — token lifetime in hours (e.g. `168`)
- `AWS_REGION` — S3 bucket region
- `AWS_S3_BUCKET` — bucket name for avatars
- `AWS_ACCESS_KEY_ID` — AWS credentials
- `AWS_SECRET_ACCESS_KEY` — AWS credentials

## Domains
- `auth` — register, login
- `user` — profile, avatar, discovery
- `feed` — posts, comments, reactions
- `connections` — friend requests
- `events` — create, RSVP, attendees
- `messages` — conversations, message requests
- `interests` — seeded list, user preferences
- `discovery` — suggestions, likes, dismissals

## Other
- All protected routes require `Authorization: Bearer <token>`
- UUID primary keys throughout
- New domains go in `internal/<domain>/`, routes wired in `cmd/api/main.go`
- New migrations go in `migrations/` as `002_...sql`, `003_...sql` etc — never edit applied ones
