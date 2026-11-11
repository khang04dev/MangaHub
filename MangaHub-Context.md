# MangaHub System Context

## Project Overview
MangaHub is a manga tracking system using five protocols: **HTTP, TCP, UDP, gRPC, and WebSocket** [8].

## Database Schema (SQLite)
Use the following tables for data persistence [7]:
- **users**: `id (PK)`, `username (Unique)`, `password_hash`, `created_at`.
- **manga**: `id (PK)`, `title`, `author`, `genres (JSON array)`, `status`, `total_chapters`, `description`.
- **user_progress**: `user_id`, `manga_id`, `current_chapter`, `status`, `updated_at`.

## Core Logic Mapping (Reference Labs)
Refer to the existing files in the workspace for these patterns [9-11]:
1. **HTTP/Gin (Lab 06):** Use for REST endpoints like `/auth/login`, `/manga/search`, and `/users/library` [2].
2. **TCP Sync (Lab 03):** Use for real-time progress broadcasting between devices [3].
3. **UDP (Lab 03):** Use for chapter release notifications to registered clients [4].
4. **WebSocket (WS Lab):** Use the Hub/Client pattern for real-time community chat [5].
5. **gRPC (gRPC Lab):** Use for internal service-to-service communication [6].

## Expected Project Structure
Apply this modular Go structure [12]:
- `cmd/`: Entry points for `api-server`, `tcp-server`, `udp-server`, `grpc-server`.
- `internal/`: Business logic for `auth`, `manga`, `user`, and protocol handlers.
- `pkg/`: Shared `models`, `database` utilities, and `utils`.
- `proto/`: Protocol Buffer definitions for gRPC.
- `data/`: JSON files for initial manga data collection [13].

## Development Rules
- **Concurrency:** Always use goroutines for TCP/WebSocket connections [3, 14].
- **Error Handling:** Return proper HTTP status codes and gRPC error codes [15, 16].
- **Validation:** Use Gin's binding tags for input validation [17, 18].
- **Security:** Implement JWT middleware for protected routes [2].