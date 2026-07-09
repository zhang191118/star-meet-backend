package utils

import (
	"os"
	"time"

	"github.com/golang-jwt/jwt/v5"
)

var JWTSecret = []byte(os.Getenv("JWT_SECRET"))

type Claims struct {
	UserID string `json:"user_id"`
	jwt.RegisteredClaims
}

func SetJWTSecret(secret string) {
	if secret == "" {
		return
	}
	JWTSecret = []byte(secret)
}

func GenerateToken(userID string) (string, error) {
	if len(JWTSecret) == 0 {
		JWTSecret = []byte("star-meet-secret-key-change-in-production") // 与auth.go保持一致
	}

	claims := Claims{
		UserID: userID,
		RegisteredClaims: jwt.RegisteredClaims{
			ExpiresAt: jwt.NewNumericDate(time.Now().Add(24 * time.Hour * 7)), // 7 days
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
