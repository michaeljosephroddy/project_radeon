package main

import (
	"context"
	"fmt"
	"log"
	"math"
	"math/rand"
	"sort"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/joho/godotenv"
	"github.com/project_radeon/api/pkg/database"
	"golang.org/x/crypto/bcrypt"
)

const totalUsers = 150

var rng = rand.New(rand.NewSource(42))

type citySeed struct {
	City     string
	Country  string
	Lat      float64
	Lng      float64
	SpreadKm float64
	Count    int
	Flavors  []string
}

type seededUser struct {
	ID                 uuid.UUID
	Username           string
	Email              string
	FirstName          string
	LastName           string
	City               string
	Country            string
	CurrentCity        string
	Bio                string
	Gender             string
	BirthDate          time.Time
	SoberSince         time.Time
	CreatedAt          time.Time
	LastActiveAt       time.Time
	SubscriptionTier   string
	SubscriptionStatus string
	IsAvailableSupport bool
	SupportUpdatedAt   *time.Time
	Lat                float64
	Lng                float64
	CurrentLat         float64
	CurrentLng         float64
	DiscoverLat        float64
	DiscoverLng        float64
	LocationUpdatedAt  time.Time
	Interests          []string
}

type seededPost struct {
	ID        uuid.UUID
	UserID    uuid.UUID
	CreatedAt time.Time
	Body      string
}

type seededMeetup struct {
	ID          uuid.UUID
	OrganizerID uuid.UUID
	City        string
}

type supportRequestRow struct {
	ID            uuid.UUID
	RequesterID   uuid.UUID
	Type          string
	City          *string
	Status        string
	MatchedUserID *uuid.UUID
	Urgency       string
	Priority      bool
	PriorityUntil *time.Time
	CreatedAt     time.Time
	ExpiresAt     time.Time
	ClosedAt      *time.Time
}

func pick[T any](items []T) T {
	return items[rng.Intn(len(items))]
}

func daysAgo(n int) time.Time {
	return time.Now().UTC().Add(-time.Duration(n) * 24 * time.Hour)
}

func hoursAgo(n int) time.Time {
	return time.Now().UTC().Add(-time.Duration(n) * time.Hour)
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
	fmt.Printf("  users: %d\n", totalUsers)
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

	fmt.Println("→ clearing existing data…")
	if _, err := tx.Exec(ctx, `
		TRUNCATE messages, chat_members, chats,
			support_responses, support_requests,
			meetup_attendees, meetups,
			post_reactions, comments, post_images, posts,
			user_interests, friendships, users
		RESTART IDENTITY CASCADE
	`); err != nil {
		return fmt.Errorf("truncate: %w", err)
	}

	interestNames, interestIDs, err := loadInterests(ctx, tx)
	if err != nil {
		return fmt.Errorf("load interests: %w", err)
	}

	fmt.Printf("→ inserting %d realistic users…\n", totalUsers)
	users := buildUsers(interestNames)
	if err := insertUsers(ctx, tx, users, passwordHash); err != nil {
		return err
	}
	if err := insertUserInterests(ctx, tx, users, interestIDs); err != nil {
		return err
	}

	fmt.Println("→ inserting friendships…")
	acceptedFriendIDs, err := insertFriendships(ctx, tx, users)
	if err != nil {
		return err
	}

	fmt.Println("→ inserting posts, comments, and reactions…")
	posts, err := insertPosts(ctx, tx, users, acceptedFriendIDs)
	if err != nil {
		return err
	}
	if err := insertComments(ctx, tx, posts, users); err != nil {
		return err
	}
	if err := insertReactions(ctx, tx, posts, users); err != nil {
		return err
	}

	fmt.Println("→ inserting meetups…")
	meetups, err := insertMeetups(ctx, tx, users)
	if err != nil {
		return err
	}
	if err := insertMeetupAttendees(ctx, tx, meetups, users); err != nil {
		return err
	}

	fmt.Println("→ inserting support requests, responses, chats, and messages…")
	supportRequests, err := insertSupportRequests(ctx, tx, users, acceptedFriendIDs)
	if err != nil {
		return err
	}
	if err := insertSupportResponses(ctx, tx, supportRequests, users); err != nil {
		return err
	}
	if err := insertDirectChats(ctx, tx, users, acceptedFriendIDs); err != nil {
		return err
	}
	if err := insertSupportChats(ctx, tx, supportRequests, users); err != nil {
		return err
	}

	fmt.Println("→ refreshing derived discovery/profile counters…")
	if err := refreshDerivedUserState(ctx, tx); err != nil {
		return err
	}
	if err := refreshSupportResponseCounts(ctx, tx); err != nil {
		return err
	}

	return tx.Commit(ctx)
}

func loadInterests(ctx context.Context, tx pgx.Tx) ([]string, map[string]uuid.UUID, error) {
	rows, err := tx.Query(ctx, `SELECT id, name FROM interests ORDER BY name`)
	if err != nil {
		return nil, nil, err
	}
	defer rows.Close()

	var names []string
	ids := make(map[string]uuid.UUID)
	for rows.Next() {
		var id uuid.UUID
		var name string
		if err := rows.Scan(&id, &name); err != nil {
			return nil, nil, err
		}
		names = append(names, name)
		ids[name] = id
	}
	return names, ids, rows.Err()
}

func buildUsers(interestNames []string) []seededUser {
	cities := []citySeed{
		{City: "Portlaoise", Country: "Ireland", Lat: 53.029160, Lng: -7.320510, SpreadKm: 7, Count: 12, Flavors: []string{"Coffee", "Running", "Meetups"}},
		{City: "Carlow", Country: "Ireland", Lat: 52.840022, Lng: -6.927866, SpreadKm: 7, Count: 9, Flavors: []string{"Coffee", "Nature Walks", "Yoga"}},
		{City: "Dublin", Country: "Ireland", Lat: 53.349804, Lng: -6.260310, SpreadKm: 11, Count: 27, Flavors: []string{"Coffee", "Live Music", "Meetups"}},
		{City: "Cork", Country: "Ireland", Lat: 51.896893, Lng: -8.486316, SpreadKm: 9, Count: 11, Flavors: []string{"Cooking", "Live Music", "Volunteering"}},
		{City: "Athy", Country: "Ireland", Lat: 52.993279, Lng: -6.981844, SpreadKm: 6, Count: 7, Flavors: []string{"Running", "Coffee", "Books"}},
		{City: "Galway", Country: "Ireland", Lat: 53.276685, Lng: -9.045096, SpreadKm: 8, Count: 7, Flavors: []string{"Live Music", "Hiking", "Movies"}},
		{City: "Athlone", Country: "Ireland", Lat: 53.430401, Lng: -7.941021, SpreadKm: 7, Count: 7, Flavors: []string{"Gym", "Meetups", "Nature Walks"}},
		{City: "Monasterevin", Country: "Ireland", Lat: 53.142826, Lng: -7.064399, SpreadKm: 5, Count: 6, Flavors: []string{"Journaling", "Coffee", "Running"}},
		{City: "Stradbally", Country: "Ireland", Lat: 53.014746, Lng: -7.148406, SpreadKm: 4, Count: 5, Flavors: []string{"Nature Walks", "Art", "Volunteering"}},
		{City: "Abbeyleix", Country: "Ireland", Lat: 52.914571, Lng: -7.350522, SpreadKm: 4, Count: 5, Flavors: []string{"Books", "Coffee", "Volunteering"}},
		{City: "Kilkenny", Country: "Ireland", Lat: 52.654245, Lng: -7.244605, SpreadKm: 6, Count: 7, Flavors: []string{"Art", "Coffee", "Movies"}},
		{City: "Belfast", Country: "United Kingdom", Lat: 54.618853, Lng: -5.966949, SpreadKm: 8, Count: 6, Flavors: []string{"Running", "Live Music", "Cooking"}},
		{City: "Rennes", Country: "France", Lat: 48.102703, Lng: -1.677351, SpreadKm: 8, Count: 5, Flavors: []string{"Cycling", "Coffee", "Meditation"}},
		{City: "Paris", Country: "France", Lat: 48.852821, Lng: 2.331892, SpreadKm: 10, Count: 6, Flavors: []string{"Art", "Coffee", "Meetups"}},
		{City: "Berlin", Country: "Germany", Lat: 52.514288, Lng: 13.372189, SpreadKm: 10, Count: 6, Flavors: []string{"Cycling", "Live Music", "Art"}},
		{City: "Madrid", Country: "Spain", Lat: 40.403317, Lng: -3.689342, SpreadKm: 10, Count: 5, Flavors: []string{"Running", "Cooking", "Meetups"}},
		{City: "New York", Country: "United States", Lat: 40.735958, Lng: -74.042681, SpreadKm: 12, Count: 10, Flavors: []string{"Coffee", "Running", "Volunteering"}},
		{City: "Los Angeles", Country: "United States", Lat: 33.863217, Lng: -118.195127, SpreadKm: 14, Count: 8, Flavors: []string{"Hiking", "Yoga", "Movies"}},
	}

	womenNames := []string{
		"Orla", "Aisling", "Niamh", "Saoirse", "Ciara", "Aoife", "Megan", "Rachel", "Hannah", "Emma",
		"Laura", "Chloe", "Jessica", "Amy", "Natalie", "Claire", "Lucy", "Emily", "Alice", "Grace",
		"Ella", "Molly", "Katie", "Zoe", "Tara", "Leah", "Sinead", "Fiona", "Maeve", "Roisin",
	}
	menNames := []string{
		"Liam", "Cian", "Conor", "Darragh", "Sean", "Patrick", "Michael", "David", "James", "Ryan",
		"Tom", "Mark", "Kevin", "Chris", "Brian", "Paul", "Andrew", "Luke", "Matthew", "Adam",
		"Aaron", "Nathan", "Eoin", "Ronan", "Jack", "Daniel", "Shane", "Ben", "Colm", "Kieran",
	}
	neutralNames := []string{
		"Alex", "Sam", "Jordan", "Taylor", "Morgan", "Casey", "Avery", "Rowan", "Jamie", "Quinn",
		"Riley", "Charlie", "Dakota", "Noel", "Harper",
	}
	lastNames := []string{
		"Murphy", "Walsh", "Brennan", "Doyle", "Byrne", "OBrien", "Nolan", "Keane", "Kennedy", "Lynch",
		"Hughes", "Clarke", "Byers", "Moran", "Power", "Hayes", "Kavanagh", "McCarthy", "Farrell", "Foley",
		"Taylor", "Brown", "Wilson", "Martin", "Scott", "Young", "Walker", "Baker", "Phillips", "Turner",
		"Ramirez", "Campbell", "Mitchell", "Parker", "Howard", "Torres", "Mills", "Sullivan", "Reid", "Dunn",
	}
	now := time.Now().UTC()
	usedUsernames := make(map[string]int)
	users := make([]seededUser, 0, totalUsers)

	testCity := cities[0]
	testLat, testLng := jitterCoords(testCity)
	testCurrentAt := now.Add(-2 * time.Hour)
	testSupportUpdated := now.Add(-36 * time.Hour)
	testBirthDate := buildBirthDate(34)
	testSoberSince := now.AddDate(-3, -4, 0)
	testInterests := normalizeInterestSelection(interestNames, []string{"Coffee", "Running", "Meetups", "Volunteering"}, 4)
	users = append(users, seededUser{
		ID:                 uuid.New(),
		Username:           "testuser",
		Email:              "test@radeon.dev",
		FirstName:          "Test",
		LastName:           "User",
		City:               testCity.City,
		Country:            testCity.Country,
		CurrentCity:        testCity.City,
		Bio:                "Three years into recovery, usually out for a coffee, a run, or checking in with friends around town.",
		Gender:             "man",
		BirthDate:          testBirthDate,
		SoberSince:         testSoberSince,
		CreatedAt:          now.AddDate(-2, -6, 0),
		LastActiveAt:       now.Add(-90 * time.Minute),
		SubscriptionTier:   "plus",
		SubscriptionStatus: "active",
		IsAvailableSupport: true,
		SupportUpdatedAt:   &testSupportUpdated,
		Lat:                testLat,
		Lng:                testLng,
		CurrentLat:         testLat,
		CurrentLng:         testLng,
		DiscoverLat:        testLat,
		DiscoverLng:        testLng,
		LocationUpdatedAt:  testCurrentAt,
		Interests:          testInterests,
	})
	usedUsernames["testuser"] = 1

	var cityAssignments []citySeed
	for _, city := range cities {
		for i := 0; i < city.Count; i++ {
			cityAssignments = append(cityAssignments, city)
		}
	}
	if len(cityAssignments) != totalUsers-1 {
		panic(fmt.Sprintf("city assignment mismatch: got %d, want %d", len(cityAssignments), totalUsers-1))
	}

	for index, city := range cityAssignments {
		gender := weightedGender(index)
		firstName := chooseFirstName(gender, womenNames, menNames, neutralNames)
		lastName := pick(lastNames)
		username := buildUsername(firstName, lastName, usedUsernames)
		email := fmt.Sprintf("%s@radeon.dev", username)
		age := pickAge(index)
		birthDate := buildBirthDate(age)
		soberSince := buildSoberSince(now, age)
		lat, lng := jitterCoords(city)
		locationUpdatedAt := now.Add(-time.Duration(rng.Intn(96)+1) * time.Hour)
		createdAt := now.Add(-time.Duration(rng.Intn(900)+45) * 24 * time.Hour)
		lastActiveAt := now.Add(-time.Duration(rng.Intn(7*24)+2) * time.Hour)
		if lastActiveAt.Before(createdAt) {
			lastActiveAt = createdAt.Add(time.Duration(rng.Intn(240)+24) * time.Hour)
		}
		interests := chooseInterests(interestNames, city.Flavors, 3+rng.Intn(3))
		availableToSupport := now.Sub(soberSince) >= 120*24*time.Hour && rng.Intn(100) < 52
		var supportUpdated *time.Time
		if availableToSupport {
			value := now.Add(-time.Duration(rng.Intn(20*24)+12) * time.Hour)
			supportUpdated = &value
		}

		subscriptionTier := "free"
		subscriptionStatus := "inactive"
		if rng.Intn(100) < 18 {
			subscriptionTier = "plus"
			subscriptionStatus = "active"
		} else if rng.Intn(100) < 5 {
			subscriptionTier = "plus"
			subscriptionStatus = "canceled"
		}

		users = append(users, seededUser{
			ID:                 uuid.New(),
			Username:           username,
			Email:              email,
			FirstName:          firstName,
			LastName:           lastName,
			City:               city.City,
			Country:            city.Country,
			CurrentCity:        city.City,
			Bio:                buildBio(firstName, city.City, interests, soberSince, now),
			Gender:             gender,
			BirthDate:          birthDate,
			SoberSince:         soberSince,
			CreatedAt:          createdAt,
			LastActiveAt:       lastActiveAt,
			SubscriptionTier:   subscriptionTier,
			SubscriptionStatus: subscriptionStatus,
			IsAvailableSupport: availableToSupport,
			SupportUpdatedAt:   supportUpdated,
			Lat:                lat,
			Lng:                lng,
			CurrentLat:         lat,
			CurrentLng:         lng,
			DiscoverLat:        lat,
			DiscoverLng:        lng,
			LocationUpdatedAt:  locationUpdatedAt,
			Interests:          interests,
		})
	}

	return users
}

func weightedGender(index int) string {
	switch index % 10 {
	case 0:
		return "non_binary"
	case 1, 3, 5, 7, 9:
		return "woman"
	default:
		return "man"
	}
}

func chooseFirstName(gender string, womenNames, menNames, neutralNames []string) string {
	switch gender {
	case "woman":
		return pick(womenNames)
	case "man":
		return pick(menNames)
	default:
		return pick(neutralNames)
	}
}

func buildUsername(firstName, lastName string, used map[string]int) string {
	base := strings.ToLower(firstName + "." + lastName)
	base = strings.Map(func(r rune) rune {
		switch {
		case r >= 'a' && r <= 'z':
			return r
		case r >= '0' && r <= '9':
			return r
		case r == '.':
			return r
		default:
			return -1
		}
	}, base)
	base = strings.Trim(base, ".")
	if len(base) > 20 {
		base = base[:20]
	}
	if len(base) < 3 {
		base = "member"
	}

	if used[base] == 0 {
		used[base] = 1
		return base
	}

	for suffix := used[base] + 1; ; suffix++ {
		candidate := fmt.Sprintf("%s%d", base, suffix)
		if len(candidate) > 20 {
			trimmed := base[:maxInt(3, 20-len(fmt.Sprint(suffix)))]
			candidate = fmt.Sprintf("%s%d", trimmed, suffix)
		}
		if used[candidate] == 0 {
			used[base] = suffix
			used[candidate] = 1
			return candidate
		}
	}
}

func pickAge(index int) int {
	switch {
	case index%9 == 0:
		return 22 + rng.Intn(5)
	case index%7 == 0:
		return 28 + rng.Intn(7)
	case index%5 == 0:
		return 35 + rng.Intn(8)
	case index%4 == 0:
		return 43 + rng.Intn(7)
	default:
		return 26 + rng.Intn(22)
	}
}

func buildBirthDate(age int) time.Time {
	year := time.Now().UTC().Year() - age
	month := time.Month(rng.Intn(12) + 1)
	day := rng.Intn(daysInMonth(year, month)) + 1
	return time.Date(year, month, day, 0, 0, 0, 0, time.UTC)
}

func buildSoberSince(now time.Time, age int) time.Time {
	bands := [][2]int{
		{35, 85},
		{100, 320},
		{365, 900},
		{900, 2200},
		{2200, 4200},
	}

	var minDays, maxDays int
	switch roll := rng.Intn(100); {
	case roll < 18:
		minDays, maxDays = bands[0][0], bands[0][1]
	case roll < 42:
		minDays, maxDays = bands[1][0], bands[1][1]
	case roll < 68:
		minDays, maxDays = bands[2][0], bands[2][1]
	case roll < 88:
		minDays, maxDays = bands[3][0], bands[3][1]
	default:
		minDays, maxDays = bands[4][0], bands[4][1]
	}

	maxPossible := maxInt(45, (age-16)*365)
	if maxDays > maxPossible {
		maxDays = maxPossible
	}
	if minDays > maxDays {
		minDays = maxInt(30, maxDays-30)
	}
	days := minDays
	if maxDays > minDays {
		days += rng.Intn(maxDays - minDays + 1)
	}
	return now.Add(-time.Duration(days) * 24 * time.Hour)
}

func buildBio(firstName, city string, interests []string, soberSince, now time.Time) string {
	primary := "coffee"
	secondary := "long walks"
	if len(interests) > 0 {
		primary = strings.ToLower(interests[0])
	}
	if len(interests) > 1 {
		secondary = strings.ToLower(interests[1])
	}

	milestone := recoveryPhrase(now.Sub(soberSince))
	templates := []string{
		fmt.Sprintf("%s based in %s. %s sober and usually splitting free time between %s and %s.", firstName, city, milestone, primary, secondary),
		fmt.Sprintf("%s here. Recovery first, then good routines, decent coffee, and a bit of %s whenever possible.", firstName, primary),
		fmt.Sprintf("Living in %s, staying grounded, and trying to keep life simple. Big on %s, %s, and checking in with people.", city, primary, secondary),
		fmt.Sprintf("%s from %s. Still building a life that feels steady, social, and actually enjoyable without drink or drugs.", firstName, city),
	}
	return templates[rng.Intn(len(templates))]
}

func recoveryPhrase(duration time.Duration) string {
	days := int(duration.Hours() / 24)
	switch {
	case days >= 365*5:
		return "More than five years"
	case days >= 365*2:
		return "A couple of years"
	case days >= 365:
		return "Over a year"
	case days >= 180:
		return "Six months plus"
	case days >= 90:
		return "A few solid months"
	default:
		return "Early days but fully committed"
	}
}

func chooseInterests(all []string, flavors []string, count int) []string {
	return normalizeInterestSelection(all, flavors, count)
}

func normalizeInterestSelection(all []string, preferred []string, count int) []string {
	available := make(map[string]struct{}, len(all))
	for _, name := range all {
		available[name] = struct{}{}
	}

	selected := make(map[string]struct{}, count)
	var ordered []string

	for _, name := range preferred {
		if len(ordered) >= count {
			break
		}
		if _, ok := available[name]; !ok {
			continue
		}
		if _, exists := selected[name]; exists {
			continue
		}
		selected[name] = struct{}{}
		ordered = append(ordered, name)
	}

	for len(ordered) < count {
		name := pick(all)
		if _, exists := selected[name]; exists {
			continue
		}
		selected[name] = struct{}{}
		ordered = append(ordered, name)
	}

	sort.Strings(ordered)
	return ordered
}

func jitterCoords(city citySeed) (float64, float64) {
	distance := math.Sqrt(rng.Float64()) * city.SpreadKm
	angle := rng.Float64() * 2 * math.Pi
	deltaLat := (distance / 111.32) * math.Cos(angle)
	lngScale := 111.32 * math.Cos(city.Lat*math.Pi/180)
	if lngScale == 0 {
		lngScale = 111.32
	}
	deltaLng := (distance / math.Abs(lngScale)) * math.Sin(angle)
	return city.Lat + deltaLat, city.Lng + deltaLng
}

func insertUsers(ctx context.Context, tx pgx.Tx, users []seededUser, passwordHash string) error {
	for _, user := range users {
		sobrietyBand := computeSobrietyBand(user.SoberSince)
		if _, err := tx.Exec(ctx, `
			INSERT INTO users (
				id, username, email, password_hash,
				city, country, bio, gender, birth_date, sober_since,
				subscription_tier, subscription_status,
				is_available_to_support, support_updated_at,
				lat, lng, current_lat, current_lng, current_city, location_updated_at,
				discover_lat, discover_lng, sobriety_band, profile_completeness, last_active_at, created_at
			)
			VALUES (
				$1, $2, $3, $4,
				$5, $6, $7, $8, $9, $10,
				$11, $12,
				$13, $14,
				$15, $16, $17, $18, $19, $20,
				$21, $22, $23, $24, $25, $26
			)`,
			user.ID, user.Username, user.Email, passwordHash,
			user.City, user.Country, user.Bio, user.Gender, user.BirthDate, user.SoberSince,
			user.SubscriptionTier, user.SubscriptionStatus,
			user.IsAvailableSupport, user.SupportUpdatedAt,
			user.Lat, user.Lng, user.CurrentLat, user.CurrentLng, user.CurrentCity, user.LocationUpdatedAt,
			user.DiscoverLat, user.DiscoverLng, sobrietyBand, 7, user.LastActiveAt, user.CreatedAt,
		); err != nil {
			return fmt.Errorf("insert user %s: %w", user.Username, err)
		}
	}
	return nil
}

func insertUserInterests(ctx context.Context, tx pgx.Tx, users []seededUser, interestIDs map[string]uuid.UUID) error {
	for _, user := range users {
		for _, interest := range user.Interests {
			interestID, ok := interestIDs[interest]
			if !ok {
				continue
			}
			if _, err := tx.Exec(ctx,
				`INSERT INTO user_interests (user_id, interest_id) VALUES ($1, $2) ON CONFLICT DO NOTHING`,
				user.ID, interestID,
			); err != nil {
				return fmt.Errorf("insert interest %s for %s: %w", interest, user.Username, err)
			}
		}
	}
	return nil
}

func insertFriendships(ctx context.Context, tx pgx.Tx, users []seededUser) (map[uuid.UUID]struct{}, error) {
	byCity := buildCityBuckets(users)
	used := map[uuid.UUID]struct{}{users[0].ID: {}}

	acceptedSelections := []struct {
		city  string
		count int
	}{
		{"Portlaoise", 3},
		{"Dublin", 4},
		{"Carlow", 2},
		{"Athy", 2},
		{"Kilkenny", 2},
		{"Cork", 1},
		{"Galway", 1},
		{"Athlone", 1},
		{"Belfast", 1},
		{"Monasterevin", 1},
	}
	incomingSelections := []struct {
		city  string
		count int
	}{
		{"Dublin", 2},
		{"Carlow", 1},
		{"Athy", 1},
		{"Portlaoise", 1},
		{"Berlin", 1},
	}
	outgoingSelections := []struct {
		city  string
		count int
	}{
		{"Cork", 1},
		{"Galway", 1},
		{"Paris", 1},
		{"New York", 1},
	}

	acceptedFriendIDs := make(map[uuid.UUID]struct{})
	for _, selection := range acceptedSelections {
		for _, idx := range takeUsersFromCity(users, byCity, selection.city, selection.count, used) {
			createdAt := daysAgo(rng.Intn(220) + 20)
			acceptedAt := createdAt.Add(time.Duration(rng.Intn(36)+2) * time.Hour)
			if err := insertFriendship(ctx, tx, users[0].ID, users[idx].ID, users[idx].ID, "accepted", createdAt, &acceptedAt); err != nil {
				return nil, err
			}
			acceptedFriendIDs[users[idx].ID] = struct{}{}
		}
	}

	for _, selection := range incomingSelections {
		for _, idx := range takeUsersFromCity(users, byCity, selection.city, selection.count, used) {
			createdAt := daysAgo(rng.Intn(10) + 1)
			if err := insertFriendship(ctx, tx, users[0].ID, users[idx].ID, users[idx].ID, "pending", createdAt, nil); err != nil {
				return nil, err
			}
		}
	}

	for _, selection := range outgoingSelections {
		for _, idx := range takeUsersFromCity(users, byCity, selection.city, selection.count, used) {
			createdAt := daysAgo(rng.Intn(7) + 1)
			if err := insertFriendship(ctx, tx, users[0].ID, users[idx].ID, users[0].ID, "pending", createdAt, nil); err != nil {
				return nil, err
			}
		}
	}

	friendPairs := map[[2]uuid.UUID]struct{}{}
	for id := range acceptedFriendIDs {
		friendPairs[orderedPair(users[0].ID, id)] = struct{}{}
	}

	for count := 0; count < 220; {
		left := users[rng.Intn(len(users)-1)+1]
		right := users[rng.Intn(len(users)-1)+1]
		if left.ID == right.ID {
			continue
		}
		key := orderedPair(left.ID, right.ID)
		if _, exists := friendPairs[key]; exists {
			continue
		}
		friendPairs[key] = struct{}{}
		createdAt := daysAgo(rng.Intn(320) + 15)
		acceptedAt := createdAt.Add(time.Duration(rng.Intn(48)+1) * time.Hour)
		if err := insertFriendship(ctx, tx, left.ID, right.ID, left.ID, "accepted", createdAt, &acceptedAt); err != nil {
			return nil, err
		}
		count++
	}

	for count := 0; count < 24; {
		left := users[rng.Intn(len(users)-1)+1]
		right := users[rng.Intn(len(users)-1)+1]
		if left.ID == right.ID {
			continue
		}
		key := orderedPair(left.ID, right.ID)
		if _, exists := friendPairs[key]; exists {
			continue
		}
		friendPairs[key] = struct{}{}
		createdAt := daysAgo(rng.Intn(12) + 1)
		if err := insertFriendship(ctx, tx, left.ID, right.ID, left.ID, "pending", createdAt, nil); err != nil {
			return nil, err
		}
		count++
	}

	return acceptedFriendIDs, nil
}

func buildCityBuckets(users []seededUser) map[string][]int {
	buckets := make(map[string][]int)
	for index := 1; index < len(users); index++ {
		buckets[users[index].City] = append(buckets[users[index].City], index)
	}
	return buckets
}

func takeUsersFromCity(users []seededUser, buckets map[string][]int, city string, count int, used map[uuid.UUID]struct{}) []int {
	selected := make([]int, 0, count)
	for _, idx := range buckets[city] {
		if len(selected) >= count {
			break
		}
		if _, exists := used[users[idx].ID]; exists {
			continue
		}
		selected = append(selected, idx)
		used[users[idx].ID] = struct{}{}
	}
	return selected
}

func insertFriendship(ctx context.Context, tx pgx.Tx, leftID, rightID, requesterID uuid.UUID, status string, createdAt time.Time, acceptedAt *time.Time) error {
	userA, userB := leftID, rightID
	if userA.String() > userB.String() {
		userA, userB = userB, userA
	}
	if _, err := tx.Exec(ctx, `
		INSERT INTO friendships (id, user_a_id, user_b_id, requester_id, status, created_at, accepted_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7)
		ON CONFLICT (user_a_id, user_b_id) DO NOTHING`,
		uuid.New(), userA, userB, requesterID, status, createdAt, acceptedAt,
	); err != nil {
		return fmt.Errorf("insert friendship %s-%s: %w", leftID, rightID, err)
	}
	return nil
}

func orderedPair(left, right uuid.UUID) [2]uuid.UUID {
	if left.String() < right.String() {
		return [2]uuid.UUID{left, right}
	}
	return [2]uuid.UUID{right, left}
}

func insertPosts(ctx context.Context, tx pgx.Tx, users []seededUser, acceptedFriendIDs map[uuid.UUID]struct{}) ([]seededPost, error) {
	postCount := 125
	posts := make([]seededPost, 0, postCount)
	authors := weightedPostAuthors(users, acceptedFriendIDs)
	now := time.Now().UTC()

	for index := 0; index < postCount; index++ {
		author := authors[rng.Intn(len(authors))]
		createdAt := now.Add(-time.Duration(rng.Intn(130*24)+2) * time.Hour)
		if index < 20 {
			createdAt = now.Add(-time.Duration(rng.Intn(10*24)+2) * time.Hour)
		}
		post := seededPost{
			ID:        uuid.New(),
			UserID:    author.ID,
			CreatedAt: createdAt,
			Body:      buildPostBody(author, index, now),
		}
		if _, err := tx.Exec(ctx,
			`INSERT INTO posts (id, user_id, body, created_at) VALUES ($1, $2, $3, $4)`,
			post.ID, post.UserID, post.Body, post.CreatedAt,
		); err != nil {
			return nil, fmt.Errorf("insert post %d: %w", index, err)
		}
		posts = append(posts, post)
	}

	return posts, nil
}

func weightedPostAuthors(users []seededUser, acceptedFriendIDs map[uuid.UUID]struct{}) []seededUser {
	var authors []seededUser
	for _, user := range users {
		weight := 1
		switch {
		case user.ID == users[0].ID:
			weight = 8
		case user.City == "Dublin" || user.City == "Portlaoise" || user.City == "Carlow":
			weight = 5
		case user.Country == "Ireland":
			weight = 3
		default:
			weight = 2
		}
		if _, ok := acceptedFriendIDs[user.ID]; ok {
			weight += 3
		}
		if time.Since(user.LastActiveAt) < 72*time.Hour {
			weight += 2
		}
		for i := 0; i < weight; i++ {
			authors = append(authors, user)
		}
	}
	return authors
}

func buildPostBody(user seededUser, index int, now time.Time) string {
	interestA := strings.ToLower(user.Interests[0])
	interestB := strings.ToLower(user.Interests[minInt(1, len(user.Interests)-1)])
	milestone := recoveryPhrase(now.Sub(user.SoberSince))
	templates := []string{
		fmt.Sprintf("%s sober today. Took things slow, got outside, and finished the day feeling steadier than it started.", milestone),
		fmt.Sprintf("Quiet day in %s. Coffee, %s, and a reminder that routine is doing a lot of heavy lifting for me lately.", user.City, interestA),
		fmt.Sprintf("Still amazed that a normal day built around %s and %s can feel this good now.", interestA, interestB),
		fmt.Sprintf("Had a wobble earlier but reached out instead of disappearing. That still feels like progress every single time."),
		fmt.Sprintf("Checking in from %s. Keeping it simple today: meeting, some %s, and an early night.", user.City, interestB),
		fmt.Sprintf("The best part of recovery lately has been getting interested in small things again. Today it was %s.", interestA),
		fmt.Sprintf("Anyone else find that the boring, ordinary days are where the real healing happens? Feeling that this week."),
		fmt.Sprintf("Spent the evening on %s and actually enjoyed my own company for once.", interestB),
	}
	return templates[index%len(templates)]
}

func insertComments(ctx context.Context, tx pgx.Tx, posts []seededPost, users []seededUser) error {
	commentBodies := []string{
		"Fair play. Needed to read this today.",
		"That sounds really solid. Delighted for you.",
		"I relate to this a lot. Thanks for saying it plainly.",
		"You're doing better than you think.",
		"This kind of honesty helps more than people realise.",
		"Keep going. One steady day at a time.",
		"Been there. Proud of you for sticking with it.",
		"Love this. Recovery can be so ordinary in the best way.",
		"That line about routine really landed with me.",
		"I needed that reminder. Thanks.",
	}

	now := time.Now().UTC()
	for index, post := range posts {
		commentCount := 0
		switch {
		case index < 12:
			commentCount = 10 + rng.Intn(8)
		case index < 48:
			commentCount = 3 + rng.Intn(6)
		default:
			commentCount = rng.Intn(4)
		}

		for count := 0; count < commentCount; count++ {
			commenter := users[rng.Intn(len(users))]
			if commenter.ID == post.UserID && len(users) > 1 {
				commenter = users[(count+index+1)%len(users)]
			}
			createdAt := randomTimeBetween(post.CreatedAt.Add(15*time.Minute), now.Add(-10*time.Minute))
			if _, err := tx.Exec(ctx,
				`INSERT INTO comments (id, post_id, user_id, body, created_at) VALUES ($1, $2, $3, $4, $5)`,
				uuid.New(), post.ID, commenter.ID, pick(commentBodies), createdAt,
			); err != nil {
				return fmt.Errorf("insert comment for post %s: %w", post.ID, err)
			}
		}
	}
	return nil
}

func insertReactions(ctx context.Context, tx pgx.Tx, posts []seededPost, users []seededUser) error {
	seen := make(map[[2]string]struct{})
	for count := 0; count < 650; {
		post := posts[rng.Intn(len(posts))]
		user := users[rng.Intn(len(users))]
		key := [2]string{post.ID.String(), user.ID.String()}
		if _, exists := seen[key]; exists {
			continue
		}
		seen[key] = struct{}{}
		if _, err := tx.Exec(ctx,
			`INSERT INTO post_reactions (id, post_id, user_id, type) VALUES ($1, $2, $3, 'like')
			 ON CONFLICT (post_id, user_id, type) DO NOTHING`,
			uuid.New(), post.ID, user.ID,
		); err != nil {
			return fmt.Errorf("insert reaction: %w", err)
		}
		count++
	}
	return nil
}

func insertMeetups(ctx context.Context, tx pgx.Tx, users []seededUser) ([]seededMeetup, error) {
	defs := []struct {
		organizer int
		title     string
		desc      string
		city      string
		startsAt  time.Time
		capacity  int
	}{
		{0, "Portlaoise Coffee Check-In", "A relaxed weekend coffee for people in recovery.", "Portlaoise", time.Now().UTC().Add(6 * 24 * time.Hour), 24},
		{5, "Dublin Coastal Walk", "Easy pace walk and a catch-up afterwards.", "Dublin", time.Now().UTC().Add(9 * 24 * time.Hour), 30},
		{12, "Cork Sunday Brunch", "Low-key brunch table and a good check-in.", "Cork", time.Now().UTC().Add(12 * 24 * time.Hour), 18},
		{20, "Galway Sober Social", "Evening hangout for anyone nearby.", "Galway", time.Now().UTC().Add(16 * 24 * time.Hour), 20},
		{28, "Kilkenny Midweek Walk", "Fresh air, short loop, and a natter.", "Kilkenny", time.Now().UTC().Add(8 * 24 * time.Hour), 16},
		{36, "Belfast Morning Run", "Easy 5k and coffee after.", "Belfast", time.Now().UTC().Add(10 * 24 * time.Hour), 16},
		{44, "Paris Recovery Picnic", "Bring a snack, meet some people, no pressure.", "Paris", time.Now().UTC().Add(18 * 24 * time.Hour), 28},
		{52, "Berlin Book and Brew", "Books, tea, and a quiet room full of kind people.", "Berlin", time.Now().UTC().Add(21 * 24 * time.Hour), 18},
		{60, "Madrid Park Meetup", "Sunshine, chats, and a walk around the park.", "Madrid", time.Now().UTC().Add(14 * 24 * time.Hour), 22},
		{70, "New York Evening Check-In", "Casual after-work meet for people staying steady.", "New York", time.Now().UTC().Add(11 * 24 * time.Hour), 26},
	}

	meetups := make([]seededMeetup, 0, len(defs))
	for _, def := range defs {
		meetupID := uuid.New()
		organizerID := users[def.organizer].ID
		if _, err := tx.Exec(ctx,
			`INSERT INTO meetups (id, organiser_id, title, description, city, starts_at, capacity) VALUES ($1, $2, $3, $4, $5, $6, $7)`,
			meetupID, organizerID, def.title, def.desc, def.city, def.startsAt, def.capacity,
		); err != nil {
			return nil, fmt.Errorf("insert meetup %s: %w", def.title, err)
		}
		meetups = append(meetups, seededMeetup{ID: meetupID, OrganizerID: organizerID, City: def.city})
	}
	return meetups, nil
}

func insertMeetupAttendees(ctx context.Context, tx pgx.Tx, meetups []seededMeetup, users []seededUser) error {
	byCity := make(map[string][]seededUser)
	for _, user := range users {
		byCity[user.City] = append(byCity[user.City], user)
	}

	for _, meetup := range meetups {
		candidates := byCity[meetup.City]
		attendeeTarget := 8 + rng.Intn(16)
		if attendeeTarget > len(candidates) {
			attendeeTarget = len(candidates)
		}
		seen := map[uuid.UUID]struct{}{meetup.OrganizerID: {}}
		if _, err := tx.Exec(ctx,
			`INSERT INTO meetup_attendees (meetup_id, user_id, rsvp_at) VALUES ($1, $2, $3) ON CONFLICT DO NOTHING`,
			meetup.ID, meetup.OrganizerID, daysAgo(rng.Intn(20)+1),
		); err != nil {
			return fmt.Errorf("insert organizer attendee: %w", err)
		}
		for len(seen) < attendeeTarget {
			user := candidates[rng.Intn(len(candidates))]
			if _, exists := seen[user.ID]; exists {
				continue
			}
			seen[user.ID] = struct{}{}
			if _, err := tx.Exec(ctx,
				`INSERT INTO meetup_attendees (meetup_id, user_id, rsvp_at) VALUES ($1, $2, $3) ON CONFLICT DO NOTHING`,
				meetup.ID, user.ID, daysAgo(rng.Intn(18)+1),
			); err != nil {
				return fmt.Errorf("insert meetup attendee: %w", err)
			}
		}
	}
	return nil
}

func insertSupportRequests(ctx context.Context, tx pgx.Tx, users []seededUser, acceptedFriendIDs map[uuid.UUID]struct{}) ([]supportRequestRow, error) {
	type choices struct {
		city string
		ids  []uuid.UUID
	}

	acceptedFriends := make([]uuid.UUID, 0, len(acceptedFriendIDs))
	for id := range acceptedFriendIDs {
		acceptedFriends = append(acceptedFriends, id)
	}

	communityTypes := []string{"need_to_talk", "need_distraction", "need_encouragement", "need_in_person_help"}
	requestMessages := []string{
		"Having a rough evening and trying not to isolate. Could use a check-in.",
		"Feeling very in my head today and need a distraction before I spiral.",
		"Milestone day and weirdly emotional about it. Anyone around?",
		"Really tempted to bail on my routine tonight. Need encouragement.",
		"Would love to hear from someone who gets what this kind of day feels like.",
		"First sober family event tomorrow and the nerves are loud.",
		"Been flat all afternoon. Looking for some company or a quick chat.",
		"Doing okay on the outside but not great on the inside. Reaching out early.",
		"Need a reset after work before I talk myself into bad ideas.",
		"Would really appreciate a kind voice or even a quick message.",
	}

	requests := make([]supportRequestRow, 0, 36)
	now := time.Now().UTC()

	urgencies := []string{"when_you_can", "soon", "right_now"}

	makeRequest := func(requesterID uuid.UUID, requestType string, city *string, status string, matchedUserID *uuid.UUID) supportRequestRow {
		createdAt := now.Add(-time.Duration(rng.Intn(10*24)+2) * time.Hour)
		expiresAt := createdAt.Add(time.Duration(24+rng.Intn(36)) * time.Hour)
		var closedAt *time.Time
		urgency := urgencies[rng.Intn(len(urgencies))]
		priority := status == "open" && (urgency == "right_now" || rng.Intn(6) == 0)
		var priorityUntil *time.Time
		if priority {
			value := createdAt.Add(time.Duration(3+rng.Intn(8)) * time.Hour)
			if value.Before(now) && status == "open" {
				value = now.Add(time.Duration(2+rng.Intn(4)) * time.Hour)
			}
			priorityUntil = &value
		}
		if status == "closed" {
			value := createdAt.Add(time.Duration(rng.Intn(16)+2) * time.Hour)
			closedAt = &value
			priority = false
			priorityUntil = nil
		}
		return supportRequestRow{
			ID:            uuid.New(),
			RequesterID:   requesterID,
			Type:          requestType,
			City:          city,
			Status:        status,
			MatchedUserID: matchedUserID,
			Urgency:       urgency,
			Priority:      priority,
			PriorityUntil: priorityUntil,
			CreatedAt:     createdAt,
			ExpiresAt:     expiresAt,
			ClosedAt:      closedAt,
		}
	}

	for index := 0; index < 14; index++ {
		requester := users[rng.Intn(len(users)-1)+1]
		requests = append(requests, makeRequest(requester.ID, pick(communityTypes), nil, "open", nil))
	}

	for _, city := range []string{"Portlaoise", "Dublin", "Carlow", "Cork", "Galway", "Kilkenny", "Athlone", "New York"} {
		cityValue := city
		var candidates []seededUser
		for _, user := range users {
			if user.City == city {
				candidates = append(candidates, user)
			}
		}
		requester := candidates[rng.Intn(len(candidates))]
		requests = append(requests, makeRequest(requester.ID, pick(communityTypes), &cityValue, "open", nil))
	}

	for index := 0; index < 8; index++ {
		requesterID := acceptedFriends[index%len(acceptedFriends)]
		requests = append(requests, makeRequest(requesterID, pick(communityTypes), nil, "open", nil))
	}

	for index := 0; index < 4; index++ {
		requester := users[rng.Intn(len(users)-1)+1]
		matchedUserID := availableSupportUserID(users, requester.ID)
		requests = append(requests, makeRequest(requester.ID, pick(communityTypes), nil, "matched", &matchedUserID))
	}

	for index := 0; index < 2; index++ {
		requesterID := acceptedFriends[(index+8)%len(acceptedFriends)]
		matchedUserID := availableSupportUserID(users, requesterID)
		requests = append(requests, makeRequest(requesterID, pick(communityTypes), nil, "matched", &matchedUserID))
	}

	for index := 0; index < 4; index++ {
		requester := users[rng.Intn(len(users)-1)+1]
		requests = append(requests, makeRequest(requester.ID, pick(communityTypes), nil, "closed", nil))
	}

	portlaoise := "Portlaoise"
	testRequest := supportRequestRow{
		ID:            uuid.New(),
		RequesterID:   users[0].ID,
		Type:          "need_to_talk",
		City:          &portlaoise,
		Status:        "open",
		Urgency:       "right_now",
		Priority:      true,
		PriorityUntil: ptrTime(now.Add(5 * time.Hour)),
		CreatedAt:     now.Add(-6 * time.Hour),
		ExpiresAt:     now.Add(40 * time.Hour),
	}
	requests = append(requests, testRequest)

	for index, request := range requests {
		if _, err := tx.Exec(ctx, `
			INSERT INTO support_requests (
				id, requester_id, type, message, city, status, matched_user_id,
				expires_at, created_at, closed_at, priority_visibility, priority_expires_at, urgency
			)
			VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13)`,
			request.ID, request.RequesterID, request.Type, requestMessages[index%len(requestMessages)],
			request.City, request.Status, request.MatchedUserID, request.ExpiresAt, request.CreatedAt, request.ClosedAt,
			request.Priority, request.PriorityUntil, request.Urgency,
		); err != nil {
			return nil, fmt.Errorf("insert support request %d: %w", index, err)
		}
	}

	return requests, nil
}

func availableSupportUserID(users []seededUser, exclude uuid.UUID) uuid.UUID {
	var candidates []uuid.UUID
	for _, user := range users {
		if user.ID == exclude || !user.IsAvailableSupport {
			continue
		}
		candidates = append(candidates, user.ID)
	}
	return candidates[rng.Intn(len(candidates))]
}

func insertSupportResponses(ctx context.Context, tx pgx.Tx, requests []supportRequestRow, users []seededUser) error {
	responseTypes := []string{"can_chat", "check_in_later", "can_meet"}
	responseBodies := []string{
		"I have time now if you want to chat.",
		"I can check back in with you later this evening.",
		"I'm nearby enough to meet if that would genuinely help.",
		"You're not alone with this. Happy to talk.",
		"Sending you a bit of steadiness. Reach back if you can.",
		"I've had days like that. Here if you want company.",
	}

	available := make([]seededUser, 0)
	for _, user := range users {
		if user.IsAvailableSupport {
			available = append(available, user)
		}
	}

	seen := make(map[[3]string]struct{})
	for _, request := range requests {
		if request.Status != "open" {
			continue
		}
		responseCount := 2 + rng.Intn(5)
		for created := 0; created < responseCount; {
			responder := available[rng.Intn(len(available))]
			if responder.ID == request.RequesterID {
				continue
			}
			responseType := pick(responseTypes)
			key := [3]string{request.ID.String(), responder.ID.String(), responseType}
			if _, exists := seen[key]; exists {
				continue
			}
			seen[key] = struct{}{}
			createdAt := request.CreatedAt.Add(time.Duration(rng.Intn(14)+1) * time.Hour)
			if _, err := tx.Exec(ctx, `
				INSERT INTO support_responses (id, support_request_id, responder_id, response_type, message, created_at)
				VALUES ($1, $2, $3, $4, $5, $6)`,
				uuid.New(), request.ID, responder.ID, responseType, pick(responseBodies), createdAt,
			); err != nil {
				return fmt.Errorf("insert support response for %s: %w", request.ID, err)
			}
			created++
		}
	}
	return nil
}

func insertDirectChats(ctx context.Context, tx pgx.Tx, users []seededUser, acceptedFriendIDs map[uuid.UUID]struct{}) error {
	messagePool := []string{
		"How are you getting on today?",
		"Bit up and down, but still steady.",
		"That still counts as a good day in my book.",
		"Did you make it to the meeting in the end?",
		"Yeah, and I felt better for going.",
		"Fair play. The hard part is usually just showing up.",
		"Exactly. Once I'm there I'm usually grand.",
		"Want to grab coffee this week?",
		"Would love that. Thursday maybe?",
		"Thursday works. Mid-morning suits me best.",
		"I've been thinking about what you said the other day.",
		"Hopefully in a good way.",
		"It was. Helped me reset a bit.",
		"Glad to hear it.",
		"Keep me posted later if you need a hand.",
	}

	var testUserChatPartners []uuid.UUID
	for id := range acceptedFriendIDs {
		testUserChatPartners = append(testUserChatPartners, id)
	}
	sort.Slice(testUserChatPartners, func(i, j int) bool { return testUserChatPartners[i].String() < testUserChatPartners[j].String() })

	pairs := make([][2]uuid.UUID, 0, 18)
	for index := 0; index < minInt(9, len(testUserChatPartners)); index++ {
		pairs = append(pairs, [2]uuid.UUID{users[0].ID, testUserChatPartners[index]})
	}
	for len(pairs) < 18 {
		left := users[rng.Intn(len(users)-1)+1].ID
		right := users[rng.Intn(len(users)-1)+1].ID
		if left == right {
			continue
		}
		duplicate := false
		for _, pair := range pairs {
			if (pair[0] == left && pair[1] == right) || (pair[0] == right && pair[1] == left) {
				duplicate = true
				break
			}
		}
		if duplicate {
			continue
		}
		pairs = append(pairs, [2]uuid.UUID{left, right})
	}

	for index, pair := range pairs {
		chatID := uuid.New()
		chatCreatedAt := daysAgo(rng.Intn(80) + 3)
		if _, err := tx.Exec(ctx,
			`INSERT INTO chats (id, is_group, status, created_at) VALUES ($1, false, 'active', $2)`,
			chatID, chatCreatedAt,
		); err != nil {
			return fmt.Errorf("insert chat %d: %w", index, err)
		}
		if _, err := tx.Exec(ctx,
			`INSERT INTO chat_members (chat_id, user_id, role) VALUES ($1, $2, 'requester'), ($1, $3, 'addressee')`,
			chatID, pair[0], pair[1],
		); err != nil {
			return fmt.Errorf("insert chat members %d: %w", index, err)
		}

		messageCount := 8 + rng.Intn(14)
		baseTime := randomTimeBetween(chatCreatedAt, time.Now().UTC().Add(-24*time.Hour))
		for messageIndex := 0; messageIndex < messageCount; messageIndex++ {
			senderID := pair[0]
			if messageIndex%2 == 1 {
				senderID = pair[1]
			}
			sentAt := baseTime.Add(time.Duration(messageIndex*(rng.Intn(80)+12)) * time.Minute)
			if _, err := tx.Exec(ctx,
				`INSERT INTO messages (id, chat_id, sender_id, body, sent_at) VALUES ($1, $2, $3, $4, $5)`,
				uuid.New(), chatID, senderID, messagePool[(index+messageIndex)%len(messagePool)], sentAt,
			); err != nil {
				return fmt.Errorf("insert message %d/%d: %w", index, messageIndex, err)
			}
		}
	}

	return nil
}

func insertSupportChats(ctx context.Context, tx pgx.Tx, requests []supportRequestRow, users []seededUser) error {
	messagePool := []string{
		"Hey, I saw your support request and wanted to check in properly.",
		"Thanks for reaching out. It's been a rough day.",
		"You're not carrying it alone tonight.",
		"That helps more than you know.",
		"Want to talk through what happened?",
		"Yeah, I think I need that.",
		"Take your time. No pressure.",
		"Appreciate that. I'm calming down a bit already.",
	}

	created := 0
	for _, request := range requests {
		if request.Status != "matched" || request.MatchedUserID == nil || created >= 4 {
			continue
		}

		chatID := uuid.New()
		if _, err := tx.Exec(ctx,
			`INSERT INTO chats (id, is_group, status, support_request_id, created_at) VALUES ($1, false, 'active', $2, $3)`,
			chatID, request.ID, request.CreatedAt.Add(30*time.Minute),
		); err != nil {
			return fmt.Errorf("insert support chat: %w", err)
		}
		if _, err := tx.Exec(ctx,
			`INSERT INTO chat_members (chat_id, user_id, role) VALUES ($1, $2, 'requester'), ($1, $3, 'addressee')`,
			chatID, request.RequesterID, *request.MatchedUserID,
		); err != nil {
			return fmt.Errorf("insert support chat members: %w", err)
		}
		for messageIndex := 0; messageIndex < 8; messageIndex++ {
			senderID := request.RequesterID
			if messageIndex%2 == 0 {
				senderID = *request.MatchedUserID
			}
			sentAt := request.CreatedAt.Add(time.Duration(messageIndex+1) * 18 * time.Minute)
			if _, err := tx.Exec(ctx,
				`INSERT INTO messages (id, chat_id, sender_id, body, sent_at) VALUES ($1, $2, $3, $4, $5)`,
				uuid.New(), chatID, senderID, messagePool[messageIndex%len(messagePool)], sentAt,
			); err != nil {
				return fmt.Errorf("insert support chat message: %w", err)
			}
		}

		if _, err := tx.Exec(ctx, `
			INSERT INTO support_responses (id, support_request_id, responder_id, response_type, message, chat_id, created_at)
			VALUES ($1, $2, $3, 'can_chat', $4, $5, $6)
			ON CONFLICT (support_request_id, responder_id, response_type) DO UPDATE
			SET chat_id = EXCLUDED.chat_id`,
			uuid.New(), request.ID, *request.MatchedUserID, "Opened a direct support chat.", chatID, request.CreatedAt.Add(20*time.Minute),
		); err != nil {
			return fmt.Errorf("insert matched support response: %w", err)
		}
		created++
	}
	return nil
}

func refreshDerivedUserState(ctx context.Context, tx pgx.Tx) error {
	if _, err := tx.Exec(ctx, `
		UPDATE users u
		SET friend_count = (
			SELECT COUNT(*)
			FROM friendships f
			WHERE f.status = 'accepted'
				AND (f.user_a_id = u.id OR f.user_b_id = u.id)
		),
		discover_lat = COALESCE(u.current_lat, u.lat),
		discover_lng = COALESCE(u.current_lng, u.lng),
		sobriety_band = CASE
			WHEN u.sober_since IS NULL THEN NULL
			WHEN CURRENT_DATE - u.sober_since < 30 THEN 1
			WHEN CURRENT_DATE - u.sober_since < 90 THEN 2
			WHEN CURRENT_DATE - u.sober_since < 365 THEN 3
			WHEN CURRENT_DATE - u.sober_since < 730 THEN 4
			WHEN CURRENT_DATE - u.sober_since < 1825 THEN 5
			ELSE 6
		END,
		profile_completeness = (
			CASE WHEN NULLIF(u.avatar_url, '') IS NOT NULL THEN 1 ELSE 0 END
			+ CASE WHEN NULLIF(u.city, '') IS NOT NULL THEN 1 ELSE 0 END
			+ CASE WHEN NULLIF(u.country, '') IS NOT NULL THEN 1 ELSE 0 END
			+ CASE WHEN NULLIF(u.bio, '') IS NOT NULL THEN 1 ELSE 0 END
			+ CASE WHEN NULLIF(u.gender, '') IS NOT NULL THEN 1 ELSE 0 END
			+ CASE WHEN u.birth_date IS NOT NULL THEN 1 ELSE 0 END
			+ CASE WHEN u.sober_since IS NOT NULL THEN 1 ELSE 0 END
			+ CASE
				WHEN EXISTS (SELECT 1 FROM user_interests ui WHERE ui.user_id = u.id) THEN 1
				ELSE 0
			  END
		)::smallint,
		last_active_at = COALESCE((SELECT MAX(p.created_at) FROM posts p WHERE p.user_id = u.id), u.last_active_at, u.created_at)
	`); err != nil {
		return fmt.Errorf("refresh user state: %w", err)
	}
	return nil
}

func refreshSupportResponseCounts(ctx context.Context, tx pgx.Tx) error {
	if _, err := tx.Exec(ctx, `
		UPDATE support_requests sr
		SET response_count = (
			SELECT COUNT(*)
			FROM support_responses rsp
			WHERE rsp.support_request_id = sr.id
		)
	`); err != nil {
		return fmt.Errorf("refresh support counts: %w", err)
	}
	return nil
}

func randomTimeBetween(start, end time.Time) time.Time {
	if !end.After(start) {
		return start
	}
	delta := end.Sub(start)
	return start.Add(time.Duration(rng.Int63n(int64(delta))))
}

func ptrTime(value time.Time) *time.Time {
	return &value
}

func computeSobrietyBand(soberSince time.Time) int {
	days := int(time.Since(soberSince).Hours() / 24)
	switch {
	case days < 30:
		return 1
	case days < 90:
		return 2
	case days < 365:
		return 3
	case days < 730:
		return 4
	case days < 1825:
		return 5
	default:
		return 6
	}
}

func daysInMonth(year int, month time.Month) int {
	return time.Date(year, month+1, 0, 0, 0, 0, 0, time.UTC).Day()
}

func minInt(left, right int) int {
	if left < right {
		return left
	}
	return right
}

func maxInt(left, right int) int {
	if left > right {
		return left
	}
	return right
}
