package main

import (
	"log"
	"net"
	"os"

	grpcsvc "mangahub/internal/grpc"
	"mangahub/pkg/database"
	pb "mangahub/proto"

	"google.golang.org/grpc"
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

	addr := getEnv("GRPC_ADDR", ":50051")
	lis, err := net.Listen("tcp", addr)
	if err != nil {
		log.Fatalf("failed to listen: %v", err)
	}

	grpcServer := grpc.NewServer()
	pb.RegisterMangaHubQueryServer(grpcServer, grpcsvc.NewServer(db))

	log.Printf("MangaHub gRPC server listening on %s", addr)
	if err := grpcServer.Serve(lis); err != nil {
		log.Fatalf("failed to serve: %v", err)
	}
}

func getEnv(key, fallback string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}
	return fallback
}
