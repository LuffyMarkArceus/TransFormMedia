package http

import (
	"log"
	"net/http"
	"time"

	"universal-media-service/core/auth"
	"universal-media-service/core/media"

	"github.com/gin-gonic/gin"
)

type ShareHandler struct {
	repo        media.Repository
	shareSecret string
	shareTTL    int
}

func NewShareHandler(repo media.Repository, secret string, ttl int) *ShareHandler {
	return &ShareHandler{
		repo:        repo,
		shareSecret: secret,
		shareTTL:    ttl,
	}
}

func (h *ShareHandler) Generate(c *gin.Context) {
	userID := c.GetString("userID")
	if userID == "" {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "unauthorized"})
		return
	}

	mediaID := c.Param("id")
	m, err := h.repo.GetByIDForUser(c.Request.Context(), mediaID, userID)
	if err != nil {
		if respondMediaError(c, err) {
			return
		}
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to load media"})
		return
	}

	ttl := time.Duration(h.shareTTL) * time.Second
	token := auth.GenerateShareToken(m.ID, userID, h.shareSecret, ttl)

	shareURL := c.Request.Header.Get("Origin") + "/api/v1/share/" + token

	c.JSON(http.StatusOK, gin.H{
		"token":    token,
		"shareURL": shareURL,
		"expires":  time.Now().Add(ttl).Unix(),
	})
}

func (h *ShareHandler) ServeShared(c *gin.Context) {
	token := c.Param("token")
	if token == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "token required"})
		return
	}

	mediaID, _, err := auth.VerifyShareToken(token, h.shareSecret)
	if err != nil {
		log.Printf("Share token verification failed: %v", err)
		c.JSON(http.StatusForbidden, gin.H{"error": "invalid or expired share link"})
		return
	}

	m, err := h.repo.GetByID(c.Request.Context(), mediaID)
	if err != nil {
		if respondMediaError(c, err) {
			return
		}
		c.JSON(http.StatusInternalServerError, gin.H{"error": "media not found"})
		return
	}

	targetURL := m.ProcessedURL
	if targetURL == nil || *targetURL == "" {
		targetURL = &m.OriginalURL
	}

	c.Redirect(http.StatusFound, *targetURL)
}
