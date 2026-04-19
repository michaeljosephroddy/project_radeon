package main

import (
	"context"
	"fmt"
	"log"
	"math/rand"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/joho/godotenv"
	"github.com/project_radeon/api/pkg/database"
	"golang.org/x/crypto/bcrypt"
)

var rng = rand.New(rand.NewSource(42))

func pick[T any](s []T) T { return s[rng.Intn(len(s))] }

func daysAgo(n int) time.Time {
	return time.Now().Add(-time.Duration(n) * 24 * time.Hour)
}

func hoursAgo(n int) time.Time {
	return time.Now().Add(-time.Duration(n) * time.Hour)
}

func main() {
	godotenv.Load()

	pool, err := database.Connect()
	if err != nil {
		log.Fatalf("db connect: %v", err)
	}
	defer pool.Close()

	if err := seed(context.Background(), pool); err != nil {
		log.Fatalf("seed failed: %v", err)
	}

	fmt.Println("\n✓ seed complete")
	fmt.Println("  login: test@radeon.dev / password123")
}

func seed(ctx context.Context, pool *pgxpool.Pool) error {
	hash, err := bcrypt.GenerateFromPassword([]byte("password123"), bcrypt.DefaultCost)
	if err != nil {
		return fmt.Errorf("bcrypt: %w", err)
	}
	passwordHash := string(hash)

	tx, err := pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)

	// ── Wipe existing data ───────────────────────────────────────────────────
	fmt.Println("→ clearing existing data…")
	_, err = tx.Exec(ctx, `
		TRUNCATE messages, chat_members, chats,
			support_responses, support_requests,
			meetup_attendees, meetups,
			post_reactions, comments, posts,
			friendships, users
		RESTART IDENTITY CASCADE
	`)
	if err != nil {
		return fmt.Errorf("truncate: %w", err)
	}

	// ── Users ────────────────────────────────────────────────────────────────
	fmt.Println("→ inserting 100 users…")

	type userRow struct {
		id       uuid.UUID
		username string
		city     string
	}

	locations := []struct{ city, country string }{
		{"London", "United Kingdom"},
		{"Manchester", "United Kingdom"},
		{"Dublin", "Ireland"},
		{"New York", "United States"},
		{"Los Angeles", "United States"},
		{"Berlin", "Germany"},
		{"Sydney", "Australia"},
	}

	firstNames := []string{
		"James", "Sarah", "Michael", "Emma", "David", "Olivia", "Daniel", "Sophie",
		"Ryan", "Chloe", "John", "Jessica", "Tom", "Laura", "Mark", "Amy",
		"Kevin", "Rachel", "Chris", "Hannah", "Brian", "Megan", "Paul", "Claire",
		"Andrew", "Lucy", "Sean", "Natalie", "Patrick", "Emily", "Luke", "Alice",
		"Matthew", "Charlotte", "Aaron", "Grace", "Nathan", "Isabelle", "Adam", "Ella",
	}
	lastNames := []string{
		"Murphy", "Walsh", "O'Brien", "Smith", "Jones", "Williams", "Taylor", "Brown",
		"Wilson", "Evans", "Johnson", "Davis", "Miller", "Anderson", "Moore", "Martin",
		"Thompson", "White", "Harris", "Clark", "Lewis", "Robinson", "Walker", "Young",
		"Allen", "King", "Wright", "Scott", "Green", "Baker", "Adams", "Nelson",
		"Hill", "Ramirez", "Campbell", "Mitchell", "Carter", "Roberts", "Phillips", "Turner",
	}

	allSupportModes := [][]string{
		{"one_on_one", "text"},
		{"one_on_one", "voice"},
		{"group", "text"},
		{"one_on_one", "text", "voice"},
		{"text"},
		{"voice", "one_on_one"},
	}

	users := make([]userRow, 100)

	// User 0: primary test account
	users[0] = userRow{id: uuid.New(), username: "testuser", city: "London"}
	loc0 := locations[0]
	soberSince0 := daysAgo(730)
	supportUpdated0 := daysAgo(30)
	if _, err := tx.Exec(ctx, `
		INSERT INTO users (id, username, first_name, last_name, email, password_hash, city, country, sober_since, created_at, is_available_to_support, support_modes, support_updated_at)
		VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13)`,
		users[0].id, "testuser", "Test", "User", "test@radeon.dev", passwordHash,
		loc0.city, loc0.country, soberSince0, daysAgo(400),
		true, []string{"one_on_one", "text"}, supportUpdated0,
	); err != nil {
		return fmt.Errorf("insert testuser: %w", err)
	}

	// Users 1-99
	for i := 1; i < 100; i++ {
		loc := locations[rng.Intn(len(locations))]
		fn := firstNames[rng.Intn(len(firstNames))]
		ln := lastNames[rng.Intn(len(lastNames))]
		avail := i <= 44
		modes := []string{}
		var supportUpdated *time.Time
		if avail {
			modes = allSupportModes[rng.Intn(len(allSupportModes))]
			t := daysAgo(rng.Intn(90) + 1)
			supportUpdated = &t
		}
		soberSince := daysAgo(rng.Intn(1825) + 30)
		users[i] = userRow{
			id:       uuid.New(),
			username: fmt.Sprintf("user_%02d", i),
			city:     loc.city,
		}
		if _, err := tx.Exec(ctx, `
			INSERT INTO users (id, username, first_name, last_name, email, password_hash, city, country, sober_since, created_at, is_available_to_support, support_modes, support_updated_at)
			VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13)`,
			users[i].id, fmt.Sprintf("user_%02d", i), fn, ln,
			fmt.Sprintf("user_%02d@radeon.dev", i), passwordHash,
			loc.city, loc.country, soberSince, daysAgo(rng.Intn(365)+30),
			avail, modes, supportUpdated,
		); err != nil {
			return fmt.Errorf("insert user %d: %w", i, err)
		}
	}

	// ── Friendships ──────────────────────────────────────────────────────────
	fmt.Println("→ inserting friendships…")

	// Helper: insert with user_a < user_b constraint enforced, explicit requester
	insertFriendship := func(uid1, uid2, requesterID uuid.UUID, status string, createdAt time.Time, acceptedAt *time.Time) error {
		userA, userB := uid1, uid2
		if userA.String() > userB.String() {
			userA, userB = userB, userA
		}
		_, err := tx.Exec(ctx, `
			INSERT INTO friendships (id, user_a_id, user_b_id, requester_id, status, created_at, accepted_at)
			VALUES ($1,$2,$3,$4,$5,$6,$7)
			ON CONFLICT (user_a_id, user_b_id) DO NOTHING`,
			uuid.New(), userA, userB, requesterID, status, createdAt, acceptedAt,
		)
		return err
	}

	// Test user ↔ users 1-20: accepted friendships
	for i := 1; i <= 20; i++ {
		ca := daysAgo(rng.Intn(150) + 30)
		aa := ca.Add(time.Duration(rng.Intn(24)+1) * time.Hour)
		if err := insertFriendship(users[0].id, users[i].id, users[i].id, "accepted", ca, &aa); err != nil {
			return fmt.Errorf("friendship 0↔%d: %w", i, err)
		}
	}

	// Users 21-28 → test user: pending (incoming to test user)
	for i := 21; i <= 28; i++ {
		ca := daysAgo(rng.Intn(10) + 1)
		if err := insertFriendship(users[0].id, users[i].id, users[i].id, "pending", ca, nil); err != nil {
			return fmt.Errorf("friendship in %d→0: %w", i, err)
		}
	}

	// Test user → users 29-32: pending (outgoing from test user)
	for i := 29; i <= 32; i++ {
		ca := daysAgo(rng.Intn(5) + 1)
		if err := insertFriendship(users[0].id, users[i].id, users[0].id, "pending", ca, nil); err != nil {
			return fmt.Errorf("friendship 0→%d: %w", i, err)
		}
	}

	// ~100 accepted friendships among other users
	friendSet := make(map[[2]string]bool)
	for i := 0; i <= 32; i++ {
		a, b := users[0].id.String(), users[i].id.String()
		if a > b {
			a, b = b, a
		}
		friendSet[[2]string{a, b}] = true
	}
	for count := 0; count < 100; {
		i, j := rng.Intn(98)+1, rng.Intn(98)+1
		if i == j {
			continue
		}
		a, b := users[i].id.String(), users[j].id.String()
		if a > b {
			a, b = b, a
		}
		key := [2]string{a, b}
		if friendSet[key] {
			continue
		}
		friendSet[key] = true
		ca := daysAgo(rng.Intn(200) + 30)
		aa := ca.Add(time.Duration(rng.Intn(48)+1) * time.Hour)
		if err := insertFriendship(users[i].id, users[j].id, users[i].id, "accepted", ca, &aa); err != nil {
			return fmt.Errorf("bulk friendship %d↔%d: %w", i, j, err)
		}
		count++
	}

	// ── Posts ────────────────────────────────────────────────────────────────
	fmt.Println("→ inserting 40 posts…")

	postBodies := []string{
		"Day 30 sober. Never thought I'd make it this far. Taking it one day at a time.",
		"Morning run at 6am. Best way to start the day clean. The endorphins are real.",
		"Grateful for this community. You all have no idea how much it means.",
		"Hit a rough patch this week but I didn't pick up. Small wins matter.",
		"Three years today. Never giving this back.",
		"Yoga class this morning then coffee with a friend. Simple life is good life.",
		"Anyone else find weekends the hardest? Checking in.",
		"Just got my 6-month chip. Still can't believe it.",
		"Reminder: your past doesn't define your future. Keep going.",
		"First sober holiday. It was actually... nice?",
		"Meditation has genuinely changed how I handle cravings. Highly recommend.",
		"Went to my old bar last night with sober friends. Different experience entirely.",
		"Some days are just hard. That's ok. Tomorrow is another chance.",
		"New hobby unlocked: cycling. Much better than my old habits.",
		"The fog is lifting. Starting to feel like myself again.",
		"Found an AA meeting I actually like. The people there are incredible.",
		"Journalling every morning. Game changer for processing emotions.",
		"Ran my first 5k today. Sober me can do things old me never could.",
		"Grateful for the hard days too. They show me how far I've come.",
		"One month, one week, three days. Still counting. Still here.",
		"Coffee is my new best friend. Not complaining.",
		"Weekend hike with friends from this group. Unreal views.",
		"Therapy appointment today. Doing the work.",
		"Called my sponsor at 2am. He picked up. That's everything.",
		"Celebrated 1 year with a cake and zero regrets.",
		"The support in this app is something else. Thank you all.",
		"Bad day. Didn't use. Win.",
		"Learning to sit with uncomfortable feelings without running from them.",
		"Date night, sober. Actually enjoyed myself.",
		"Reading before bed instead of drinking. Life is genuinely better.",
		"Went to a wedding sober. Danced anyway.",
		"New city, new meeting, same warm welcome. Recovery community is universal.",
		"My kids said I seem happier. That hit different.",
		"Progress not perfection. My new mantra.",
		"Slept 8 hours. Wild what sobriety does for sleep.",
		"Two years ago I couldn't imagine this life. Now I can't imagine any other.",
		"Gratitude list today: morning coffee, sunshine, and all of you.",
		"Support meeting tonight. Always leave feeling better.",
		"Finding joy in small things again. That's recovery.",
		"This is hard. But I'm harder.",
	}

	type postRow struct {
		id        uuid.UUID
		userID    uuid.UUID
		createdAt time.Time
	}
	posts := make([]postRow, 40)

	// Posts 0-4: by friends 1-5 (high-engagement, appear in test user's feed)
	for i := 0; i < 5; i++ {
		posts[i] = postRow{uuid.New(), users[i+1].id, daysAgo(50 + rng.Intn(30))}
	}
	// Posts 5-9: by test user
	for i := 5; i < 10; i++ {
		posts[i] = postRow{uuid.New(), users[0].id, daysAgo(rng.Intn(60) + 5)}
	}
	// Posts 10-24: friends 6-20
	for i := 10; i < 25; i++ {
		posts[i] = postRow{uuid.New(), users[i-4].id, daysAgo(rng.Intn(90) + 10)}
	}
	// Posts 25-39: other users
	for i := 25; i < 40; i++ {
		posts[i] = postRow{uuid.New(), users[rng.Intn(65)+33].id, daysAgo(rng.Intn(150) + 30)}
	}

	for idx, p := range posts {
		if _, err := tx.Exec(ctx,
			`INSERT INTO posts (id, user_id, body, created_at) VALUES ($1,$2,$3,$4)`,
			p.id, p.userID, postBodies[idx], p.createdAt,
		); err != nil {
			return fmt.Errorf("insert post %d: %w", idx, err)
		}
	}

	// ── Comments ─────────────────────────────────────────────────────────────
	fmt.Println("→ inserting comments (100 / 50 / 20 + spread)…")

	commentBodies := []string{
		"Keep going, you've got this!",
		"So proud of you! This is huge.",
		"One day at a time. You're doing amazing.",
		"This made my day. Thank you for sharing.",
		"I felt this in my soul. Same boat here.",
		"The progress you've made is incredible. Don't stop.",
		"Sending so much love your way.",
		"This community needed to hear this today.",
		"You're an inspiration to all of us.",
		"Same. Every single day. We've got this.",
		"The hard days prove how strong you are.",
		"I'm here if you ever need to talk.",
		"Proud of you doesn't begin to cover it.",
		"This hit home. Thank you for being real.",
		"Keep putting one foot in front of the other.",
		"You're not alone in this. Never forget that.",
		"The best is yet to come. I believe that.",
		"Day by day, friend. Day by day.",
		"This is what recovery looks like. Beautiful.",
		"Called my sponsor after reading this. Thank you.",
		"Crying happy tears for you right now.",
		"You give me hope on hard days. Thank you.",
		"This is the kind of post that keeps me going.",
		"The person you're becoming is worth every hard moment.",
		"Your story matters. Keep telling it.",
		"So glad you're here and shared this.",
		"Progress over perfection. Always.",
		"This community is everything.",
		"I needed this today more than you know.",
		"Wow. Just wow. You're incredible.",
		"Every day sober is a gift.",
		"The struggle is real but so is the recovery.",
		"Grateful you shared this. Truly.",
		"You've got a whole community rooting for you.",
		"Still here. Still reading. Still proud of you.",
		"The growth I've seen from you is incredible.",
		"Never giving up sometimes just means getting through the hour.",
		"All the love to you right now.",
		"Thank you for the reminder. Needed it.",
		"This is what strength looks like.",
	}

	insertComments := func(postID uuid.UUID, count int, baseAgo, spreadDays int) error {
		userPool := make([]uuid.UUID, 100)
		for i := range userPool {
			userPool[i] = users[i].id
		}
		rng.Shuffle(len(userPool), func(i, j int) { userPool[i], userPool[j] = userPool[j], userPool[i] })

		for i := 0; i < count; i++ {
			commenter := userPool[i%len(userPool)]
			body := commentBodies[rng.Intn(len(commentBodies))]
			// Spread comments evenly from baseAgo back to baseAgo-spreadDays
			offsetDays := baseAgo - (i*spreadDays)/count
			if offsetDays < 0 {
				offsetDays = 0
			}
			createdAt := daysAgo(offsetDays).Add(-time.Duration(rng.Intn(60)) * time.Minute)
			if _, err := tx.Exec(ctx,
				`INSERT INTO comments (id, post_id, user_id, body, created_at) VALUES ($1,$2,$3,$4,$5)`,
				uuid.New(), postID, commenter, body, createdAt,
			); err != nil {
				return err
			}
		}
		return nil
	}

	commentPlan := []struct {
		postIdx int
		count   int
		baseAgo int
		spread  int
	}{
		{0, 100, 48, 45},  // friend's post: 100 comments over ~45 days
		{1, 50, 42, 38},   // another friend: 50 comments
		{5, 20, 28, 24},   // test user's post: 20 comments
		{2, 12, 35, 30},
		{3, 10, 40, 35},
		{4, 9, 30, 25},
		{6, 14, 25, 20},
		{7, 11, 20, 15},
		{8, 8, 18, 14},
		{9, 7, 15, 12},
	}
	for _, cp := range commentPlan {
		if err := insertComments(posts[cp.postIdx].id, cp.count, cp.baseAgo, cp.spread); err != nil {
			return fmt.Errorf("comments on post %d: %w", cp.postIdx, err)
		}
	}
	// Posts 10-24: 3-7 comments each
	for i := 10; i < 25; i++ {
		if err := insertComments(posts[i].id, 3+rng.Intn(5), 60, 50); err != nil {
			return fmt.Errorf("comments post %d: %w", i, err)
		}
	}
	// Posts 25-39: 1-3 comments each
	for i := 25; i < 40; i++ {
		if err := insertComments(posts[i].id, 1+rng.Intn(3), 90, 80); err != nil {
			return fmt.Errorf("comments post %d: %w", i, err)
		}
	}

	// ── Post reactions ───────────────────────────────────────────────────────
	fmt.Println("→ inserting ~300 reactions…")

	reactedPairs := make(map[[2]string]bool)
	for count := 0; count < 300; {
		pi := rng.Intn(40)
		ui := rng.Intn(100)
		key := [2]string{posts[pi].id.String(), users[ui].id.String()}
		if reactedPairs[key] {
			continue
		}
		reactedPairs[key] = true
		if _, err := tx.Exec(ctx,
			`INSERT INTO post_reactions (id, post_id, user_id, type) VALUES ($1,$2,$3,'like')
			 ON CONFLICT (post_id, user_id, type) DO NOTHING`,
			uuid.New(), posts[pi].id, users[ui].id,
		); err != nil {
			return fmt.Errorf("reaction: %w", err)
		}
		count++
	}

	// ── Meetups ──────────────────────────────────────────────────────────────
	fmt.Println("→ inserting 8 meetups…")

	type meetupRow struct {
		id          uuid.UUID
		organiserID uuid.UUID
		city        string
	}

	meetupDefs := []struct {
		organiser   uuid.UUID
		title, desc string
		city        string
		startsAt    time.Time
		capacity    int
	}{
		// Test user organises meetup 0
		{users[0].id, "London Sober Social", "Casual coffee and chat for people in recovery.", "London", time.Now().Add(7 * 24 * time.Hour), 30},
		{users[1].id, "Morning Runners — Manchester", "Sober running group, 5k easy pace, all welcome.", "Manchester", time.Now().Add(3 * 24 * time.Hour), 20},
		{users[2].id, "Dublin Recovery Walk", "Weekly walk around the bay. Good craic, good people.", "Dublin", time.Now().Add(10 * 24 * time.Hour), 40},
		{users[3].id, "NYC Mindfulness Morning", "Guided meditation and breakfast for people in recovery.", "New York", time.Now().Add(14 * 24 * time.Hour), 25},
		{users[4].id, "LA Beach Meetup", "Sunrise walk on the beach. Bring your coffee.", "Los Angeles", time.Now().Add(20 * 24 * time.Hour), 50},
		{users[5].id, "Berlin Sober Hiking", "Half-day hike in Grunewald. Moderate difficulty.", "Berlin", time.Now().Add(5 * 24 * time.Hour), 15},
		{users[6].id, "London Book Club", "Monthly sober book club. Bring your thoughts.", "London", time.Now().Add(30 * 24 * time.Hour), 20},
		{users[7].id, "Sydney Harbour Picnic", "BYO food, good vibes included.", "Sydney", time.Now().Add(25 * 24 * time.Hour), 60},
	}

	meetups := make([]meetupRow, len(meetupDefs))
	for i, m := range meetupDefs {
		meetups[i] = meetupRow{id: uuid.New(), organiserID: m.organiser, city: m.city}
		if _, err := tx.Exec(ctx,
			`INSERT INTO meetups (id, organiser_id, title, description, city, starts_at, capacity) VALUES ($1,$2,$3,$4,$5,$6,$7)`,
			meetups[i].id, m.organiser, m.title, m.desc, m.city, m.startsAt, m.capacity,
		); err != nil {
			return fmt.Errorf("insert meetup %d: %w", i, err)
		}
		// Organiser auto-attends
		if _, err := tx.Exec(ctx,
			`INSERT INTO meetup_attendees (meetup_id, user_id, rsvp_at) VALUES ($1,$2,$3) ON CONFLICT DO NOTHING`,
			meetups[i].id, m.organiser, daysAgo(rng.Intn(20)+5),
		); err != nil {
			return fmt.Errorf("organiser rsvp %d: %w", i, err)
		}
	}

	// Test user attends meetups 1, 2, 3
	for _, idx := range []int{1, 2, 3} {
		if _, err := tx.Exec(ctx,
			`INSERT INTO meetup_attendees (meetup_id, user_id, rsvp_at) VALUES ($1,$2,$3) ON CONFLICT DO NOTHING`,
			meetups[idx].id, users[0].id, daysAgo(rng.Intn(8)+1),
		); err != nil {
			return fmt.Errorf("test user rsvp %d: %w", idx, err)
		}
	}

	// Fill attendees per meetup
	attendeeTargets := []int{27, 18, 35, 22, 45, 13, 17, 55}
	rsvpd := make(map[[2]string]bool)
	for i, m := range meetups {
		rsvpd[[2]string{m.id.String(), m.organiserID.String()}] = true
		rsvpd[[2]string{m.id.String(), users[0].id.String()}] = true
		added := len(rsvpd) // approximate — just need to hit the target
		added = 1
		if i <= 3 {
			added = 2 // organiser + test user
		}
		for added < attendeeTargets[i] {
			u := users[rng.Intn(100)]
			key := [2]string{m.id.String(), u.id.String()}
			if rsvpd[key] {
				continue
			}
			rsvpd[key] = true
			if _, err := tx.Exec(ctx,
				`INSERT INTO meetup_attendees (meetup_id, user_id, rsvp_at) VALUES ($1,$2,$3) ON CONFLICT DO NOTHING`,
				m.id, u.id, daysAgo(rng.Intn(25)+1),
			); err != nil {
				return fmt.Errorf("meetup attendee: %w", err)
			}
			added++
		}
	}

	// ── Support requests ─────────────────────────────────────────────────────
	fmt.Println("→ inserting 25 support requests…")

	supportTypes := []string{"need_to_talk", "need_distraction", "need_encouragement", "need_company"}
	supportMessages := []string{
		"Really struggling today and could use someone to talk to.",
		"Need a distraction — been having dark thoughts. Just want to chat.",
		"Hit a milestone and nobody around me understands.",
		"Feeling really isolated. Would love some company right now.",
		"Rough day at work and feeling the pull. Anyone around?",
		"It's my anniversary date and I can't stop thinking about it.",
		"Just need to hear from someone who gets it.",
		"Three months tomorrow and I'm scared I'll mess it up.",
		"First weekend without meetings due to travel. Feeling wobbly.",
		"Had a dream about relapsing and it shook me. Need to talk.",
		"Family gathering coming up. Nervous and need support.",
		"Feeling proud but also alone. Anyone want to celebrate?",
		"Had a slip and feeling ashamed. Not sure what to do next.",
		"Triggers everywhere today. Need some distraction.",
		"Hit six months and can't stop crying happy tears.",
		"New job starts Monday and anxiety is through the roof.",
		"My sponsor is away. Feeling untethered.",
		"Bored and that's dangerous for me. Company please?",
		"Went to a party sober for the first time. Need to debrief.",
		"Feeling really grateful and want to share it with people who get it.",
		"The cravings are bad today. I haven't used but I need support.",
		"Told my family about my recovery. Mixed reactions.",
		"Starting step 4 tomorrow. Terrified.",
		"Feeling invisible in my recovery. Does it get easier?",
		"Rough afternoon. Anyone around to talk?",
	}

	type supportReqRow struct {
		id          uuid.UUID
		requesterID uuid.UUID
	}
	supportReqs := make([]supportReqRow, 25)

	// Friends of test user as requesters (so they're visible to test user under 'friends' audience)
	friendRequesters := make([]uuid.UUID, 20)
	for i := 0; i < 20; i++ {
		friendRequesters[i] = users[i+1].id
	}

	for i := 0; i < 25; i++ {
		var audience, city string
		var requesterID uuid.UUID
		switch {
		case i < 12:
			// community — visible to everyone
			audience = "community"
			requesterID = users[rng.Intn(65)+33].id
		case i < 18:
			// city-scoped
			audience = "city"
			loc := pick(locations)
			city = loc.city
			requesterID = users[rng.Intn(65)+33].id
		default:
			// friends — use friends of test user as requesters so they show up
			audience = "friends"
			requesterID = friendRequesters[rng.Intn(len(friendRequesters))]
		}

		var status string
		var matchedUID *uuid.UUID
		switch {
		case i < 18:
			status = "open"
		case i < 22:
			status = "matched"
			mid := users[rng.Intn(40)+1].id
			matchedUID = &mid
		default:
			status = "closed"
		}

		createdAt := hoursAgo(rng.Intn(48) + 2)
		// Open requests expire well in the future so they're visible
		expiresAt := time.Now().Add(time.Duration(24+rng.Intn(48)) * time.Hour)
		if status != "open" {
			expiresAt = createdAt.Add(48 * time.Hour)
		}

		var cityPtr *string
		if city != "" {
			cityPtr = &city
		}
		var closedAt *time.Time
		if status == "closed" {
			t := createdAt.Add(time.Duration(rng.Intn(24)+1) * time.Hour)
			closedAt = &t
		}

		supportReqs[i] = supportReqRow{id: uuid.New(), requesterID: requesterID}
		if _, err := tx.Exec(ctx, `
			INSERT INTO support_requests (id, requester_id, type, message, audience, city, status, matched_user_id, expires_at, created_at, closed_at)
			VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11)`,
			supportReqs[i].id, requesterID, pick(supportTypes), supportMessages[i],
			audience, cityPtr, status, matchedUID, expiresAt, createdAt, closedAt,
		); err != nil {
			return fmt.Errorf("support request %d: %w", i, err)
		}
	}

	// Test user's own support request (index 24 — overwrite)
	testSRID := uuid.New()
	testSRCreated := hoursAgo(6)
	testSRExpires := time.Now().Add(42 * time.Hour)
	if _, err := tx.Exec(ctx, `
		INSERT INTO support_requests (id, requester_id, type, message, audience, city, status, matched_user_id, expires_at, created_at, closed_at)
		VALUES ($1,$2,'need_to_talk',$3,'friends',NULL,'open',NULL,$4,$5,NULL)`,
		testSRID, users[0].id, "Rough afternoon. Anyone around to talk?", testSRExpires, testSRCreated,
	); err != nil {
		return fmt.Errorf("test user support request: %w", err)
	}
	supportReqs[24] = supportReqRow{id: testSRID, requesterID: users[0].id}

	// ── Support responses ────────────────────────────────────────────────────
	fmt.Println("→ inserting support responses…")

	responseTypes := []string{"can_chat", "check_in_later", "nearby"}
	responseMessages := []string{
		"I'm here and have time to chat now.",
		"I'll check in with you in a bit — hang tight.",
		"I'm nearby, happy to meet if that'd help.",
		"Sending you strength. I've been there.",
		"Happy to talk whenever you're ready.",
		"Not far from you — let me know if you want company.",
		"I'll check back in a couple of hours. You're not alone.",
		"DM me. I have time right now.",
		"Been there. Happy to chat.",
		"You've got this. And you've got us.",
	}

	respondedPairs := make(map[[2]string]bool)
	// Respond to open requests (0-17)
	for i := 0; i < 18; i++ {
		sr := supportReqs[i]
		numResponses := 3 + rng.Intn(8) // 3-10 per request
		added := 0
		for added < numResponses {
			u := users[rng.Intn(44)+1] // users who are available to support
			if u.id == sr.requesterID {
				continue
			}
			key := [2]string{sr.id.String(), u.id.String()}
			if respondedPairs[key] {
				continue
			}
			respondedPairs[key] = true
			rt := pick(responseTypes)
			msg := pick(responseMessages)
			createdAt := hoursAgo(rng.Intn(20) + 1)
			if _, err := tx.Exec(ctx, `
				INSERT INTO support_responses (id, support_request_id, responder_id, response_type, message, created_at)
				VALUES ($1,$2,$3,$4,$5,$6) ON CONFLICT DO NOTHING`,
				uuid.New(), sr.id, u.id, rt, msg, createdAt,
			); err != nil {
				return fmt.Errorf("support response sr%d: %w", i, err)
			}
			added++
		}
	}

	// Test user responded to 3 requests
	for i := 0; i < 3; i++ {
		sr := supportReqs[i]
		key := [2]string{sr.id.String(), users[0].id.String()}
		if respondedPairs[key] {
			continue
		}
		respondedPairs[key] = true
		if _, err := tx.Exec(ctx, `
			INSERT INTO support_responses (id, support_request_id, responder_id, response_type, message, created_at)
			VALUES ($1,$2,$3,'can_chat',$4,$5) ON CONFLICT DO NOTHING`,
			uuid.New(), sr.id, users[0].id, "I'm here if you want to chat.", hoursAgo(rng.Intn(12)+1),
		); err != nil {
			return fmt.Errorf("test user response %d: %w", i, err)
		}
	}

	// ── Chats + messages ─────────────────────────────────────────────────────
	fmt.Println("→ inserting 12 chats with messages…")

	messagePool := []string{
		"Hey, how are you doing today?",
		"Not bad! Had a tough morning but getting through it.",
		"Proud of you for reaching out. That takes courage.",
		"Thanks. This app has been a lifeline honestly.",
		"Same. The community here is something else.",
		"Did you see that post earlier? Really hit home.",
		"Yeah I commented. The person seems really strong.",
		"How's the sobriety journey going for you?",
		"Day 47 today. Hard to believe.",
		"That's massive! You should be so proud.",
		"Thank you. Days like today I really need to hear that.",
		"I'm always here if you need to talk.",
		"Means a lot. Genuinely.",
		"Are you going to the meetup next week?",
		"Thinking about it! Are you?",
		"Yeah, would be good to meet in person.",
		"These chats are great but real life connection is different.",
		"Exactly. How long have you been on the journey?",
		"Just over a year now. You?",
		"Eight months. Feels like forever and no time at all.",
		"Ha, I know that feeling well.",
		"Did you ever try journalling? Changed things for me.",
		"Started last month actually! You're right, it helps.",
		"What time do you usually do it?",
		"Morning, before the day gets going. Sets the tone.",
		"Smart. I might try that.",
		"Let me know how it goes!",
		"Will do. Thanks for the tip.",
		"Anytime. That's what we're here for.",
		"Sending good vibes. You've got this.",
	}

	type chatDef struct {
		userA, userB uuid.UUID
		msgCount     int
	}

	chatDefs := []chatDef{
		{users[0].id, users[1].id, 25},
		{users[0].id, users[2].id, 20},
		{users[0].id, users[3].id, 15},
		{users[0].id, users[4].id, 18},
		{users[0].id, users[5].id, 22},
		{users[1].id, users[2].id, 14},
		{users[3].id, users[4].id, 16},
		{users[5].id, users[6].id, 12},
		{users[7].id, users[8].id, 18},
		{users[9].id, users[10].id, 20},
		{users[11].id, users[12].id, 15},
		{users[13].id, users[14].id, 10},
	}

	for _, cd := range chatDefs {
		var chatID uuid.UUID
		if err := tx.QueryRow(ctx,
			`INSERT INTO chats (id, is_group, status, created_at) VALUES ($1, false, 'active', $2) RETURNING id`,
			uuid.New(), daysAgo(rng.Intn(60)+10),
		).Scan(&chatID); err != nil {
			return fmt.Errorf("insert chat: %w", err)
		}
		for _, m := range []struct {
			uid  uuid.UUID
			role string
		}{
			{cd.userA, "requester"}, {cd.userB, "addressee"},
		} {
			if _, err := tx.Exec(ctx,
				`INSERT INTO chat_members (chat_id, user_id, role) VALUES ($1,$2,$3)`,
				chatID, m.uid, m.role,
			); err != nil {
				return fmt.Errorf("chat member: %w", err)
			}
		}
		baseTime := daysAgo(rng.Intn(45) + 5)
		for i := 0; i < cd.msgCount; i++ {
			senderID := cd.userA
			if i%2 != 0 {
				senderID = cd.userB
			}
			sentAt := baseTime.Add(time.Duration(i) * time.Duration(rng.Intn(90)+5) * time.Minute)
			if _, err := tx.Exec(ctx,
				`INSERT INTO messages (id, chat_id, sender_id, body, sent_at) VALUES ($1,$2,$3,$4,$5)`,
				uuid.New(), chatID, senderID, pick(messagePool), sentAt,
			); err != nil {
				return fmt.Errorf("message: %w", err)
			}
		}
	}

	return tx.Commit(ctx)
}
