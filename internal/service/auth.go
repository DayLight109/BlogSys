package service

import (
	"errors"
	"time"

	"github.com/lilce/blog-api/internal/auth"
	"github.com/lilce/blog-api/internal/model"
	"github.com/lilce/blog-api/internal/repository"
)

var (
	ErrInvalidCredentials = errors.New("invalid username or password")
	ErrUserExists         = errors.New("user already exists")
)

type AuthService struct {
	users *repository.UserRepository
	tm    *auth.TokenManager
}

func NewAuthService(users *repository.UserRepository, tm *auth.TokenManager) *AuthService {
	return &AuthService{users: users, tm: tm}
}

type TokenPair struct {
	AccessToken      string
	AccessExpiresAt  time.Time
	RefreshToken     string
	RefreshExpiresAt time.Time
	User             *model.User
}

func (s *AuthService) Login(username, password string) (*TokenPair, error) {
	u, err := s.users.FindByUsername(username)
	if err != nil {
		return nil, err
	}
	if u == nil || !auth.VerifyPassword(u.PasswordHash, password) {
		return nil, ErrInvalidCredentials
	}
	return s.issuePair(u)
}

func (s *AuthService) Refresh(refreshToken string) (*TokenPair, error) {
	claims, err := s.tm.Parse(refreshToken)
	if err != nil {
		return nil, err
	}
	if claims.Type != auth.TypeRefresh {
		return nil, errors.New("not a refresh token")
	}
	u, err := s.users.FindByID(claims.UserID)
	if err != nil {
		return nil, err
	}
	if u == nil {
		return nil, errors.New("user not found")
	}
	return s.issuePair(u)
}

func (s *AuthService) EnsureAdminSeed(username, password string) error {
	n, err := s.users.Count()
	if err != nil {
		return err
	}
	if n > 0 {
		return nil
	}
	hash, err := auth.HashPassword(password)
	if err != nil {
		return err
	}
	return s.users.Create(&model.User{
		Username:     username,
		PasswordHash: hash,
		Role:         "admin",
	})
}

func (s *AuthService) issuePair(u *model.User) (*TokenPair, error) {
	at, aExp, err := s.tm.Issue(u.ID, u.Username, u.Role, auth.TypeAccess)
	if err != nil {
		return nil, err
	}
	rt, rExp, err := s.tm.Issue(u.ID, u.Username, u.Role, auth.TypeRefresh)
	if err != nil {
		return nil, err
	}
	return &TokenPair{
		AccessToken:      at,
		AccessExpiresAt:  aExp,
		RefreshToken:     rt,
		RefreshExpiresAt: rExp,
		User:             u,
	}, nil
}
