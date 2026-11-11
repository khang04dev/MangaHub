package grpc

import (
	"context"
	"database/sql"
	"encoding/json"
	"log"

	pb "mangahub/proto"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

type Server struct {
	pb.UnimplementedMangaHubQueryServer
	db *sql.DB
}

func NewServer(db *sql.DB) *Server {
	return &Server{db: db}
}

func (s *Server) GetManga(ctx context.Context, req *pb.GetMangaRequest) (*pb.GetMangaResponse, error) {
	log.Printf("GetManga: id=%d", req.Id)

	row := s.db.QueryRowContext(ctx, `SELECT id, title, author, genres, status, total_chapters, description FROM manga WHERE id = ?`, req.Id)
	m, err := scanManga(row.Scan)
	if err == sql.ErrNoRows {
		return nil, status.Errorf(codes.NotFound, "manga not found: id=%d", req.Id)
	}
	if err != nil {
		return nil, status.Errorf(codes.Internal, "database error: %v", err)
	}

	return &pb.GetMangaResponse{Manga: m}, nil
}

func (s *Server) ListManga(ctx context.Context, req *pb.ListMangaRequest) (*pb.ListMangaResponse, error) {
	page := req.Page
	if page < 1 {
		page = 1
	}
	pageSize := req.PageSize
	if pageSize < 1 || pageSize > 100 {
		pageSize = 20
	}
	offset := (page - 1) * pageSize

	var total int32
	if err := s.db.QueryRowContext(ctx, "SELECT COUNT(*) FROM manga").Scan(&total); err != nil {
		return nil, status.Errorf(codes.Internal, "failed to count manga: %v", err)
	}

	rows, err := s.db.QueryContext(ctx, `SELECT id, title, author, genres, status, total_chapters, description FROM manga ORDER BY id LIMIT ? OFFSET ?`, pageSize, offset)
	if err != nil {
		return nil, status.Errorf(codes.Internal, "failed to list manga: %v", err)
	}
	defer rows.Close()

	items := make([]*pb.Manga, 0)
	for rows.Next() {
		m, err := scanManga(rows.Scan)
		if err != nil {
			return nil, status.Errorf(codes.Internal, "failed to parse manga: %v", err)
		}
		items = append(items, m)
	}

	return &pb.ListMangaResponse{Manga: items, Total: total, Page: page, PageSize: pageSize}, nil
}

func (s *Server) GetUserProgress(ctx context.Context, req *pb.GetUserProgressRequest) (*pb.GetUserProgressResponse, error) {
	row := s.db.QueryRowContext(ctx, `SELECT user_id, manga_id, current_chapter, status, updated_at FROM user_progress WHERE user_id = ? AND manga_id = ?`, req.UserId, req.MangaId)

	progress, err := scanProgress(row.Scan)
	if err == sql.ErrNoRows {
		return nil, status.Errorf(codes.NotFound, "progress not found: user=%d manga=%d", req.UserId, req.MangaId)
	}
	if err != nil {
		return nil, status.Errorf(codes.Internal, "database error: %v", err)
	}

	return &pb.GetUserProgressResponse{Progress: progress}, nil
}

func (s *Server) ListUserProgress(ctx context.Context, req *pb.ListUserProgressRequest) (*pb.ListUserProgressResponse, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT user_id, manga_id, current_chapter, status, updated_at FROM user_progress WHERE user_id = ? ORDER BY updated_at DESC`, req.UserId)
	if err != nil {
		return nil, status.Errorf(codes.Internal, "failed to list progress: %v", err)
	}
	defer rows.Close()

	items := make([]*pb.Progress, 0)
	for rows.Next() {
		progress, err := scanProgress(rows.Scan)
		if err != nil {
			return nil, status.Errorf(codes.Internal, "failed to parse progress: %v", err)
		}
		items = append(items, progress)
	}

	return &pb.ListUserProgressResponse{Progress: items, Count: int32(len(items))}, nil
}

func scanManga(scan func(dest ...any) error) (*pb.Manga, error) {
	var manga pb.Manga
	var genresJSON string
	if err := scan(&manga.Id, &manga.Title, &manga.Author, &genresJSON, &manga.Status, &manga.TotalChapters, &manga.Description); err != nil {
		return nil, err
	}

	if genresJSON != "" {
		if err := json.Unmarshal([]byte(genresJSON), &manga.Genres); err != nil {
			return nil, err
		}
	}

	return &manga, nil
}

func scanProgress(scan func(dest ...any) error) (*pb.Progress, error) {
	var progress pb.Progress
	if err := scan(&progress.UserId, &progress.MangaId, &progress.CurrentChapter, &progress.Status, &progress.UpdatedAt); err != nil {
		return nil, err
	}

	return &progress, nil
}
