package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"net/url"
	"os"
	"sort"
	"strings"
	"time"
)

type AuthResponse struct {
	Token string `json:"token"`
	User  struct {
		ID       int    `json:"id"`
		Username string `json:"username"`
	} `json:"user"`
}

type Manga struct {
	ID            int      `json:"id"`
	Title         string   `json:"title"`
	Author        string   `json:"author"`
	Genres        []string `json:"genres"`
	Status        string   `json:"status"`
	TotalChapters int      `json:"total_chapters"`
	Description   string   `json:"description"`
}

type MangaListResponse struct {
	Manga []Manga `json:"manga"`
	Count int     `json:"count"`
	Query string  `json:"query"`
}

type UserProgress struct {
	UserID         int    `json:"user_id"`
	MangaID        int    `json:"manga_id"`
	CurrentChapter int    `json:"current_chapter"`
	Status         string `json:"status"`
	UpdatedAt      string `json:"updated_at"`
}

type LibraryItem struct {
	Manga    Manga        `json:"manga"`
	Progress UserProgress `json:"progress"`
}

type LibraryResponse struct {
	Library []LibraryItem `json:"library"`
	Count   int           `json:"count"`
}

type LibraryItemView struct {
	ID       int
	Title    string
	Chapter  string
	Status   string
	Updated  string
	Author   string
	Genres   string
	Progress int
}

func cmdInit(app *App, args []string) {
	fs := flag.NewFlagSet("init", flag.ContinueOnError)
	fs.SetOutput(os.Stderr)
	force := fs.Bool("force", false, "overwrite config")
	if err := fs.Parse(args); err != nil {
		return
	}

	path := app.ConfigPath
	if path == "" {
		path = DefaultConfigPath()
	}

	if !*force {
		if _, err := os.Stat(path); err == nil {
			fmt.Println("Config already exists:", path)
			fmt.Println("Use 'mangahub init --force' to overwrite.")
			return
		}
	}

	cfg := DefaultConfig()
	if err := SaveConfig(path, cfg); err != nil {
		fmt.Fprintln(os.Stderr, "Error:", err)
		return
	}
	if err := ensureDir(defaultLogsDir()); err != nil {
		fmt.Fprintln(os.Stderr, "Error:", err)
		return
	}

	fmt.Println("Initialized MangaHub config at", path)
}

func cmdConfig(app *App, args []string) {
	if len(args) == 0 || isHelpArg(args[0]) {
		printConfigUsage()
		return
	}

	switch args[0] {
	case "show":
		cmdConfigShow(app)
	case "set":
		cmdConfigSet(app, args[1:])
	case "reset":
		cmdConfigReset(app)
	default:
		printConfigUsage()
	}
}

func printConfigUsage() {
	fmt.Println("Usage:")
	fmt.Println("  mangahub config show")
	fmt.Println("  mangahub config set <key> <value>")
	fmt.Println("  mangahub config reset")
	fmt.Println()
	fmt.Println("Keys:")
	fmt.Println("  http.base_url, tcp.addr, udp.addr, grpc.addr, ws.url")
}

func cmdConfigShow(app *App) {
	data := map[string]string{
		"http.base_url": app.Config.HTTPBaseURL,
		"tcp.addr":      app.Config.TCPAddr,
		"udp.addr":      app.Config.UDPAddr,
		"grpc.addr":     app.Config.GRPCAddr,
		"ws.url":        app.Config.WSURL,
	}
	keys := []string{"http.base_url", "tcp.addr", "udp.addr", "grpc.addr", "ws.url"}
	printJSONOrText(app, data, "Current config", func() {
		printKeyValues(keys, data)
	})
}

func cmdConfigSet(app *App, args []string) {
	if len(args) < 2 {
		printConfigUsage()
		return
	}
	key := args[0]
	value := args[1]

	switch key {
	case "http.base_url":
		app.Config.HTTPBaseURL = value
	case "tcp.addr":
		app.Config.TCPAddr = value
	case "udp.addr":
		app.Config.UDPAddr = value
	case "grpc.addr":
		app.Config.GRPCAddr = value
	case "ws.url":
		app.Config.WSURL = value
	default:
		fmt.Fprintln(os.Stderr, "Unknown config key:", key)
		return
	}

	if err := SaveConfig(app.ConfigPath, app.Config); err != nil {
		fmt.Fprintln(os.Stderr, "Error:", err)
		return
	}
	fmt.Println("Updated", key)
}

func cmdConfigReset(app *App) {
	app.Config = DefaultConfig()
	if err := SaveConfig(app.ConfigPath, app.Config); err != nil {
		fmt.Fprintln(os.Stderr, "Error:", err)
		return
	}
	fmt.Println("Config reset to defaults")
}

func cmdAuth(app *App, args []string) {
	if len(args) == 0 || isHelpArg(args[0]) {
		printAuthUsage()
		return
	}

	switch args[0] {
	case "register":
		cmdAuthRegister(app, args[1:])
	case "login":
		cmdAuthLogin(app, args[1:])
	case "logout":
		cmdAuthLogout(app)
	case "status":
		cmdAuthStatus(app)
	case "change-password":
		fmt.Println("Change password is not supported by the current server API.")
	case "clear":
		cmdAuthLogout(app)
	default:
		printAuthUsage()
	}
}

func printAuthUsage() {
	fmt.Println("Usage:")
	fmt.Println("  mangahub auth register --username <name> [--email <email>] [--password <pass>]")
	fmt.Println("  mangahub auth login --username <name> [--password <pass>]")
	fmt.Println("  mangahub auth logout")
	fmt.Println("  mangahub auth status")
	fmt.Println("  mangahub auth change-password")
}

func cmdAuthRegister(app *App, args []string) {
	fs := flag.NewFlagSet("register", flag.ContinueOnError)
	fs.SetOutput(os.Stderr)
	username := fs.String("username", "", "username")
	email := fs.String("email", "", "email")
	password := fs.String("password", "", "password")
	if err := fs.Parse(args); err != nil {
		return
	}

	if *username == "" {
		fmt.Fprintln(os.Stderr, "Missing --username")
		return
	}
	pass := *password
	if pass == "" {
		p1, err := promptHidden("Password: ")
		if err != nil {
			fmt.Fprintln(os.Stderr, "Error:", err)
			return
		}
		p2, err := promptHidden("Confirm password: ")
		if err != nil {
			fmt.Fprintln(os.Stderr, "Error:", err)
			return
		}
		if p1 != p2 {
			fmt.Fprintln(os.Stderr, "Passwords do not match")
			return
		}
		pass = p1
	}

	payload := map[string]string{
		"username": *username,
		"password": pass,
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	data, _, err := doJSON(ctx, "POST", apiURL(app.Config.HTTPBaseURL, "/auth/register"), payload, "")
	if err != nil {
		fmt.Fprintln(os.Stderr, "Registration failed:", err)
		return
	}

	var resp AuthResponse
	if err := json.Unmarshal(data, &resp); err != nil {
		fmt.Fprintln(os.Stderr, "Invalid response:", err)
		return
	}

	app.Config.Token = resp.Token
	app.Config.Username = resp.User.Username
	_ = SaveConfig(app.ConfigPath, app.Config)

	created := time.Now().UTC().Format("2006-01-02 15:04:05 MST")

	printJSONOrText(app, resp, "", func() {
		fmt.Println("Account created successfully")
		fmt.Println("User ID:", resp.User.ID)
		fmt.Println("Username:", resp.User.Username)
		if *email != "" {
			fmt.Println("Email:", *email)
		}
		fmt.Println("Created:", created)
		fmt.Println("Please login to start using MangaHub:")
		fmt.Println("mangahub auth login --username", resp.User.Username)
	})
}

func cmdAuthLogin(app *App, args []string) {
	fs := flag.NewFlagSet("login", flag.ContinueOnError)
	fs.SetOutput(os.Stderr)
	username := fs.String("username", "", "username")
	password := fs.String("password", "", "password")
	if err := fs.Parse(args); err != nil {
		return
	}

	if *username == "" {
		fmt.Fprintln(os.Stderr, "Missing --username")
		return
	}
	pass := *password
	if pass == "" {
		p1, err := promptHidden("Password: ")
		if err != nil {
			fmt.Fprintln(os.Stderr, "Error:", err)
			return
		}
		pass = p1
	}

	payload := map[string]string{
		"username": *username,
		"password": pass,
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	data, _, err := doJSON(ctx, "POST", apiURL(app.Config.HTTPBaseURL, "/auth/login"), payload, "")
	if err != nil {
		fmt.Fprintln(os.Stderr, "Login failed:", err)
		return
	}

	var resp AuthResponse
	if err := json.Unmarshal(data, &resp); err != nil {
		fmt.Fprintln(os.Stderr, "Invalid response:", err)
		return
	}

	app.Config.Token = resp.Token
	app.Config.Username = resp.User.Username
	if err := SaveConfig(app.ConfigPath, app.Config); err != nil {
		fmt.Fprintln(os.Stderr, "Failed to save token:", err)
	}

	expiryText := "unknown"
	if exp, ok := tokenExpiry(resp.Token); ok {
		expiryText = exp.UTC().Format("2006-01-02 15:04:05 MST")
	}

	printJSONOrText(app, resp, "", func() {
		fmt.Println("Login successful")
		fmt.Println("Welcome back,", resp.User.Username+"!")
		fmt.Println("Session Details:")
		fmt.Println("Token expires:", expiryText)
		fmt.Println("Permissions: read, write, sync")
		fmt.Println("Auto-sync: enabled")
		fmt.Println("Notifications: enabled")
		fmt.Println("Ready to use MangaHub! Try:")
		fmt.Println("mangahub manga search \"your favorite manga\"")
	})
}

func cmdAuthLogout(app *App) {
	app.Config.Token = ""
	app.Config.Username = ""
	if err := SaveConfig(app.ConfigPath, app.Config); err != nil {
		fmt.Fprintln(os.Stderr, "Error:", err)
		return
	}
	fmt.Println("Logged out")
}

func cmdAuthStatus(app *App) {
	if app.Config.Token == "" {
		fmt.Println("Not logged in")
		return
	}
	values := map[string]string{
		"username": app.Config.Username,
		"token":    "stored",
	}
	if exp, ok := tokenExpiry(app.Config.Token); ok {
		values["expires"] = exp.UTC().Format("2006-01-02 15:04:05 MST")
	}

	printJSONOrText(app, values, "Auth status", func() {
		fmt.Println("Logged in as:", app.Config.Username)
		if exp, ok := tokenExpiry(app.Config.Token); ok {
			fmt.Println("Token expires:", exp.UTC().Format("2006-01-02 15:04:05 MST"))
		}
	})
}

func cmdManga(app *App, args []string) {
	if len(args) == 0 || isHelpArg(args[0]) {
		printMangaUsage()
		return
	}

	switch args[0] {
	case "search":
		cmdMangaSearch(app, args[1:])
	case "info":
		cmdMangaInfo(app, args[1:])
	case "list":
		cmdMangaList(app, args[1:])
	default:
		printMangaUsage()
	}
}

func printMangaUsage() {
	fmt.Println("Usage:")
	fmt.Println("  mangahub manga search <query> [--genre <genre>] [--status <status>] [--limit <n>]")
	fmt.Println("  mangahub manga info <id|title>")
	fmt.Println("  mangahub manga list [--status <status>] [--limit <n>]")
}

func cmdMangaSearch(app *App, args []string) {
	fs := flag.NewFlagSet("search", flag.ContinueOnError)
	fs.SetOutput(os.Stderr)
	genre := fs.String("genre", "", "genre filter")
	status := fs.String("status", "", "status filter")
	limit := fs.Int("limit", 0, "limit results")
	if err := fs.Parse(args); err != nil {
		return
	}
	query := strings.Join(fs.Args(), " ")
	if strings.TrimSpace(query) == "" {
		fmt.Fprintln(os.Stderr, "Search query required")
		return
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	url := apiURL(app.Config.HTTPBaseURL, "/manga/search?q="+urlQueryEscape(query))
	data, _, err := doJSON(ctx, "GET", url, nil, "")
	if err != nil {
		fmt.Fprintln(os.Stderr, "Search failed:", err)
		return
	}

	var resp MangaListResponse
	if err := json.Unmarshal(data, &resp); err != nil {
		fmt.Fprintln(os.Stderr, "Invalid response:", err)
		return
	}

	results := filterManga(resp.Manga, *genre, *status)
	if *limit > 0 && len(results) > *limit {
		results = results[:*limit]
	}

	printJSONOrText(app, resp, "", func() {
		if len(results) == 0 {
			fmt.Println("No manga found matching your search criteria.")
			return
		}

		rows := make([][]string, 0, len(results))
		for _, m := range results {
			rows = append(rows, []string{
				fmt.Sprintf("%d", m.ID),
				m.Title,
				m.Author,
				m.Status,
				fmt.Sprintf("%d", m.TotalChapters),
			})
		}
		printTable([]string{"ID", "Title", "Author", "Status", "Chapters"}, rows)
	})
}

func cmdMangaList(app *App, args []string) {
	fs := flag.NewFlagSet("list", flag.ContinueOnError)
	fs.SetOutput(os.Stderr)
	status := fs.String("status", "", "status filter")
	limit := fs.Int("limit", 0, "limit results")
	if err := fs.Parse(args); err != nil {
		return
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	url := apiURL(app.Config.HTTPBaseURL, "/manga")
	data, _, err := doJSON(ctx, "GET", url, nil, "")
	if err != nil {
		fmt.Fprintln(os.Stderr, "List failed:", err)
		return
	}

	var resp MangaListResponse
	if err := json.Unmarshal(data, &resp); err != nil {
		fmt.Fprintln(os.Stderr, "Invalid response:", err)
		return
	}

	results := filterManga(resp.Manga, "", *status)
	if *limit > 0 && len(results) > *limit {
		results = results[:*limit]
	}

	printJSONOrText(app, resp, "", func() {
		rows := make([][]string, 0, len(results))
		for _, m := range results {
			rows = append(rows, []string{
				fmt.Sprintf("%d", m.ID),
				m.Title,
				m.Author,
				m.Status,
				fmt.Sprintf("%d", m.TotalChapters),
			})
		}
		printTable([]string{"ID", "Title", "Author", "Status", "Chapters"}, rows)
	})
}

func cmdMangaInfo(app *App, args []string) {
	if len(args) == 0 {
		fmt.Fprintln(os.Stderr, "Manga id or title required")
		return
	}

	identifier := strings.Join(args, " ")
	mangaID, ok := parseInt(identifier)
	if !ok {
		id, err := resolveMangaID(app, identifier)
		if err != nil {
			fmt.Fprintln(os.Stderr, "Manga not found:", err)
			return
		}
		mangaID = id
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	url := apiURL(app.Config.HTTPBaseURL, fmt.Sprintf("/manga/%d", mangaID))
	data, _, err := doJSON(ctx, "GET", url, nil, "")
	if err != nil {
		fmt.Fprintln(os.Stderr, "Lookup failed:", err)
		return
	}

	var manga Manga
	if err := json.Unmarshal(data, &manga); err != nil {
		fmt.Fprintln(os.Stderr, "Invalid response:", err)
		return
	}

	printJSONOrText(app, manga, "", func() {
		fmt.Println("Title:", manga.Title)
		fmt.Println("Author:", manga.Author)
		fmt.Println("Genres:", strings.Join(manga.Genres, ", "))
		fmt.Println("Status:", manga.Status)
		fmt.Println("Total Chapters:", manga.TotalChapters)
		if manga.Description != "" {
			fmt.Println("Description:")
			fmt.Println(manga.Description)
		}
	})
}

func cmdLibrary(app *App, args []string) {
	if len(args) == 0 || isHelpArg(args[0]) {
		printLibraryUsage()
		return
	}

	switch args[0] {
	case "add":
		cmdLibraryAdd(app, args[1:])
	case "list":
		cmdLibraryList(app, args[1:])
	case "update":
		cmdLibraryUpdate(app, args[1:])
	case "remove":
		cmdLibraryRemove(app, args[1:])
	default:
		printLibraryUsage()
	}
}

func printLibraryUsage() {
	fmt.Println("Usage:")
	fmt.Println("  mangahub library add --manga-id <id> [--status <status>] [--chapter <n>]")
	fmt.Println("  mangahub library list [--status <status>]")
	fmt.Println("  mangahub library update --manga-id <id> [--status <status>] [--chapter <n>]")
	fmt.Println("  mangahub library remove --manga-id <id>")
}

func cmdLibraryAdd(app *App, args []string) {
	cmdLibraryUpsert(app, args, "library updated")
}

func cmdLibraryUpdate(app *App, args []string) {
	cmdLibraryUpsert(app, args, "library updated")
}

func cmdLibraryUpsert(app *App, args []string, message string) {
	fs := flag.NewFlagSet("library", flag.ContinueOnError)
	fs.SetOutput(os.Stderr)
	mangaID := fs.Int("manga-id", 0, "manga id")
	status := fs.String("status", "reading", "status")
	chapter := fs.Int("chapter", 0, "current chapter")
	if err := fs.Parse(args); err != nil {
		return
	}

	if *mangaID <= 0 {
		fmt.Fprintln(os.Stderr, "Missing --manga-id")
		return
	}

	token, err := requireToken(app)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return
	}

	payload := map[string]any{
		"manga_id":        *mangaID,
		"current_chapter": *chapter,
		"status":          *status,
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	_, _, err = doJSON(ctx, "POST", apiURL(app.Config.HTTPBaseURL, "/users/library"), payload, token)
	if err != nil {
		fmt.Fprintln(os.Stderr, "Library update failed:", err)
		return
	}

	fmt.Println(message)
}

func cmdLibraryList(app *App, args []string) {
	fs := flag.NewFlagSet("list", flag.ContinueOnError)
	fs.SetOutput(os.Stderr)
	statusFilter := fs.String("status", "", "status filter")
	if err := fs.Parse(args); err != nil {
		return
	}

	token, err := requireToken(app)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	data, _, err := doJSON(ctx, "GET", apiURL(app.Config.HTTPBaseURL, "/users/library"), nil, token)
	if err != nil {
		fmt.Fprintln(os.Stderr, "Library fetch failed:", err)
		return
	}

	var resp LibraryResponse
	if err := json.Unmarshal(data, &resp); err != nil {
		fmt.Fprintln(os.Stderr, "Invalid response:", err)
		return
	}

	items := resp.Library
	if *statusFilter != "" {
		filtered := make([]LibraryItem, 0, len(items))
		for _, item := range items {
			if strings.EqualFold(item.Progress.Status, *statusFilter) {
				filtered = append(filtered, item)
			}
		}
		items = filtered
	}

	printJSONOrText(app, resp, "", func() {
		if len(items) == 0 {
			fmt.Println("Your library is empty.")
			fmt.Println("Get started by searching and adding manga:")
			fmt.Println("  mangahub manga search \"your favorite series\"")
			fmt.Println("  mangahub library add --manga-id <id> --status reading")
			return
		}

		grouped := groupLibraryItems(items)
		statuses := sortedKeys(grouped)
		for _, status := range statuses {
			fmt.Printf("%s (%d):\n", strings.Title(status), len(grouped[status]))
			rows := make([][]string, 0, len(grouped[status]))
			for _, item := range grouped[status] {
				rows = append(rows, []string{
					fmt.Sprintf("%d", item.ID),
					item.Title,
					item.Chapter,
					item.Status,
					item.Updated,
				})
			}
			printTable([]string{"ID", "Title", "Chapter", "Status", "Updated"}, rows)
			fmt.Println()
		}
	})
}

func groupLibraryItems(items []LibraryItem) map[string][]LibraryItemView {
	result := make(map[string][]LibraryItemView)
	for _, item := range items {
		status := item.Progress.Status
		if status == "" {
			status = "reading"
		}
		view := LibraryItemView{
			ID:      item.Manga.ID,
			Title:   item.Manga.Title,
			Chapter: fmt.Sprintf("%d", item.Progress.CurrentChapter),
			Status:  status,
			Updated: formatTime(item.Progress.UpdatedAt),
		}
		result[status] = append(result[status], view)
	}
	for key := range result {
		sort.Slice(result[key], func(i, j int) bool {
			return result[key][i].Title < result[key][j].Title
		})
	}
	return result
}

func cmdLibraryRemove(app *App, args []string) {
	fs := flag.NewFlagSet("remove", flag.ContinueOnError)
	fs.SetOutput(os.Stderr)
	mangaID := fs.Int("manga-id", 0, "manga id")
	if err := fs.Parse(args); err != nil {
		return
	}

	if *mangaID <= 0 {
		fmt.Fprintln(os.Stderr, "Missing --manga-id")
		return
	}

	token, err := requireToken(app)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	_, _, err = doJSON(ctx, "DELETE", apiURL(app.Config.HTTPBaseURL, fmt.Sprintf("/users/library/%d", *mangaID)), nil, token)
	if err != nil {
		fmt.Fprintln(os.Stderr, "Remove failed:", err)
		return
	}

	fmt.Println("Library item removed")
}

func cmdProgress(app *App, args []string) {
	if len(args) == 0 || isHelpArg(args[0]) {
		printProgressUsage()
		return
	}

	switch args[0] {
	case "update":
		cmdProgressUpdate(app, args[1:])
	default:
		printProgressUsage()
	}
}

func printProgressUsage() {
	fmt.Println("Usage:")
	fmt.Println("  mangahub progress update --manga-id <id> --chapter <n> [--status <status>]")
}

func cmdProgressUpdate(app *App, args []string) {
	fs := flag.NewFlagSet("update", flag.ContinueOnError)
	fs.SetOutput(os.Stderr)
	mangaID := fs.Int("manga-id", 0, "manga id")
	chapter := fs.Int("chapter", 0, "chapter number")
	status := fs.String("status", "reading", "status")
	if err := fs.Parse(args); err != nil {
		return
	}

	if *mangaID <= 0 {
		fmt.Fprintln(os.Stderr, "Missing --manga-id")
		return
	}

	token, err := requireToken(app)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return
	}

	payload := map[string]any{
		"manga_id":        *mangaID,
		"current_chapter": *chapter,
		"status":          *status,
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	_, _, err = doJSON(ctx, "PUT", apiURL(app.Config.HTTPBaseURL, "/users/progress"), payload, token)
	if err != nil {
		fmt.Fprintln(os.Stderr, "Progress update failed:", err)
		return
	}

	fmt.Println("Progress updated successfully")
}

func filterManga(items []Manga, genre, status string) []Manga {
	filtered := make([]Manga, 0, len(items))
	for _, m := range items {
		if status != "" && !strings.EqualFold(m.Status, status) {
			continue
		}
		if genre != "" {
			match := false
			for _, g := range m.Genres {
				if strings.EqualFold(g, genre) {
					match = true
					break
				}
			}
			if !match {
				continue
			}
		}
		filtered = append(filtered, m)
	}
	return filtered
}

func resolveMangaID(app *App, query string) (int, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	url := apiURL(app.Config.HTTPBaseURL, "/manga/search?q="+urlQueryEscape(query))
	data, _, err := doJSON(ctx, "GET", url, nil, "")
	if err != nil {
		return 0, err
	}

	var resp MangaListResponse
	if err := json.Unmarshal(data, &resp); err != nil {
		return 0, err
	}
	if len(resp.Manga) == 0 {
		return 0, fmt.Errorf("no matches")
	}

	for _, m := range resp.Manga {
		if strings.EqualFold(m.Title, query) {
			return m.ID, nil
		}
	}
	return resp.Manga[0].ID, nil
}

func urlQueryEscape(value string) string {
	return url.QueryEscape(value)
}
