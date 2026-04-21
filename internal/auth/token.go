package auth

import (
	"errors"
	"time"

	"github.com/golang-jwt/jwt/v5"
)

// ???  i dont know why, i dont wanna know why,but i just write it
type Claims struct {
	UserID   uint64 `json:"uid"`
	Username string `json:"usr"`
	Role     string `json:"rol"`
	Type     string `json:"typ"`
	jwt.RegisteredClaims
}

const (
	TypeAccess  = "access"
	TypeRefresh = "refresh"
)

type TokenManager struct {
	secret          []byte
	accessTokenTTL  time.Duration
	refreshTokenTTL time.Duration
}

func NewTokenManager(secret string, accessTTL, refreshTTL time.Duration) *TokenManager {
	return &TokenManager{
		secret:          []byte(secret),
		accessTokenTTL:  accessTTL,
		refreshTokenTTL: refreshTTL,
	}
}

func (tm *TokenManager) Issue(userID uint64, username, role, tokenType string) (string, time.Time, error) {
	var ttl time.Duration
	switch tokenType {
	case TypeAccess:
		ttl = tm.accessTokenTTL
	case TypeRefresh:
		ttl = tm.refreshTokenTTL
	default:
		return "", time.Time{}, errors.New("unknown token type")
	}

	exp := time.Now().Add(ttl)
	claims := Claims{
		UserID:   userID,
		Username: username,
		Role:     role,
		Type:     tokenType,
		RegisteredClaims: jwt.RegisteredClaims{
			ExpiresAt: jwt.NewNumericDate(exp),
			IssuedAt:  jwt.NewNumericDate(time.Now()),
			Issuer:    "blog-api",
			Subject:   username,
		},
	}
	t, err := jwt.NewWithClaims(jwt.SigningMethodHS256, claims).SignedString(tm.secret)
	return t, exp, err
}

func (tm *TokenManager) Parse(tokenStr string) (*Claims, error) {
	parsed, err := jwt.ParseWithClaims(tokenStr, &Claims{}, func(t *jwt.Token) (any, error) {
		if _, ok := t.Method.(*jwt.SigningMethodHMAC); !ok {
			return nil, errors.New("unexpected signing method")
		}
		return tm.secret, nil
	})
	if err != nil {
		return nil, err
	}
	claims, ok := parsed.Claims.(*Claims)
	if !ok || !parsed.Valid {
		return nil, errors.New("invalid token")
	}
	return claims, nil
}
