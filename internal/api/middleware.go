package api

import (
	"net/http"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/Muhammedhashirm009/portix/internal/auth"
	"github.com/Muhammedhashirm009/portix/internal/config"
)

// AuthMiddleware validates JWT tokens on protected routes
func AuthMiddleware(cfg *config.Config) gin.HandlerFunc {
	return func(c *gin.Context) {
		// Get token from cookie first, then Authorization header
		tokenStr := ""

		// Try cookie
		if cookie, err := c.Cookie("tunnelpanel_token"); err == nil {
			tokenStr = cookie
		}

		// Try Authorization header
		if tokenStr == "" {
			authHeader := c.GetHeader("Authorization")
			if strings.HasPrefix(authHeader, "Bearer ") {
				tokenStr = strings.TrimPrefix(authHeader, "Bearer ")
			}
		}

		// Try query param (needed for WebSocket connections)
		if tokenStr == "" {
			if qToken := c.Query("token"); qToken != "" {
				tokenStr = qToken
			}
		}

		if tokenStr == "" {
			// Check if this is an API request or page request
			if strings.HasPrefix(c.Request.URL.Path, "/api/") {
				c.JSON(http.StatusUnauthorized, gin.H{"error": "authentication required"})
			} else {
				c.Redirect(http.StatusFound, "/login")
			}
			c.Abort()
			return
		}

		claims, err := auth.ValidateToken(tokenStr, cfg.JWTSecret)
		if err != nil {
			if strings.HasPrefix(c.Request.URL.Path, "/api/") {
				c.JSON(http.StatusUnauthorized, gin.H{"error": "invalid or expired token"})
			} else {
				c.Redirect(http.StatusFound, "/login")
			}
			c.Abort()
			return
		}

		// Store user info in context
		c.Set("user_id", claims.UserID)
		c.Set("username", claims.Username)
		c.Set("is_admin", claims.IsAdmin)
		c.Next()
	}
}

// SetupCheckMiddleware redirects to setup if not configured
func SetupCheckMiddleware() gin.HandlerFunc {
	return func(c *gin.Context) {
		// Skip for setup, login, and static routes
		path := c.Request.URL.Path
		if path == "/setup" || path == "/api/setup" || strings.HasPrefix(path, "/static/") {
			c.Next()
			return
		}

		// Check if setup is complete
		cfg := config.Get()
		if cfg.JWTSecret == "" {
			c.Redirect(http.StatusFound, "/setup")
			c.Abort()
			return
		}

		c.Next()
	}
}

// RateLimitMiddleware basic rate limiting for login attempts
func RateLimitMiddleware() gin.HandlerFunc {
	// Simple in-memory rate limiter
	type attempt struct {
		count    int
		lastTime int64
	}
	attempts := make(map[string]*attempt)

	return func(c *gin.Context) {
		if c.Request.URL.Path != "/api/auth/login" {
			c.Next()
			return
		}

		ip := c.ClientIP()
		a, exists := attempts[ip]
		now := time.Now().Unix()

		if !exists {
			attempts[ip] = &attempt{count: 1, lastTime: now}
			c.Next()
			return
		}

		// Reset after 15 minutes
		if now-a.lastTime > 900 {
			a.count = 1
			a.lastTime = now
			c.Next()
			return
		}

		// Block after 10 failed attempts
		if a.count > 10 {
			c.JSON(http.StatusTooManyRequests, gin.H{"error": "too many login attempts, try again later"})
			c.Abort()
			return
		}

		a.count++
		a.lastTime = now
		c.Next()
	}
}
