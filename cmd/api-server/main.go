package main

import (
	"context"
	"database/sql"
	"log"
	"net/http"
	"os"
	"strconv"
	"time"

	"github.com/gin-gonic/gin"

	"mangahub/internal/auth"
	"mangahub/internal/manga"
	"mangahub/internal/tcp"
	"mangahub/internal/udp"
	"mangahub/internal/user"
	"mangahub/internal/websocket"
	"mangahub/pkg/database"
)

func main() {
	dbPath := getEnv("MANGAHUB_DB_PATH", "./data/mangahub.db")
	if err := os.MkdirAll("data", 0o755); err != nil {
		log.Fatal(err)
	}

	db, err := database.OpenSQLite(dbPath)
	if err != nil {
		log.Fatal(err)
	}
	defer db.Close()

	if err := database.Migrate(db); err != nil {
		log.Fatal(err)
	}

	if shouldSeed() {
		seedOpts := manga.SeedOptions{
			Limit:       getEnvInt("SEED_LIMIT", 100),
			ScrapeLimit: getEnvInt("SEED_SCRAPE_LIMIT", 20),
			RateLimit:   time.Second,
			BaseURL:     getEnv("MANGADEX_BASE_URL", "https://api.mangadex.org"),
			Language:    getEnv("MANGADEX_LANGUAGE", "en"),
			Logger:      log.Default(),
		}
		if _, err := manga.SeedFromMangaDex(context.Background(), db, seedOpts); err != nil {
			log.Printf("Seed error: %v", err)
		}
	}

	progressAddr := getEnv("TCP_ADDR", ":9090")
	progressServer := tcp.NewProgressSyncServer(progressAddr, log.Default())
	go func() {
		if err := progressServer.Start(); err != nil {
			log.Printf("TCP progress sync stopped: %v", err)
		}
	}()

	udpAddr := getEnv("UDP_ADDR", ":7071")
	notificationServer := udp.NewNotificationServer(udpAddr, log.Default())
	go func() {
		if err := notificationServer.Start(); err != nil {
			log.Printf("UDP notification server stopped: %v", err)
		}
	}()

	hub := websocket.NewHub()
	go hub.Run()

	jwtSecret := []byte(getEnv("JWT_SECRET", "dev-secret-change-me"))
	router := buildRouter(db, jwtSecret, 24*time.Hour, progressServer, notificationServer, hub)

	addr := getEnv("HTTP_ADDR", ":8080")
	if err := router.Run(addr); err != nil {
		log.Fatal(err)
	}
}

func getEnv(key, fallback string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}
	return fallback
}

func getEnvInt(key string, fallback int) int {
	value := os.Getenv(key)
	if value == "" {
		return fallback
	}
	if n, err := strconv.Atoi(value); err == nil {
		return n
	}
	return fallback
}

func shouldSeed() bool {
	value := os.Getenv("SEED_MANGA")
	if value == "" {
		return false
	}
	if value == "1" || value == "true" || value == "TRUE" {
		return true
	}
	return false
}

func buildRouter(db *sql.DB, jwtSecret []byte, tokenTTL time.Duration, progressBroadcaster tcp.Broadcaster, udpNotifier udp.Notifier, wsHub *websocket.Hub) *gin.Engine {
	r := gin.Default()
	r.GET("/health", func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{"status": "ok"})
	})

	authHandler := auth.NewHandler(db, jwtSecret, tokenTTL)
	authHandler.RegisterRoutes(r.Group("/auth"))

	mangaHandler := manga.NewHandler(db, udpNotifier)
	mangaHandler.RegisterPublicRoutes(r.Group("/manga"))

	protected := r.Group("/")
	protected.Use(auth.AuthMiddleware(jwtSecret))

	mangaHandler.RegisterProtectedRoutes(protected.Group("/manga"))

	userHandler := user.NewHandler(db, progressBroadcaster)
	userHandler.RegisterRoutes(protected.Group("/users/library"))
	userHandler.RegisterProgressRoutes(protected.Group("/users/progress"))

	if wsHub != nil {
		// Protect WebSocket endpoint with JWT middleware so clients must
		// present a valid Authorization: Bearer <token> header when
		// upgrading the connection.
		protected.GET("/ws", func(c *gin.Context) {
			websocket.ServeWS(wsHub, c.Writer, c.Request)
		})
	}

	return r
}
