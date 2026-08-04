package auth

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"time"

	"github.com/gocql/gocql"
	"github.com/golang-jwt/jwt/v5"
	"golang.org/x/crypto/bcrypt"
)

// Config holds auth configuration.
type Config struct {
	JWTSecret     string
	TokenExpiry   time.Duration
	DefaultUser   string
	DefaultPass   string
}

// DefaultConfig returns default auth config.
func DefaultConfig() Config {
	return Config{
		TokenExpiry: 24 * time.Hour,
		DefaultUser: "admin",
		DefaultPass: "arteriadeployment",
	}
}

// Claims holds JWT token claims.
type Claims struct {
	UserID   string `json:"user_id"`
	Username string `json:"username"`
	Role     string `json:"role"`
	jwt.RegisteredClaims
}

// HashPassword hashes a password with bcrypt.
func HashPassword(password string) (string, error) {
	bytes, err := bcrypt.GenerateFromPassword([]byte(password), 12)
	return string(bytes), err
}

// CheckPassword verifies a password against a hash.
func CheckPassword(password, hash string) bool {
	return bcrypt.CompareHashAndPassword([]byte(hash), []byte(password)) == nil
}

// GenerateToken creates a signed JWT token.
func GenerateToken(secret, userID, username, role string, expiry time.Duration) (string, error) {
	claims := &Claims{
		UserID:   userID,
		Username: username,
		Role:     role,
		RegisteredClaims: jwt.RegisteredClaims{
			ExpiresAt: jwt.NewNumericDate(time.Now().Add(expiry)),
			IssuedAt:  jwt.NewNumericDate(time.Now()),
			Issuer:    "arteria",
		},
	}

	token := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
	return token.SignedString([]byte(secret))
}

// ValidateToken verifies and parses a JWT token.
func ValidateToken(tokenStr, secret string) (*Claims, error) {
	token, err := jwt.ParseWithClaims(tokenStr, &Claims{}, func(token *jwt.Token) (interface{}, error) {
		if _, ok := token.Method.(*jwt.SigningMethodHMAC); !ok {
			return nil, fmt.Errorf("unexpected signing method: %v", token.Header["alg"])
		}
		return []byte(secret), nil
	})
	if err != nil {
		return nil, err
	}

	claims, ok := token.Claims.(*Claims)
	if !ok || !token.Valid {
		return nil, fmt.Errorf("invalid token")
	}
	return claims, nil
}

// GenerateSecret creates a random 32-byte hex secret.
func GenerateSecret() string {
	b := make([]byte, 32)
	rand.Read(b)
	return hex.EncodeToString(b)
}

// EnsureDefaultUser creates the default admin user if no users exist.
func EnsureDefaultUser(session *gocql.Session, username, password string) error {
	var count int
	if err := session.Query(`SELECT COUNT(*) FROM arteria.users`).Scan(&count); err != nil {
		return err
	}

	if count > 0 {
		return nil
	}

	hash, err := HashPassword(password)
	if err != nil {
		return err
	}

	userID := gocql.TimeUUID()
	return session.Query(`INSERT INTO arteria.users (user_id, username, password_hash, role, is_active, created_at) VALUES (?, ?, ?, ?, ?, ?)`,
		userID, username, hash, "admin", true, time.Now()).Exec()
}
