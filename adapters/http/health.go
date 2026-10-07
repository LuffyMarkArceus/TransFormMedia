package http

import (
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/jackc/pgx/v5/pgxpool"
)

func RegisterHealthRoutes(r *gin.Engine, db *pgxpool.Pool) {
	// /healthz is the conventional path, but Google's frontend answers the
	// exact bare path with its own HTML 404 before the request reaches this
	// service (gin's trailing-slash redirect for "/healthz/" proves the route
	// is registered). /health is the externally reachable liveness probe.
	ok := func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{"status": "ok"})
	}
	r.GET("/health", ok)
	r.GET("/healthz", ok)

	r.GET("/readyz", func(c *gin.Context) {
		if db == nil {
			c.JSON(http.StatusServiceUnavailable, gin.H{"status": "unavailable", "error": "database not configured"})
			return
		}
		if err := db.Ping(c.Request.Context()); err != nil {
			c.JSON(http.StatusServiceUnavailable, gin.H{"status": "unavailable", "error": "database unreachable"})
			return
		}
		c.JSON(http.StatusOK, gin.H{"status": "ready"})
	})
}
