package auth

import (
	"strings"

	"github.com/gin-gonic/gin"
)

func ClerkAuthMiddleware() gin.HandlerFunc {
	return func(c *gin.Context) {
		authHeader := c.GetHeader("Authorization")
		if authHeader == "" {
			c.AbortWithStatusJSON(401, gin.H{
				"error": "Missing Authorization header",
			})
			return
		}

		if !strings.HasPrefix(authHeader, "Bearer ") {
			c.AbortWithStatusJSON(401, gin.H{
				"error": "Invalid Authorization header format",
			})
			return
		}

		tokenStr := strings.TrimPrefix(authHeader, "Bearer ")

		claims, err := VerifyClerkJWT(tokenStr)
		if err != nil {
			c.AbortWithStatusJSON(401, gin.H{
				"error": err.Error(),
			})
			return
		}

		// Store values in Gin context
		c.Set("userID", claims.Sub)
		c.Set("email", claims.Email)
		if claims.Username != "" {
			c.Set("username", claims.Username)
		}

		c.Next()
	}
}
