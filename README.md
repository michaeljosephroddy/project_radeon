# project_radeon

Go REST API for a sober social network and people discovery app.

## Stack
- Go + chi router
- PostgreSQL via pgx (raw SQL, no ORM)
- Redis for server-side response caching
- JWT (HS256) auth
- AWS S3 for avatar storage

## Running locally
- Create a `.env` file with the values listed below
- `make migrate` to bootstrap a new DB or apply new tracked migrations
- `make migrate-status` to view applied vs pending migration files
- `make run` to start the server

## .env values
- `PORT` — server port (e.g. `8080`)
- `ENV` — environment name (e.g. `development`)
- `DATABASE_URL` — postgres connection string
- `JWT_SECRET` — signing secret, make it long and random
- `JWT_EXPIRY_HOURS` — token lifetime in hours (e.g. `168`)
- `CACHE_ENABLED` — set to `true` to enable Redis-backed read caching
- `REDIS_ADDR` — Redis host and port (e.g. `localhost:6379`)
- `REDIS_PASSWORD` — Redis password, if required
- `REDIS_DB` — Redis logical database number (e.g. `0`)
- `REDIS_TLS` — set to `true` for TLS-enabled Redis endpoints such as secured ElastiCache deployments
- `REDIS_PREFIX` — Redis key prefix for this app (defaults to `pr`)
- `AWS_REGION` — S3 bucket region
- `AWS_S3_BUCKET` — bucket name for avatars
- `AWS_ACCESS_KEY_ID` — AWS credentials
- `AWS_SECRET_ACCESS_KEY` — AWS credentials

## Redis locally
- Redis can run as a native local service; Docker is not required
- With Redis installed via `apt`, confirm it is available with `redis-cli ping`
- Expect `PONG`

## Domains
- `auth` — register, login
- `user` — profile, avatar, discovery
- `feed` — posts, comments, reactions
- `connections` — friend requests
- `meetups` — create, RSVP, attendees
- `chats` — messages, chat requests
- `interests` — seeded list, user preferences
- `discovery` — suggestions, likes, dismissals

## Other
- All protected routes require `Authorization: Bearer <token>`
- UUID primary keys throughout
- New domains go in `internal/<domain>/`, routes wired in `cmd/api/main.go`
- New migrations go in `migrations/` as `002_...sql`, `003_...sql` etc — never edit applied ones
