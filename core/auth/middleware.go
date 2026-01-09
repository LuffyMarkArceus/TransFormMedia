package auth

import (
	"log"
	"net/http"
	"os"
	"strings"

	"github.com/MicahParks/keyfunc/v3"
	"github.com/gin-gonic/gin"
	"github.com/golang-jwt/jwt/v5"
)

// clerkKeyFunc is the function used to verify tokens
var clerkKeyFunc jwt.Keyfunc

// InitJWKS initializes the keyfunc.Keyfunc that will fetch & refresh the remote JWKS
func InitJWKS() {
	issuer := os.Getenv("CLERK_ISSUER")
	if issuer == "" {
		log.Fatal("CLERK_ISSUER environment variable is not set")
	}

	jwksURL := issuer + "/.well-known/jwks.json"

	// Create the keyfunc.Keyfunc using the JWKS URL
	kf, err := keyfunc.NewDefault([]string{jwksURL})
	if err != nil {
		log.Fatalf("Failed to create crypto keyfunc from JWKS: %v", err)
	}

	// jwt.Parse expects a Keyfunc — that is provided by the keyfunc.Keyfunc object
	clerkKeyFunc = kf.Keyfunc

	log.Println("✅ Clerk JWKS keyfunc successfully initialized")
}

// ClerkAuthMiddleware verifies the JWT in the Authorization Bearer token
func ClerkAuthMiddleware() gin.HandlerFunc {
	return func(c *gin.Context) {
		authHeader := c.GetHeader("Authorization")
		if authHeader == "" || !strings.HasPrefix(authHeader, "Bearer ") {
			c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"error": "Missing or invalid Authorization header"})
			return
		}

		tokenStr := strings.TrimPrefix(authHeader, "Bearer ")

		// Parse and validate the token using the clerkKeyFunc
		token, err := jwt.Parse(tokenStr, clerkKeyFunc)
		if err != nil || !token.Valid {
			log.Printf("JWT parse/verify error: %v", err)
			c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"error": "Invalid token"})
			return
		}

		// Extract standard claims as a map
		claims, ok := token.Claims.(jwt.MapClaims)
		if !ok {
			c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"error": "Invalid token claims"})
			return
		}

		// Save user info into the context for handlers
		c.Set("userID", claims["sub"])
		c.Set("email", claims["email"])
		c.Set("issuer", claims["iss"])

		c.Next()
	}
}
