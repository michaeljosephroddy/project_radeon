# Backend Changes — project_radeon

The following features have been removed from the project_radeon backend. They were part of a dating/match feature that has been moved to a separate app (project_spark). This document is to inform frontend changes needed in the project_radeon React Native app.

---

## Removed: Connections

Connections were a friend-request style system where users could send, accept, or decline connection requests. On a mutual like, a connection of type `MATCH` was automatically created. This entire system has been removed.

**Endpoints removed:**
- `POST /connections` — send a connection request
- `GET /connections` — list accepted connections
- `GET /connections/pending` — list incoming pending requests
- `PATCH /connections/{id}` — accept or decline a request
- `DELETE /connections/{id}` — remove a connection

**Replaced by:** a follow system (see below).

---

## Removed: Interests

Users could select interests from a fixed list. These were used to power the discovery/matching algorithm. The interests system has been removed entirely from project_radeon.

**Endpoints removed:**
- `GET /interests` — list all available interests
- `PUT /users/me/interests` — set the current user's interests

---

## Removed: Likes

Users could like other users' profiles. If two users liked each other it was considered a mutual match and a connection of type `MATCH` was created. Users could also view who had liked them.

**Endpoints removed:**
- `POST /users/{id}/like` — like a user
- `GET /users/me/likes` — get list of users who have liked the current user

---

## Removed: Discovery / Suggestions

A people discovery system that used interest vectors and location data to score and rank other users as potential matches. Users could also dismiss suggestions so they wouldn't appear again.

**Endpoints removed:**
- `GET /users/suggestions` — get ranked list of suggested users based on interests and location
- `POST /users/{id}/dismiss` — dismiss a user from suggestions

---

## Removed: Location and Discovery Fields on User

User profiles no longer include location coordinates or discovery radius. These were only used by the matching/discovery algorithm.

**Fields removed from the user object:**
- `lat` — latitude
- `lng` — longitude
- `discovery_radius_km` — how far away to look for matches
- `avatar_url_blurred` — a heavily blurred version of the avatar, used to obscure profiles until a mutual like occurred

**`PATCH /users/me` no longer accepts:**
- `lat`
- `lng`
- `discovery_radius_km`

**`POST /users/me/avatar` no longer returns:**
- `avatar_url_blurred`

---

## Added: Follows

Connections have been replaced by a follow system. Users can follow each other without requiring mutual acceptance.

**New endpoints:**
- `POST /users/{id}/follow` — follow a user
- `DELETE /users/{id}/follow` — unfollow a user
- `GET /users/me/following` — list users the current user follows
- `GET /users/me/followers` — list users who follow the current user

---

## Changed: Feed

The feed (`GET /feed`) previously only showed posts from users the current user was connected to. It now shows posts from everyone on the platform, ordered by most recent. A personalised algorithm will be introduced later.

---

## Added: User Posts

A new endpoint to fetch all posts by a specific user.

**New endpoint:**
- `GET /users/{id}/posts` — returns all posts by a given user, supports `?page=` and `?limit=` pagination
