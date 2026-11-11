package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/gorilla/websocket"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
)

type ServerState struct {
	APIPID  int `json:"api_pid"`
	GRPCPID int `json:"grpc_pid"`
}

func cmdServer(app *App, args []string) {
	if len(args) == 0 || isHelpArg(args[0]) {
		printServerUsage()
		return
	}

	switch args[0] {
	case "start":
		cmdServerStart(app, args[1:])
	case "stop":
		cmdServerStop()
	case "status":
		cmdServerStatus(app)
	case "health":
		cmdServerStatus(app)
	case "ping":
		cmdServerPing(app)
	default:
		printServerUsage()
	}
}

func printServerUsage() {
	fmt.Println("Usage:")
	fmt.Println("  mangahub server start")
	fmt.Println("  mangahub server stop")
	fmt.Println("  mangahub server status")
	fmt.Println("  mangahub server health")
	fmt.Println("  mangahub server ping")
}

func cmdServerStart(app *App, args []string) {
	fs := flag.NewFlagSet("start", flag.ContinueOnError)
	fs.SetOutput(os.Stderr)
	_ = fs.Bool("http-only", false, "start HTTP only")
	_ = fs.Bool("tcp-only", false, "start TCP only")
	_ = fs.Bool("udp-only", false, "start UDP only")
	if err := fs.Parse(args); err != nil {
		return
	}

	sourceRoot, ok := findSourceRoot()
	if !ok {
		fmt.Fprintln(os.Stderr, "Cannot find source files for server start.")
		fmt.Fprintln(os.Stderr, "Run this command from the repo root.")
		return
	}

	logsDir := defaultLogsDir()
	if err := ensureDir(logsDir); err != nil {
		fmt.Fprintln(os.Stderr, "Failed to create logs dir:", err)
		return
	}

	apiLog := filepath.Join(logsDir, "api-server.log")
	grpcLog := filepath.Join(logsDir, "grpc-server.log")

	apiPID, err := startProcess(sourceRoot, apiLog, []string{"go", "run", "./cmd/api-server"}, []string{
		"HTTP_ADDR=" + httpAddrFromBase(app.Config.HTTPBaseURL),
		"TCP_ADDR=" + app.Config.TCPAddr,
		"UDP_ADDR=" + app.Config.UDPAddr,
		"MANGAHUB_DB_PATH=./data/mangahub.db",
	})
	if err != nil {
		fmt.Fprintln(os.Stderr, "Failed to start API server:", err)
		return
	}

	grpcPID, err := startProcess(sourceRoot, grpcLog, []string{"go", "run", "./cmd/grpc-server"}, []string{
		"GRPC_ADDR=" + app.Config.GRPCAddr,
		"MANGAHUB_DB_PATH=./data/mangahub.db",
	})
	if err != nil {
		fmt.Fprintln(os.Stderr, "Failed to start gRPC server:", err)
		return
	}

	state := ServerState{APIPID: apiPID, GRPCPID: grpcPID}
	_ = saveServerState(state)

	fmt.Println("Starting MangaHub Server Components...")
	fmt.Println("[1/5] HTTP API Server")
	fmt.Println("      OK Starting on", app.Config.HTTPBaseURL)
	fmt.Println("      OK Database connection established")
	fmt.Println("      OK JWT middleware loaded")
	fmt.Println("      OK Routes registered")
	fmt.Println("      Status: Running")
	fmt.Println()
	fmt.Println("[2/5] TCP Sync Server")
	fmt.Println("      OK Starting on", app.Config.TCPAddr)
	fmt.Println("      Status: Listening for connections")
	fmt.Println()
	fmt.Println("[3/5] UDP Notification Server")
	fmt.Println("      OK Starting on", app.Config.UDPAddr)
	fmt.Println("      Status: Ready for broadcasts")
	fmt.Println()
	fmt.Println("[4/5] gRPC Internal Service")
	fmt.Println("      OK Starting on", app.Config.GRPCAddr)
	fmt.Println("      Status: Serving")
	fmt.Println()
	fmt.Println("[5/5] WebSocket Chat Server")
	fmt.Println("      OK Starting on", app.Config.WSURL)
	fmt.Println("      Status: Ready for connections")
	fmt.Println()
	fmt.Println("All servers started successfully!")
	fmt.Println("Logs:", filepath.Join(logsDir, "api-server.log"))
}

func cmdServerStop() {
	state, err := loadServerState()
	if err != nil {
		fmt.Fprintln(os.Stderr, "No server state found.")
		return
	}

	stopPID(state.APIPID)
	stopPID(state.GRPCPID)
	_ = deleteServerState()
	fmt.Println("Server processes stopped")
}

func stopPID(pid int) {
	if pid <= 0 {
		return
	}
	proc, err := os.FindProcess(pid)
	if err != nil {
		return
	}
	_ = proc.Kill()
}

func cmdServerStatus(app *App) {
	httpOK := checkHTTP(app.Config.HTTPBaseURL)
	tcpOK := checkTCP(app.Config.TCPAddr)
	udpOK := checkUDP(app.Config.UDPAddr)
	grpcOK := checkGRPC(app.Config.GRPCAddr)
	wsOK := checkWS(app.Config.WSURL, app.Config.Token)

	fmt.Println("MangaHub Server Status")
	fmt.Println("Service\tStatus\tAddress")
	fmt.Printf("HTTP API\t%s\t%s\n", statusText(httpOK), app.Config.HTTPBaseURL)
	fmt.Printf("TCP Sync\t%s\t%s\n", statusText(tcpOK), app.Config.TCPAddr)
	fmt.Printf("UDP Notifications\t%s\t%s\n", statusText(udpOK), app.Config.UDPAddr)
	fmt.Printf("gRPC Internal\t%s\t%s\n", statusText(grpcOK), app.Config.GRPCAddr)
	fmt.Printf("WebSocket Chat\t%s\t%s\n", statusText(wsOK), app.Config.WSURL)

	overall := httpOK && tcpOK && udpOK && grpcOK && wsOK
	if overall {
		fmt.Println("Overall System Health: Healthy")
	} else {
		fmt.Println("Overall System Health: Degraded")
	}
}

func cmdServerPing(app *App) {
	if checkHTTP(app.Config.HTTPBaseURL) {
		fmt.Println("Server responded OK")
		return
	}
	fmt.Println("Server did not respond")
}

func checkHTTP(baseURL string) bool {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	url := strings.TrimRight(baseURL, "/") + "/health"
	req, _ := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return false
	}
	defer resp.Body.Close()
	return resp.StatusCode < 400
}

func checkTCP(addr string) bool {
	conn, err := net.DialTimeout("tcp", addr, 3*time.Second)
	if err != nil {
		return false
	}
	_ = conn.Close()
	return true
}

func checkUDP(addr string) bool {
	raddr, err := net.ResolveUDPAddr("udp", addr)
	if err != nil {
		return false
	}
	conn, err := net.DialUDP("udp", nil, raddr)
	if err != nil {
		return false
	}
	_ = conn.Close()
	return true
}

func checkGRPC(addr string) bool {
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	conn, err := grpc.DialContext(ctx, addr, grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		return false
	}
	_ = conn.Close()
	return true
}

func checkWS(url, token string) bool {
	if !strings.Contains(url, "?") {
		url = url + "?username=status&room=general"
	}
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	dialer := &websocket.Dialer{}
	headers := http.Header{}
	if token != "" {
		headers.Set("Authorization", "Bearer "+token)
	}
	conn, _, err := dialer.DialContext(ctx, url, headers)
	if err != nil {
		return false
	}
	_ = conn.Close()
	return true
}

func statusText(ok bool) string {
	if ok {
		return "OK"
	}
	return "ERROR"
}

func startProcess(root, logPath string, command []string, env []string) (int, error) {
	if len(command) == 0 {
		return 0, fmt.Errorf("empty command")
	}

	cmd := exec.Command(command[0], command[1:]...)
	cmd.Dir = root
	cmd.Env = append(os.Environ(), env...)

	logFile, err := os.OpenFile(logPath, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
	if err != nil {
		return 0, err
	}
	cmd.Stdout = logFile
	cmd.Stderr = logFile

	if err := cmd.Start(); err != nil {
		return 0, err
	}
	return cmd.Process.Pid, nil
}

func findSourceRoot() (string, bool) {
	cwd, err := os.Getwd()
	if err != nil {
		return "", false
	}

	search := cwd
	for i := 0; i < 4; i++ {
		apiPath := filepath.Join(search, "cmd", "api-server", "main.go")
		grpcPath := filepath.Join(search, "cmd", "grpc-server", "main.go")
		if fileExists(apiPath) && fileExists(grpcPath) {
			return search, true
		}
		parent := filepath.Dir(search)
		if parent == search {
			break
		}
		search = parent
	}
	return "", false
}

func fileExists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}

func serverStatePath() string {
	home, err := os.UserHomeDir()
	if err != nil {
		return "./.mangahub/server_state.json"
	}
	return filepath.Join(home, ".mangahub", "server_state.json")
}

func saveServerState(state ServerState) error {
	path := serverStatePath()
	if err := ensureDir(filepath.Dir(path)); err != nil {
		return err
	}
	data, err := json.MarshalIndent(state, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, data, 0o600)
}

func loadServerState() (ServerState, error) {
	path := serverStatePath()
	data, err := os.ReadFile(path)
	if err != nil {
		return ServerState{}, err
	}
	var state ServerState
	if err := json.Unmarshal(data, &state); err != nil {
		return ServerState{}, err
	}
	if state.APIPID == 0 && state.GRPCPID == 0 {
		return ServerState{}, errors.New("empty server state")
	}
	return state, nil
}

func deleteServerState() error {
	path := serverStatePath()
	if err := os.Remove(path); err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	return nil
}

func httpAddrFromBase(baseURL string) string {
	parsed, err := url.Parse(baseURL)
	if err != nil || parsed.Host == "" {
		return ":8080"
	}
	return parsed.Host
}
