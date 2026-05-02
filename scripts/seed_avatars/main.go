package main

import (
	"bytes"
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"log"
	"math/rand"
	"mime/multipart"
	"net/http"
	"net/textproto"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

type userToSeed struct {
	ID       string
	Email    string
	Username string
	Gender   string
	InChats  bool
}

type assignment struct {
	User  userToSeed
	Image string
}

func main() {
	var (
		dbURL           = flag.String("db", os.Getenv("DATABASE_URL"), "Postgres connection string (defaults to $DATABASE_URL)")
		apiURL          = flag.String("api", "http://localhost:8080", "API base URL")
		imagesDir       = flag.String("images", expandHome("~/Downloads"), "Directory containing manN.jpg / womanN.jpg")
		password        = flag.String("password", "password123", "Shared password for seed users")
		testEmail       = flag.String("test-email", "test@radeon.dev", "Email of the user whose chat partners need unique photos")
		includeTestUser = flag.Bool("include-test-user", true, "Include the test user in avatar uploads")
		dryRun          = flag.Bool("dry-run", false, "Print the planned assignments without uploading")
	)
	flag.Parse()

	if *dbURL == "" {
		log.Fatal("DATABASE_URL not set; pass --db or export DATABASE_URL")
	}

	ctx := context.Background()
	pool, err := pgxpool.New(ctx, *dbURL)
	if err != nil {
		log.Fatalf("connect db: %v", err)
	}
	defer pool.Close()

	users, err := loadUsers(ctx, pool, *testEmail, *includeTestUser)
	if err != nil {
		log.Fatalf("load users: %v", err)
	}

	rng := rand.New(rand.NewSource(time.Now().UnixNano()))
	assignments := planAssignments(users, rng)

	if *dryRun {
		printPlan(assignments)
		return
	}

	if err := verifyImagesExist(*imagesDir); err != nil {
		log.Fatalf("image check: %v", err)
	}

	client := &http.Client{Timeout: 30 * time.Second}
	successes, failures := 0, 0
	// The API rate-limits per IP at ~1 req/sec with a small burst, and each
	// user costs two requests (login + upload). Pace at ~2.2s/user so we stay
	// under the limit and don't depend on retry-after recovery.
	pacing := 2200 * time.Millisecond
	for _, a := range assignments {
		imgPath := filepath.Join(*imagesDir, a.Image)
		if err := uploadAvatar(ctx, client, *apiURL, *password, a.User, imgPath); err != nil {
			failures++
			log.Printf("✗ %-22s (%-10s) ← %s: %v", a.User.Username, a.User.Gender, a.Image, err)
		} else {
			successes++
			marker := " "
			if a.User.InChats {
				marker = "★"
			}
			fmt.Printf("%s ✓ %-22s (%-10s) ← %s\n", marker, a.User.Username, a.User.Gender, a.Image)
		}
		time.Sleep(pacing)
	}
	fmt.Printf("\nDone: %d uploaded, %d failed (★ = chat-list partner)\n", successes, failures)
}

func loadUsers(ctx context.Context, pool *pgxpool.Pool, testEmail string, includeTestUser bool) ([]userToSeed, error) {
	rows, err := pool.Query(ctx, `
		WITH test_user AS (
			SELECT id FROM users WHERE email = $1
		),
		chat_partners AS (
			SELECT DISTINCT cm2.user_id
			FROM chat_members cm
			JOIN chat_members cm2 ON cm2.chat_id = cm.chat_id AND cm2.user_id != cm.user_id
			WHERE cm.user_id = (SELECT id FROM test_user)
		)
		SELECT u.id::text, u.email, u.username, COALESCE(u.gender, ''),
		       EXISTS(SELECT 1 FROM chat_partners cp WHERE cp.user_id = u.id) AS in_chats
		FROM users u
		WHERE ($2::boolean OR u.id != (SELECT id FROM test_user))
		ORDER BY in_chats DESC, u.username`, testEmail, includeTestUser)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []userToSeed
	for rows.Next() {
		var u userToSeed
		if err := rows.Scan(&u.ID, &u.Email, &u.Username, &u.Gender, &u.InChats); err != nil {
			return nil, err
		}
		out = append(out, u)
	}
	return out, rows.Err()
}

func planAssignments(users []userToSeed, rng *rand.Rand) []assignment {
	manImages := imageList("man")
	womanImages := imageList("woman")
	allImages := append(append([]string{}, manImages...), womanImages...)

	var chatPartners, others []userToSeed
	for _, u := range users {
		if u.InChats {
			chatPartners = append(chatPartners, u)
		} else {
			others = append(others, u)
		}
	}

	// Process chat partners in a stable order so re-runs assign the same image
	// to the same user.
	sort.SliceStable(chatPartners, func(i, j int) bool {
		if chatPartners[i].Gender != chatPartners[j].Gender {
			return genderOrder(chatPartners[i].Gender) < genderOrder(chatPartners[j].Gender)
		}
		return chatPartners[i].Username < chatPartners[j].Username
	})

	var out []assignment
	usedImages := map[string]bool{}
	manIdx, womanIdx := 0, 0

	for _, u := range chatPartners {
		var img string
		switch u.Gender {
		case "man":
			if manIdx >= len(manImages) {
				log.Printf("warning: %d man chat partners exceed %d man images; %s repeats", manIdx+1, len(manImages), u.Username)
				img = manImages[manIdx%len(manImages)]
			} else {
				img = manImages[manIdx]
				manIdx++
			}
		case "woman":
			if womanIdx >= len(womanImages) {
				log.Printf("warning: %d woman chat partners exceed %d woman images; %s repeats", womanIdx+1, len(womanImages), u.Username)
				img = womanImages[womanIdx%len(womanImages)]
			} else {
				img = womanImages[womanIdx]
				womanIdx++
			}
		default:
			img = pickUnusedRandom(allImages, usedImages, rng)
		}
		usedImages[img] = true
		out = append(out, assignment{User: u, Image: img})
	}

	for i, u := range others {
		var img string
		switch u.Gender {
		case "man":
			img = manImages[i%len(manImages)]
		case "woman":
			img = womanImages[i%len(womanImages)]
		default:
			img = allImages[rng.Intn(len(allImages))]
		}
		out = append(out, assignment{User: u, Image: img})
	}

	return out
}

func pickUnusedRandom(all []string, used map[string]bool, rng *rand.Rand) string {
	available := make([]string, 0, len(all))
	for _, c := range all {
		if !used[c] {
			available = append(available, c)
		}
	}
	if len(available) == 0 {
		return all[rng.Intn(len(all))]
	}
	return available[rng.Intn(len(available))]
}

func imageList(prefix string) []string {
	out := make([]string, 10)
	for i := 0; i < 10; i++ {
		out[i] = fmt.Sprintf("%s%d.jpg", prefix, i+1)
	}
	return out
}

func genderOrder(g string) int {
	switch g {
	case "man":
		return 0
	case "woman":
		return 1
	default:
		return 2
	}
}

func verifyImagesExist(dir string) error {
	for _, prefix := range []string{"man", "woman"} {
		for _, name := range imageList(prefix) {
			path := filepath.Join(dir, name)
			if _, err := os.Stat(path); err != nil {
				return fmt.Errorf("missing %s: %w", path, err)
			}
		}
	}
	return nil
}

func uploadAvatar(ctx context.Context, client *http.Client, apiURL, password string, u userToSeed, imgPath string) error {
	token, err := loginAs(ctx, client, apiURL, u.Email, password)
	if err != nil {
		return fmt.Errorf("login: %w", err)
	}

	file, err := os.Open(imgPath)
	if err != nil {
		return fmt.Errorf("open image: %w", err)
	}
	defer file.Close()

	body := &bytes.Buffer{}
	writer := multipart.NewWriter(body)
	header := make(textproto.MIMEHeader)
	header.Set("Content-Disposition", fmt.Sprintf(`form-data; name="avatar"; filename=%q`, filepath.Base(imgPath)))
	header.Set("Content-Type", "image/jpeg")
	part, err := writer.CreatePart(header)
	if err != nil {
		return fmt.Errorf("create form part: %w", err)
	}
	if _, err := io.Copy(part, file); err != nil {
		return fmt.Errorf("copy file: %w", err)
	}
	if err := writer.Close(); err != nil {
		return fmt.Errorf("close writer: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, strings.TrimRight(apiURL, "/")+"/users/me/avatar", body)
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Content-Type", writer.FormDataContentType())

	resp, err := doWithBackoff(client, req, body.Bytes(), writer.FormDataContentType())
	if err != nil {
		return fmt.Errorf("http: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 400 {
		respBody, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("upload %d: %s", resp.StatusCode, strings.TrimSpace(string(respBody)))
	}
	io.Copy(io.Discard, resp.Body)
	return nil
}

// doWithBackoff retries on 429 with exponential backoff. The request body is
// re-supplied each attempt because http.Request consumes it on send.
func doWithBackoff(client *http.Client, req *http.Request, body []byte, contentType string) (*http.Response, error) {
	delays := []time.Duration{2 * time.Second, 4 * time.Second, 8 * time.Second}
	for attempt := 0; ; attempt++ {
		if body != nil {
			req.Body = io.NopCloser(bytes.NewReader(body))
			req.Header.Set("Content-Type", contentType)
		}
		resp, err := client.Do(req)
		if err != nil {
			return nil, err
		}
		if resp.StatusCode != http.StatusTooManyRequests || attempt >= len(delays) {
			return resp, nil
		}
		io.Copy(io.Discard, resp.Body)
		resp.Body.Close()
		time.Sleep(delays[attempt])
	}
}

func loginAs(ctx context.Context, client *http.Client, apiURL, email, password string) (string, error) {
	payload, _ := json.Marshal(map[string]string{
		"email":    email,
		"password": password,
	})
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, strings.TrimRight(apiURL, "/")+"/auth/login", bytes.NewReader(payload))
	if err != nil {
		return "", err
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := doWithBackoff(client, req, payload, "application/json")
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 400 {
		body, _ := io.ReadAll(resp.Body)
		return "", fmt.Errorf("login %d: %s", resp.StatusCode, strings.TrimSpace(string(body)))
	}

	var result struct {
		Data struct {
			Token string `json:"token"`
		} `json:"data"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return "", fmt.Errorf("decode: %w", err)
	}
	if result.Data.Token == "" {
		return "", fmt.Errorf("no token in response")
	}
	return result.Data.Token, nil
}

func expandHome(path string) string {
	if strings.HasPrefix(path, "~/") {
		if home, err := os.UserHomeDir(); err == nil {
			return filepath.Join(home, path[2:])
		}
	}
	return path
}

func printPlan(plan []assignment) {
	fmt.Println("DRY RUN — no uploads will happen.")
	fmt.Println()
	for _, a := range plan {
		marker := " "
		if a.User.InChats {
			marker = "★"
		}
		fmt.Printf("%s %-22s (%-10s) ← %s\n", marker, a.User.Username, a.User.Gender, a.Image)
	}
	fmt.Printf("\nTotal: %d users (★ = chat-list partner)\n", len(plan))
}
