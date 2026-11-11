package main

import (
	"bufio"
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"net"
	"net/http"
	"os"
	"strings"
	"time"

	pb "mangahub/proto"

	"github.com/gorilla/websocket"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
)

func cmdSync(app *App, args []string) {
	if len(args) == 0 || isHelpArg(args[0]) {
		printSyncUsage()
		return
	}

	switch args[0] {
	case "connect":
		cmdSyncConnect(app, args[1:])
	case "monitor":
		cmdSyncMonitor(app, args[1:])
	case "status":
		cmdSyncStatus(app)
	case "disconnect":
		fmt.Println("No persistent sync session to disconnect.")
	default:
		printSyncUsage()
	}
}

func printSyncUsage() {
	fmt.Println("Usage:")
	fmt.Println("  mangahub sync connect [--user-id <id>]")
	fmt.Println("  mangahub sync monitor [--user-id <id>]")
	fmt.Println("  mangahub sync status")
	fmt.Println("  mangahub sync disconnect")
}

func cmdSyncConnect(app *App, args []string) {
	userID := resolveUserID(app, args)
	if userID == 0 {
		fmt.Fprintln(os.Stderr, "User ID required (login or --user-id)")
		return
	}

	conn, err := net.DialTimeout("tcp", app.Config.TCPAddr, 5*time.Second)
	if err != nil {
		fmt.Fprintln(os.Stderr, "Connection failed:", err)
		return
	}
	defer conn.Close()

	if err := sendTCPRegister(conn, userID); err != nil {
		fmt.Fprintln(os.Stderr, "Register failed:", err)
		return
	}

	sessionID := fmt.Sprintf("sess_%d", time.Now().Unix())
	fmt.Println("Connected successfully")
	fmt.Println("Connection Details:")
	fmt.Println("Server:", app.Config.TCPAddr)
	fmt.Println("User:", app.Config.Username)
	fmt.Println("Session ID:", sessionID)
	fmt.Println("Connected at:", time.Now().UTC().Format("2006-01-02 15:04:05 MST"))
	fmt.Println("Sync Status:")
	fmt.Println("Auto-sync: enabled")
	fmt.Println("Conflict resolution: last_write_wins")
	fmt.Println("Devices connected: 1")
}

func cmdSyncMonitor(app *App, args []string) {
	userID := resolveUserID(app, args)
	if userID == 0 {
		fmt.Fprintln(os.Stderr, "User ID required (login or --user-id)")
		return
	}

	conn, err := net.DialTimeout("tcp", app.Config.TCPAddr, 5*time.Second)
	if err != nil {
		fmt.Fprintln(os.Stderr, "Connection failed:", err)
		return
	}
	defer conn.Close()

	if err := sendTCPRegister(conn, userID); err != nil {
		fmt.Fprintln(os.Stderr, "Register failed:", err)
		return
	}

	fmt.Println("Monitoring real-time sync updates... (Press Ctrl+C to exit)")
	scanner := bufio.NewScanner(conn)
	for scanner.Scan() {
		fmt.Println(scanner.Text())
	}
	if err := scanner.Err(); err != nil {
		fmt.Fprintln(os.Stderr, "Read error:", err)
	}
}

func cmdSyncStatus(app *App) {
	conn, err := net.DialTimeout("tcp", app.Config.TCPAddr, 5*time.Second)
	if err != nil {
		fmt.Println("TCP Sync Status: disconnected")
		fmt.Println("Server:", app.Config.TCPAddr)
		return
	}
	_ = conn.Close()
	fmt.Println("TCP Sync Status: connected")
	fmt.Println("Server:", app.Config.TCPAddr)
}

func sendTCPRegister(conn net.Conn, userID int) error {
	payload := map[string]any{
		"type":    "register",
		"user_id": userID,
	}
	encoder := json.NewEncoder(conn)
	return encoder.Encode(payload)
}

func resolveUserID(app *App, args []string) int {
	fs := flag.NewFlagSet("user", flag.ContinueOnError)
	fs.SetOutput(os.Stderr)
	userID := fs.Int("user-id", 0, "user id")
	_ = fs.Parse(args)
	if *userID > 0 {
		return *userID
	}
	if id, ok := parseUserIDFromToken(app.Config.Token); ok {
		return id
	}
	return 0
}

func cmdNotify(app *App, args []string) {
	if len(args) == 0 || isHelpArg(args[0]) {
		printNotifyUsage()
		return
	}

	switch args[0] {
	case "subscribe":
		cmdNotifySubscribe(app, args[1:])
	case "test":
		cmdNotifyTest(app, args[1:])
	default:
		printNotifyUsage()
	}
}

func printNotifyUsage() {
	fmt.Println("Usage:")
	fmt.Println("  mangahub notify subscribe [--user-id <id>]")
	fmt.Println("  mangahub notify test [--manga-id <id>] [--chapter <n>]")
}

func cmdNotifySubscribe(app *App, args []string) {
	userID := resolveUserID(app, args)
	if userID == 0 {
		fmt.Fprintln(os.Stderr, "User ID required (login or --user-id)")
		return
	}

	remote, err := net.ResolveUDPAddr("udp", app.Config.UDPAddr)
	if err != nil {
		fmt.Fprintln(os.Stderr, "Invalid UDP address:", err)
		return
	}

	conn, err := net.DialUDP("udp", nil, remote)
	if err != nil {
		fmt.Fprintln(os.Stderr, "UDP connect failed:", err)
		return
	}
	defer conn.Close()

	payload, _ := json.Marshal(map[string]any{"type": "register", "user_id": userID})
	if _, err := conn.Write(payload); err != nil {
		fmt.Fprintln(os.Stderr, "Register failed:", err)
		return
	}

	fmt.Println("Subscribed to release notifications. Listening...")
	buffer := make([]byte, 2048)
	for {
		n, _, err := conn.ReadFromUDP(buffer)
		if err != nil {
			fmt.Fprintln(os.Stderr, "UDP read error:", err)
			return
		}
		fmt.Println(string(buffer[:n]))
	}
}

func cmdNotifyTest(app *App, args []string) {
	fs := flag.NewFlagSet("test", flag.ContinueOnError)
	fs.SetOutput(os.Stderr)
	mangaID := fs.Int("manga-id", 0, "manga id")
	chapter := fs.Int("chapter", 0, "chapter number")
	if err := fs.Parse(args); err != nil {
		return
	}

	if *mangaID > 0 && *chapter > 0 {
		token, err := requireToken(app)
		if err != nil {
			fmt.Fprintln(os.Stderr, err)
			return
		}
		payload := map[string]any{"chapter": *chapter}
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		_, _, err = doJSON(ctx, "POST", apiURL(app.Config.HTTPBaseURL, fmt.Sprintf("/manga/%d/release", *mangaID)), payload, token)
		if err != nil {
			fmt.Fprintln(os.Stderr, "Release notify failed:", err)
			return
		}
		fmt.Println("Release notification sent")
		return
	}

	fmt.Println("Notify test ready. Use --manga-id and --chapter to trigger a release.")
}

func cmdGRPC(app *App, args []string) {
	if len(args) == 0 || isHelpArg(args[0]) {
		printGRPCUsage()
		return
	}

	if args[0] != "manga" {
		printGRPCUsage()
		return
	}
	cmdGRPCManga(app, args[1:])
}

func printGRPCUsage() {
	fmt.Println("Usage:")
	fmt.Println("  mangahub grpc manga get --id <id>")
	fmt.Println("  mangahub grpc manga search --query <text>")
}

func cmdGRPCManga(app *App, args []string) {
	if len(args) == 0 || isHelpArg(args[0]) {
		printGRPCUsage()
		return
	}

	switch args[0] {
	case "get":
		cmdGRPCMangaGet(app, args[1:])
	case "search":
		cmdGRPCMangaSearch(app, args[1:])
	default:
		printGRPCUsage()
	}
}

func grpcClient(app *App) (pb.MangaHubQueryClient, *grpc.ClientConn, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	conn, err := grpc.DialContext(ctx, app.Config.GRPCAddr, grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		return nil, nil, err
	}
	return pb.NewMangaHubQueryClient(conn), conn, nil
}

func cmdGRPCMangaGet(app *App, args []string) {
	fs := flag.NewFlagSet("get", flag.ContinueOnError)
	fs.SetOutput(os.Stderr)
	id := fs.Int("id", 0, "manga id")
	if err := fs.Parse(args); err != nil {
		return
	}
	if *id <= 0 {
		fmt.Fprintln(os.Stderr, "Missing --id")
		return
	}

	client, conn, err := grpcClient(app)
	if err != nil {
		fmt.Fprintln(os.Stderr, "gRPC connect failed:", err)
		return
	}
	defer conn.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	resp, err := client.GetManga(ctx, &pb.GetMangaRequest{Id: int32(*id)})
	if err != nil {
		fmt.Fprintln(os.Stderr, "gRPC error:", err)
		return
	}

	printJSONOrText(app, resp, "", func() {
		fmt.Println("ID:", resp.Manga.Id)
		fmt.Println("Title:", resp.Manga.Title)
		fmt.Println("Author:", resp.Manga.Author)
		fmt.Println("Status:", resp.Manga.Status)
		fmt.Println("Total Chapters:", resp.Manga.TotalChapters)
	})
}

func cmdGRPCMangaSearch(app *App, args []string) {
	fs := flag.NewFlagSet("search", flag.ContinueOnError)
	fs.SetOutput(os.Stderr)
	query := fs.String("query", "", "query")
	if err := fs.Parse(args); err != nil {
		return
	}
	if *query == "" {
		fmt.Fprintln(os.Stderr, "Missing --query")
		return
	}

	client, conn, err := grpcClient(app)
	if err != nil {
		fmt.Fprintln(os.Stderr, "gRPC connect failed:", err)
		return
	}
	defer conn.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	resp, err := client.ListManga(ctx, &pb.ListMangaRequest{Page: 1, PageSize: 100})
	if err != nil {
		fmt.Fprintln(os.Stderr, "gRPC error:", err)
		return
	}

	queryLower := strings.ToLower(*query)
	rows := make([][]string, 0)
	for _, m := range resp.Manga {
		if strings.Contains(strings.ToLower(m.Title), queryLower) || strings.Contains(strings.ToLower(m.Author), queryLower) {
			rows = append(rows, []string{
				fmt.Sprintf("%d", m.Id),
				m.Title,
				m.Author,
				m.Status,
				fmt.Sprintf("%d", m.TotalChapters),
			})
		}
	}

	printJSONOrText(app, resp, "", func() {
		if len(rows) == 0 {
			fmt.Println("No manga found.")
			return
		}
		printTable([]string{"ID", "Title", "Author", "Status", "Chapters"}, rows)
	})
}

func cmdChat(app *App, args []string) {
	if len(args) == 0 || isHelpArg(args[0]) {
		printChatUsage()
		return
	}

	switch args[0] {
	case "join":
		cmdChatJoin(app, args[1:])
	case "history":
		fmt.Println("Chat history is not available in the current server implementation.")
	default:
		printChatUsage()
	}
}

func printChatUsage() {
	fmt.Println("Usage:")
	fmt.Println("  mangahub chat join --username <name> [--manga-id <id>] [--room <room>]")
	fmt.Println("  mangahub chat history")
}

func cmdChatJoin(app *App, args []string) {
	fs := flag.NewFlagSet("join", flag.ContinueOnError)
	fs.SetOutput(os.Stderr)
	username := fs.String("username", "", "username")
	mangaID := fs.String("manga-id", "", "manga id")
	room := fs.String("room", "general", "room")
	token := fs.String("token", "", "JWT token for Authorization header")
	if err := fs.Parse(args); err != nil {
		return
	}

	if *username == "" {
		fmt.Fprintln(os.Stderr, "Missing --username")
		return
	}
	roomName := *room
	if *mangaID != "" {
		roomName = *mangaID
	}

	wsURL, err := buildWSURL(app.Config.WSURL, *username, roomName)
	if err != nil {
		fmt.Fprintln(os.Stderr, "Invalid WS URL:", err)
		return
	}

	// Prepare headers for WebSocket dial. If a token is provided, send it
	// as an Authorization header so the server can validate the JWT.
	headers := http.Header{}
	tokenVal := *token
	if tokenVal == "" && app.Config != nil && app.Config.Token != "" {
		tokenVal = app.Config.Token
	}
	if tokenVal != "" {
		headers.Set("Authorization", "Bearer "+tokenVal)
	}
	conn, _, err := websocket.DefaultDialer.Dial(wsURL, headers)
	if err != nil {
		fmt.Fprintln(os.Stderr, "WebSocket connect failed:", err)
		return
	}
	defer conn.Close()

	fmt.Println("Connected to chat. Type /help for commands or /quit to leave.")

	go func() {
		for {
			_, msg, err := conn.ReadMessage()
			if err != nil {
				return
			}
			fmt.Println(string(msg))
		}
	}()

	scanner := bufio.NewScanner(os.Stdin)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}
		if strings.HasPrefix(line, "/") {
			if handleChatCommand(conn, line) {
				return
			}
			continue
		}

		msg := map[string]string{"type": "chat", "content": line}
		if err := conn.WriteJSON(msg); err != nil {
			fmt.Fprintln(os.Stderr, "Send failed:", err)
			return
		}
	}
}

func handleChatCommand(conn *websocket.Conn, line string) bool {
	parts := strings.Fields(line)
	cmd := strings.TrimPrefix(parts[0], "/")
	switch cmd {
	case "help":
		fmt.Println("Chat Commands:")
		fmt.Println("/help, /users, /rooms, /quit")
		fmt.Println("/pm <user> <msg>")
		fmt.Println("/manga <id>")
	case "users":
		_ = conn.WriteJSON(map[string]string{"type": "list_users"})
	case "rooms":
		_ = conn.WriteJSON(map[string]string{"type": "list_rooms"})
	case "pm":
		if len(parts) < 3 {
			fmt.Println("Usage: /pm <user> <msg>")
			return false
		}
		recipient := parts[1]
		content := strings.Join(parts[2:], " ")
		_ = conn.WriteJSON(map[string]string{"type": "dm", "recipient": recipient, "content": content})
	case "manga":
		if len(parts) < 2 {
			fmt.Println("Usage: /manga <id>")
			return false
		}
		_ = conn.WriteJSON(map[string]string{"type": "join_room", "content": parts[1]})
	case "quit":
		_ = conn.WriteMessage(websocket.CloseMessage, websocket.FormatCloseMessage(websocket.CloseNormalClosure, "bye"))
		return true
	default:
		fmt.Println("Unknown command:", cmd)
	}
	return false
}

func buildWSURL(baseURL, username, room string) (string, error) {
	if !strings.Contains(baseURL, "?") {
		return fmt.Sprintf("%s?username=%s&room=%s", baseURL, urlQueryEscape(username), urlQueryEscape(room)), nil
	}
	return baseURL, nil
}
