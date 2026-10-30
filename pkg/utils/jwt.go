package utils

import (
	"os"
	"time"

	"github.com/golang-jwt/jwt/v5"
)

var JWTSecret = []byte(os.Getenv("JWT_SECRET"))

type Claims struct {
	UserID    string `json:"user_id"`
	TokenType string `json:"token_type,omitempty"`
	jwt.RegisteredClaims
}

func SetJWTSecret(secret string) {
	if secret == "" {
		return
	}
	JWTSecret = []byte(secret)
}

func GenerateToken(userID string) (string, error) {
	return GenerateAccessToken(userID)
}

func GenerateAccessToken(userID string) (string, error) {
	return generateToken(userID, "access", 15*time.Minute)
}

func GenerateRefreshToken(userID string) (string, error) {
	return generateToken(userID, "refresh", 30*24*time.Hour)
}

func generateToken(userID, tokenType string, lifetime time.Duration) (string, error) {
	if len(JWTSecret) == 0 {
		JWTSecret = []byte("star-meet-secret-key-change-in-production") // 与auth.go保持一致
	}

	claims := Claims{
		UserID:    userID,
		TokenType: tokenType,
		RegisteredClaims: jwt.RegisteredClaims{
			ExpiresAt: jwt.NewNumericDate(time.Now().Add(lifetime)),
			Issuer:    "star-meet-backend",
		},
	}

	token := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
	return token.SignedString(JWTSecret)
}

func ParseToken(tokenString string) (*Claims, error) {
	if len(JWTSecret) == 0 {
		JWTSecret = []byte("star-meet-secret-key-change-in-production") // Fallback for dev
	}

	token, err := jwt.ParseWithClaims(tokenString, &Claims{}, func(token *jwt.Token) (interface{}, error) {
		if token.Method != jwt.SigningMethodHS256 {
			return nil, jwt.ErrTokenSignatureInvalid
		}
		return JWTSecret, nil
	})

	if err != nil {
		return nil, err
	}

	if claims, ok := token.Claims.(*Claims); ok && token.Valid {
		return claims, nil
	}

	return nil, err
}
