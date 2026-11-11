package user

import (
	"database/sql"
	"encoding/json"
	"net/http"
	"strconv"
	"time"

	"github.com/gin-gonic/gin"

	"mangahub/internal/auth"
	"mangahub/internal/tcp"
	"mangahub/pkg/models"
)

type Handler struct {
	db          *sql.DB
	broadcaster tcp.Broadcaster
}

func NewHandler(db *sql.DB, broadcaster tcp.Broadcaster) *Handler {
	return &Handler{db: db, broadcaster: broadcaster}
}

type ProgressInput struct {
	MangaID        int    `json:"manga_id" binding:"required"`
	CurrentChapter int    `json:"current_chapter" binding:"gte=0"`
	Status         string `json:"status"`
}

type LibraryItem struct {
	Manga    models.Manga        `json:"manga"`
	Progress models.UserProgress `json:"progress"`
}

func (h *Handler) RegisterRoutes(rg *gin.RouterGroup) {
	rg.GET("", h.listLibrary)
	rg.GET("/:mangaId", h.getLibraryItem)
	rg.POST("", h.upsertLibraryItem)
	rg.DELETE("/:mangaId", h.deleteLibraryItem)
}

func (h *Handler) RegisterProgressRoutes(rg *gin.RouterGroup) {
	rg.PUT("", h.updateProgress)
}

func (h *Handler) listLibrary(c *gin.Context) {
	userID, ok := auth.GetUserID(c)
	if !ok {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "unauthorized"})
		return
	}

	rows, err := h.db.Query(`SELECT m.id, m.title, m.author, m.genres, m.status, m.total_chapters, m.description, up.current_chapter, up.status, up.updated_at FROM user_progress up JOIN manga m ON m.id = up.manga_id WHERE up.user_id = ? ORDER BY up.updated_at DESC`, userID)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to fetch library"})
		return
	}
	defer rows.Close()

	items := []LibraryItem{}
	for rows.Next() {
		item, err := scanLibraryItem(rows.Scan, userID)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to parse library"})
			return
		}
		items = append(items, item)
	}

	c.JSON(http.StatusOK, gin.H{"library": items, "count": len(items)})
}

func (h *Handler) getLibraryItem(c *gin.Context) {
	userID, ok := auth.GetUserID(c)
	if !ok {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "unauthorized"})
		return
	}

	mangaID, ok := parseID(c.Param("mangaId"))
	if !ok {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid manga id"})
		return
	}

	row := h.db.QueryRow(`SELECT m.id, m.title, m.author, m.genres, m.status, m.total_chapters, m.description, up.current_chapter, up.status, up.updated_at FROM user_progress up JOIN manga m ON m.id = up.manga_id WHERE up.user_id = ? AND up.manga_id = ?`, userID, mangaID)
	item, err := scanLibraryItem(row.Scan, userID)
	if err == sql.ErrNoRows {
		c.JSON(http.StatusNotFound, gin.H{"error": "library item not found"})
		return
	}
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "database error"})
		return
	}

	c.JSON(http.StatusOK, item)
}

func (h *Handler) upsertLibraryItem(c *gin.Context) {
	h.updateProgressInternal(c, "library updated")
}

func (h *Handler) updateProgress(c *gin.Context) {
	h.updateProgressInternal(c, "progress updated")
}

func (h *Handler) updateProgressInternal(c *gin.Context, message string) {
	userID, ok := auth.GetUserID(c)
	if !ok {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "unauthorized"})
		return
	}

	var input ProgressInput
	if err := c.ShouldBindJSON(&input); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "validation failed", "details": err.Error()})
		return
	}

	var exists bool
	if err := h.db.QueryRow(`SELECT EXISTS(SELECT 1 FROM manga WHERE id = ?)`, input.MangaID).Scan(&exists); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "database error"})
		return
	}
	if !exists {
		c.JSON(http.StatusNotFound, gin.H{"error": "manga not found"})
		return
	}

	status := input.Status
	if status == "" {
		status = "reading"
	}

	_, err := h.db.Exec(`INSERT INTO user_progress (user_id, manga_id, current_chapter, status, updated_at) VALUES (?, ?, ?, ?, CURRENT_TIMESTAMP) ON CONFLICT(user_id, manga_id) DO UPDATE SET current_chapter = excluded.current_chapter, status = excluded.status, updated_at = CURRENT_TIMESTAMP`, userID, input.MangaID, input.CurrentChapter, status)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "failed to update progress"})
		return
	}

	updatedAt := time.Now().UTC().Format(time.RFC3339)
	h.broadcastProgress(userID, input, status, updatedAt)

	c.JSON(http.StatusOK, gin.H{"message": message})
}

func (h *Handler) broadcastProgress(userID int, input ProgressInput, status, updatedAt string) {
	if h.broadcaster == nil {
		return
	}

	update := tcp.ProgressUpdate{
		Type:           "progress_update",
		UserID:         userID,
		MangaID:        input.MangaID,
		CurrentChapter: input.CurrentChapter,
		Status:         status,
		UpdatedAt:      updatedAt,
	}

	h.broadcaster.Broadcast(update)
}

func (h *Handler) deleteLibraryItem(c *gin.Context) {
	userID, ok := auth.GetUserID(c)
	if !ok {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "unauthorized"})
		return
	}

	mangaID, ok := parseID(c.Param("mangaId"))
	if !ok {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid manga id"})
		return
	}

	res, err := h.db.Exec(`DELETE FROM user_progress WHERE user_id = ? AND manga_id = ?`, userID, mangaID)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to delete library item"})
		return
	}

	affected, _ := res.RowsAffected()
	if affected == 0 {
		c.JSON(http.StatusNotFound, gin.H{"error": "library item not found"})
		return
	}

	c.JSON(http.StatusOK, gin.H{"message": "library item deleted"})
}

func scanLibraryItem(scan func(dest ...any) error, userID int) (LibraryItem, error) {
	var item LibraryItem
	var genresJSON string
	var currentChapter int
	var progressStatus string
	var updatedAt string

	err := scan(&item.Manga.ID, &item.Manga.Title, &item.Manga.Author, &genresJSON, &item.Manga.Status, &item.Manga.TotalChapters, &item.Manga.Description, &currentChapter, &progressStatus, &updatedAt)
	if err != nil {
		return item, err
	}

	if genresJSON != "" {
		if err := json.Unmarshal([]byte(genresJSON), &item.Manga.Genres); err != nil {
			return item, err
		}
	}

	item.Progress = models.UserProgress{
		UserID:         userID,
		MangaID:        item.Manga.ID,
		CurrentChapter: currentChapter,
		Status:         progressStatus,
		UpdatedAt:      updatedAt,
	}

	return item, nil
}

func parseID(value string) (int, bool) {
	id, err := strconv.Atoi(value)
	if err != nil || id <= 0 {
		return 0, false
	}
	return id, true
}
