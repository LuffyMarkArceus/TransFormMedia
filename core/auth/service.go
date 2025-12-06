package auth

import (
	"net/http"

	"github.com/gin-gonic/gin"
	"universal-media-service/internal/config"
)

type AuthService struct {
	cfg *config.Config
}

func NewAuthService(cfg *config.Config) *AuthService {
	return &AuthService{cfg: cfg}
}

func (s *AuthService) Register(c *gin.Context) {
	c.JSON(http.StatusCreated, gin.H{"message": "register endpoint"})
}

func (s *AuthService) Login(c *gin.Context) {
	c.JSON(http.StatusOK, gin.H{"message": "login endpoint"})
}
