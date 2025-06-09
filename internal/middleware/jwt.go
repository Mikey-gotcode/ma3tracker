package middleware

import (
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"
	"time"
	"os"
	"github.com/golang-jwt/jwt/v5"
)

var secret = []byte(getJWTSecret())

func getJWTSecret() string {
	if val := os.Getenv("JWT_SECRET"); val != "" {
		return val
	}
	return "supersecret" // fallback
}

func GenerateToken(userID uint, role string) (string, error) {
	claims := jwt.MapClaims{
		"user_id": userID,
		"role":    role,
		"exp":     time.Now().Add(72 * time.Hour).Unix(),
	}
	token := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
	return token.SignedString(secret)
}

func ValidateToken(tokenStr string) (*jwt.Token, error) {
	return jwt.Parse(tokenStr, func(token *jwt.Token) (interface{}, error) {
		return secret, nil
	})
}

// RequireAuth ensures a valid JWT is present
func RequireAuth() gin.HandlerFunc {
	return func(c *gin.Context) {
		authHeader := c.GetHeader("Authorization")
		if authHeader == "" || !strings.HasPrefix(authHeader, "Bearer ") {
			c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"error": "Missing or invalid Authorization header"})
			return
		}

		tokenString := strings.TrimPrefix(authHeader, "Bearer ")
		token, err := jwt.Parse(tokenString, func(token *jwt.Token) (interface{}, error) {
			return secret, nil
		})
		if err != nil || !token.Valid {
			c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"error": "Invalid or expired token"})
			return
		}

		// Store claims in context for downstream handlers
		if claims, ok := token.Claims.(jwt.MapClaims); ok {
			c.Set("user_id", claims["user_id"])
			c.Set("role", claims["role"])
		} else {
			c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"error": "Invalid token claims"})
			return
		}

		c.Next()
	}
}

// RequireAuthWithRole ensures the JWT is valid and the user has a specific role
func RequireAuthWithRole(requiredRole string) gin.HandlerFunc {
	return func(c *gin.Context) {
		// First ensure basic auth
		req := RequireAuth()
		req(c)
		if c.IsAborted() {
			return
		}

		// Check role
		roleIfc, exists := c.Get("role")
		if !exists {
			c.AbortWithStatusJSON(http.StatusForbidden, gin.H{"error": "Role not found in token"})
			return
		}
		if role, ok := roleIfc.(string); !ok || role != requiredRole {
			c.AbortWithStatusJSON(http.StatusForbidden, gin.H{"error": "Insufficient permissions"})
			return
		}

		c.Next()
	}
}
