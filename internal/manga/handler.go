package manga

import (
	"database/sql"
	"encoding/json"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/gin-gonic/gin"

	"mangahub/internal/udp"
	"mangahub/pkg/models"
)

type Handler struct {
	db       *sql.DB
	notifier udp.Notifier
}

func NewHandler(db *sql.DB, notifier udp.Notifier) *Handler {
	return &Handler{db: db, notifier: notifier}
}

type MangaInput struct {
	Title         string   `json:"title" binding:"required"`
	Author        string   `json:"author" binding:"required"`
	Genres        []string `json:"genres" binding:"required,min=1,dive,required"`
	Status        string   `json:"status" binding:"required"`
	TotalChapters int      `json:"total_chapters" binding:"gte=0"`
	Description   string   `json:"description"`
}

type ReleaseRequest struct {
	Chapter int `json:"chapter" binding:"required,gt=0"`
}

func (h *Handler) RegisterPublicRoutes(rg *gin.RouterGroup) {
	rg.GET("", h.listManga)
	rg.GET("/:id", h.getManga)
	rg.GET("/search", h.searchManga)
}

func (h *Handler) RegisterProtectedRoutes(rg *gin.RouterGroup) {
	rg.POST("", h.createManga)
	rg.PUT("/:id", h.updateManga)
	rg.DELETE("/:id", h.deleteManga)
	rg.POST("/:id/release", h.releaseChapter)
}

func (h *Handler) listManga(c *gin.Context) {
	rows, err := h.db.Query(`SELECT id, title, author, genres, status, total_chapters, description FROM manga ORDER BY id`)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to fetch manga"})
		return
	}
	defer rows.Close()

	items := []models.Manga{}
	for rows.Next() {
		m, err := scanManga(rows.Scan)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to parse manga"})
			return
		}
		items = append(items, m)
	}

	c.JSON(http.StatusOK, gin.H{"manga": items, "count": len(items)})
}

func (h *Handler) getManga(c *gin.Context) {
	id, ok := parseID(c.Param("id"))
	if !ok {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid manga id"})
		return
	}

	row := h.db.QueryRow(`SELECT id, title, author, genres, status, total_chapters, description FROM manga WHERE id = ?`, id)
	m, err := scanManga(row.Scan)
	if err == sql.ErrNoRows {
		c.JSON(http.StatusNotFound, gin.H{"error": "manga not found"})
		return
	}
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "database error"})
		return
	}

	c.JSON(http.StatusOK, m)
}

func (h *Handler) createManga(c *gin.Context) {
	var input MangaInput
	if err := c.ShouldBindJSON(&input); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "validation failed", "details": err.Error()})
		return
	}

	genresJSON, err := json.Marshal(input.Genres)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid genres"})
		return
	}

	res, err := h.db.Exec(`INSERT INTO manga (title, author, genres, status, total_chapters, description) VALUES (?, ?, ?, ?, ?, ?)`, input.Title, input.Author, string(genresJSON), input.Status, input.TotalChapters, input.Description)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "failed to create manga"})
		return
	}

	id, _ := res.LastInsertId()
	m := models.Manga{
		ID:            int(id),
		Title:         input.Title,
		Author:        input.Author,
		Genres:        input.Genres,
		Status:        input.Status,
		TotalChapters: input.TotalChapters,
		Description:   input.Description,
	}

	c.JSON(http.StatusCreated, m)
}

func (h *Handler) updateManga(c *gin.Context) {
	id, ok := parseID(c.Param("id"))
	if !ok {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid manga id"})
		return
	}

	var input MangaInput
	if err := c.ShouldBindJSON(&input); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "validation failed", "details": err.Error()})
		return
	}

	genresJSON, err := json.Marshal(input.Genres)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid genres"})
		return
	}

	res, err := h.db.Exec(`UPDATE manga SET title = ?, author = ?, genres = ?, status = ?, total_chapters = ?, description = ? WHERE id = ?`, input.Title, input.Author, string(genresJSON), input.Status, input.TotalChapters, input.Description, id)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "failed to update manga"})
		return
	}

	affected, _ := res.RowsAffected()
	if affected == 0 {
		c.JSON(http.StatusNotFound, gin.H{"error": "manga not found"})
		return
	}

	m := models.Manga{
		ID:            id,
		Title:         input.Title,
		Author:        input.Author,
		Genres:        input.Genres,
		Status:        input.Status,
		TotalChapters: input.TotalChapters,
		Description:   input.Description,
	}
	c.JSON(http.StatusOK, m)
}

func (h *Handler) deleteManga(c *gin.Context) {
	id, ok := parseID(c.Param("id"))
	if !ok {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid manga id"})
		return
	}

	res, err := h.db.Exec(`DELETE FROM manga WHERE id = ?`, id)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to delete manga"})
		return
	}

	affected, _ := res.RowsAffected()
	if affected == 0 {
		c.JSON(http.StatusNotFound, gin.H{"error": "manga not found"})
		return
	}

	c.JSON(http.StatusOK, gin.H{"message": "manga deleted"})
}

func (h *Handler) searchManga(c *gin.Context) {
	query := strings.TrimSpace(c.Query("q"))
	if query == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "search query required"})
		return
	}

	pattern := "%" + query + "%"
	rows, err := h.db.Query(`SELECT id, title, author, genres, status, total_chapters, description FROM manga WHERE LOWER(title) LIKE LOWER(?) OR LOWER(author) LIKE LOWER(?) ORDER BY id`, pattern, pattern)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "search failed"})
		return
	}
	defer rows.Close()

	items := []models.Manga{}
	for rows.Next() {
		m, err := scanManga(rows.Scan)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to parse manga"})
			return
		}
		items = append(items, m)
	}

	c.JSON(http.StatusOK, gin.H{"manga": items, "count": len(items), "query": query})
}

func (h *Handler) releaseChapter(c *gin.Context) {
	id, ok := parseID(c.Param("id"))
	if !ok {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid manga id"})
		return
	}

	var input ReleaseRequest
	if err := c.ShouldBindJSON(&input); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "validation failed", "details": err.Error()})
		return
	}

	var title string
	err := h.db.QueryRow(`SELECT title FROM manga WHERE id = ?`, id).Scan(&title)
	if err == sql.ErrNoRows {
		c.JSON(http.StatusNotFound, gin.H{"error": "manga not found"})
		return
	}
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "database error"})
		return
	}

	userIDs, err := h.usersWithProgress(id)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to load subscribers"})
		return
	}

	h.notifyChapterRelease(userIDs, udp.ChapterRelease{
		Type:       "chapter_release",
		MangaID:    id,
		Title:      title,
		Chapter:    input.Chapter,
		ReleasedAt: time.Now().UTC().Format(time.RFC3339),
	})

	c.JSON(http.StatusOK, gin.H{
		"message":    "chapter release notified",
		"recipients": len(userIDs),
	})
}

func (h *Handler) usersWithProgress(mangaID int) ([]int, error) {
	rows, err := h.db.Query(`SELECT DISTINCT user_id FROM user_progress WHERE manga_id = ?`, mangaID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	userIDs := []int{}
	for rows.Next() {
		var userID int
		if err := rows.Scan(&userID); err != nil {
			return nil, err
		}
		userIDs = append(userIDs, userID)
	}

	return userIDs, nil
}

func (h *Handler) notifyChapterRelease(userIDs []int, release udp.ChapterRelease) {
	if h.notifier == nil {
		return
	}
	h.notifier.Notify(userIDs, release)
}

func scanManga(scan func(dest ...any) error) (models.Manga, error) {
	var m models.Manga
	var genresJSON string
	if err := scan(&m.ID, &m.Title, &m.Author, &genresJSON, &m.Status, &m.TotalChapters, &m.Description); err != nil {
		return m, err
	}
	if genresJSON != "" {
		if err := json.Unmarshal([]byte(genresJSON), &m.Genres); err != nil {
			return m, err
		}
	}
	return m, nil
}

func parseID(value string) (int, bool) {
	id, err := strconv.Atoi(value)
	if err != nil || id <= 0 {
		return 0, false
	}
	return id, true
}
