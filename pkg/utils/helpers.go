package utils

import (
	"fmt"
	"time"

	"github.com/golang-jwt/jwt/v4"
)

func Map[T, V any](in []T, fn func(T) V) []V {
	out := make([]V, len(in))
	for i, t := range in {
		out[i] = fn(t)
	}
	return out
}

func GetTokenExpirationTime(jwtToken string) (time.Time, error) {
	type JwtClaims struct {
		jwt.RegisteredClaims
	}
	token, _, err := jwt.NewParser().ParseUnverified(jwtToken, &JwtClaims{})
	if err != nil {
		return time.Time{}, fmt.Errorf("failed to parse jwt token: %w", err)
	}

	if claims, ok := token.Claims.(*JwtClaims); ok {
		if claims.ExpiresAt != nil {
			return claims.ExpiresAt.Time, nil
		} else {
			return time.Time{}, fmt.Errorf("missing expiration claim in token")
		}
	}

	return time.Time{}, fmt.Errorf("failed to parse token claims to get expire at")
}
