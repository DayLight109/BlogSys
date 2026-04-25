package service

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"time"

	"github.com/redis/go-redis/v9"

	"github.com/lilce/blog-api/internal/auth"
	"github.com/lilce/blog-api/internal/model"
	"github.com/lilce/blog-api/internal/repository"
)

var (
	ErrInvalidCredentials = errors.New("invalid username or password")
	ErrUserExists         = errors.New("user already exists")
	ErrWeakPassword       = errors.New("password must be at least 8 characters")
	ErrAccountLocked      = errors.New("account temporarily locked, try again later")
)

// Per-user lockout thresholds. Complements the per-IP rate limiter: an
// attacker rotating IPs still can't brute-force a single username.
const (
	lockoutMaxFails = 10
	lockoutWindow   = 15 * time.Minute
	lockoutDuration = 30 * time.Minute
)

type AuthService struct {
	users *repository.UserRepository
	tm    *auth.TokenManager
	rdb   *redis.Client
}

func NewAuthService(users *repository.UserRepository, tm *auth.TokenManager, rdb *redis.Client) *AuthService {
	return &AuthService{users: users, tm: tm, rdb: rdb}
}

type TokenPair struct {
	AccessToken      string
	AccessExpiresAt  time.Time
	RefreshToken     string
	RefreshExpiresAt time.Time
	User             *model.User
}

func failKey(username string) string   { return fmt.Sprintf("auth:fail:%s", username) }
func lockKey(username string) string   { return fmt.Sprintf("auth:lock:%s", username) }
func refreshKey(jti string) string     { return fmt.Sprintf("auth:refresh:%s", jti) }
func refreshUserKey(userID uint64) string {
	return fmt.Sprintf("auth:user_refresh:%d", userID)
}
func refreshInvalidBeforeKey(userID uint64) string {
	return fmt.Sprintf("auth:refresh_invalid_before:%d", userID)
}

// isLocked returns true when the username has an active lockout record.
func (s *AuthService) isLocked(ctx context.Context, username string) bool {
	if s.rdb == nil {
		return false
	}
	n, _ := s.rdb.Exists(ctx, lockKey(username)).Result()
	return n > 0
}

// recordFail increments the per-user failure counter and activates a lockout
// once the threshold is hit within the sliding window.
func (s *AuthService) recordFail(ctx context.Context, username string) {
	if s.rdb == nil {
		return
	}
	key := failKey(username)
	n, err := s.rdb.Incr(ctx, key).Result()
	if err != nil {
		return
	}
	if n == 1 {
		s.rdb.Expire(ctx, key, lockoutWindow)
	}
	if n >= lockoutMaxFails {
		s.rdb.Set(ctx, lockKey(username), "1", lockoutDuration)
		s.rdb.Del(ctx, key)
	}
}

func (s *AuthService) clearFails(ctx context.Context, username string) {
	if s.rdb == nil {
		return
	}
	s.rdb.Del(ctx, failKey(username), lockKey(username))
}

func (s *AuthService) Login(username, password string) (*TokenPair, error) {
	ctx := context.Background()
	if s.isLocked(ctx, username) {
		// Still burn bcrypt time so responses look identical to the normal
		// wrong-password path — don't leak that lockout is active.
		auth.DummyCompare()
		return nil, ErrInvalidCredentials
	}

	u, err := s.users.FindByUsername(username)
	if err != nil {
		return nil, err
	}
	// Always consume bcrypt time to defeat username-enumeration via timing.
	if u == nil {
		auth.DummyCompare()
		s.recordFail(ctx, username)
		return nil, ErrInvalidCredentials
	}
	if !auth.VerifyPassword(u.PasswordHash, password) {
		s.recordFail(ctx, username)
		return nil, ErrInvalidCredentials
	}
	s.clearFails(ctx, username)
	return s.issuePair(u)
}

// ChangePassword verifies the current password and replaces it with a new one
// meeting a minimal strength policy.
func (s *AuthService) ChangePassword(userID uint64, current, next string) error {
	if len(next) < 8 {
		return ErrWeakPassword
	}
	ctx := context.Background()
	u, err := s.users.FindByID(userID)
	if err != nil {
		return err
	}
	if u == nil {
		return ErrInvalidCredentials
	}
	if !auth.VerifyPassword(u.PasswordHash, current) {
		return ErrInvalidCredentials
	}
	hash, err := auth.HashPassword(next)
	if err != nil {
		return err
	}
	u.PasswordHash = hash
	if err := s.users.Update(u); err != nil {
		return err
	}
	return s.invalidateUserRefresh(ctx, userID)
}

func (s *AuthService) Refresh(refreshToken string) (*TokenPair, error) {
	ctx := context.Background()
	claims, err := s.tm.Parse(refreshToken)
	if err != nil {
		return nil, err
	}
	if claims.Type != auth.TypeRefresh {
		return nil, errors.New("not a refresh token")
	}
	if claims.ID == "" {
		return nil, errors.New("refresh token missing id")
	}
	if err := s.ensureRefreshStillValid(ctx, claims); err != nil {
		return nil, err
	}
	if err := s.consumeRefresh(ctx, claims); err != nil {
		return nil, err
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

func (s *AuthService) Logout(refreshToken string) {
	claims, err := s.tm.Parse(refreshToken)
	if err != nil || claims.Type != auth.TypeRefresh || claims.ID == "" {
		return
	}
	_ = s.revokeRefresh(context.Background(), claims.UserID, claims.ID)
}

// EnsureAdminSeedIfEmpty creates the initial admin user only when the users
// table is empty. Returns whether a new user was created.
func (s *AuthService) EnsureAdminSeedIfEmpty(username, password string) (bool, error) {
	n, err := s.users.Count()
	if err != nil {
		return false, err
	}
	if n > 0 {
		return false, nil
	}
	hash, err := auth.HashPassword(password)
	if err != nil {
		return false, err
	}
	if err := s.users.Create(&model.User{
		Username:     username,
		PasswordHash: hash,
		Role:         "admin",
	}); err != nil {
		return false, err
	}
	return true, nil
}

// EnsureAdminSeed retained for backwards compatibility.
func (s *AuthService) EnsureAdminSeed(username, password string) error {
	_, err := s.EnsureAdminSeedIfEmpty(username, password)
	return err
}

func (s *AuthService) issuePair(u *model.User) (*TokenPair, error) {
	at, aExp, err := s.tm.Issue(u.ID, u.Username, u.Role, auth.TypeAccess)
	if err != nil {
		return nil, err
	}
	rt, rExp, rJTI, err := s.tm.IssueWithID(u.ID, u.Username, u.Role, auth.TypeRefresh)
	if err != nil {
		return nil, err
	}
	if err := s.rememberRefresh(context.Background(), u.ID, rJTI, rExp); err != nil {
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

func (s *AuthService) rememberRefresh(ctx context.Context, userID uint64, jti string, expiresAt time.Time) error {
	if s.rdb == nil {
		return nil
	}
	ttl := time.Until(expiresAt)
	if ttl <= 0 {
		return errors.New("refresh token already expired")
	}
	userKey := refreshUserKey(userID)
	pipe := s.rdb.TxPipeline()
	pipe.Set(ctx, refreshKey(jti), strconv.FormatUint(userID, 10), ttl)
	pipe.SAdd(ctx, userKey, jti)
	pipe.Expire(ctx, userKey, ttl)
	_, err := pipe.Exec(ctx)
	return err
}

func (s *AuthService) ensureRefreshStillValid(ctx context.Context, claims *auth.Claims) error {
	if s.rdb == nil {
		return nil
	}
	if claims.IssuedAt == nil {
		return errors.New("refresh token missing issued-at")
	}
	cutoff, err := s.rdb.Get(ctx, refreshInvalidBeforeKey(claims.UserID)).Int64()
	if errors.Is(err, redis.Nil) {
		return nil
	}
	if err != nil {
		return err
	}
	if claims.IssuedAt.Time.Unix() < cutoff {
		return errors.New("refresh token revoked")
	}
	return nil
}

func (s *AuthService) consumeRefresh(ctx context.Context, claims *auth.Claims) error {
	if s.rdb == nil {
		return nil
	}
	rawUserID, err := s.rdb.GetDel(ctx, refreshKey(claims.ID)).Result()
	if errors.Is(err, redis.Nil) {
		return errors.New("refresh token revoked")
	}
	if err != nil {
		return err
	}
	if rawUserID != strconv.FormatUint(claims.UserID, 10) {
		return errors.New("refresh token user mismatch")
	}
	_ = s.rdb.SRem(ctx, refreshUserKey(claims.UserID), claims.ID).Err()
	return nil
}

func (s *AuthService) revokeRefresh(ctx context.Context, userID uint64, jti string) error {
	if s.rdb == nil {
		return nil
	}
	pipe := s.rdb.TxPipeline()
	pipe.Del(ctx, refreshKey(jti))
	pipe.SRem(ctx, refreshUserKey(userID), jti)
	_, err := pipe.Exec(ctx)
	return err
}

func (s *AuthService) invalidateUserRefresh(ctx context.Context, userID uint64) error {
	if s.rdb == nil {
		return nil
	}
	userKey := refreshUserKey(userID)
	jtis, err := s.rdb.SMembers(ctx, userKey).Result()
	if err != nil && !errors.Is(err, redis.Nil) {
		return err
	}
	pipe := s.rdb.TxPipeline()
	pipe.Set(ctx, refreshInvalidBeforeKey(userID), time.Now().Unix(), s.tm.RefreshTTL())
	for _, jti := range jtis {
		pipe.Del(ctx, refreshKey(jti))
	}
	pipe.Del(ctx, userKey)
	_, err = pipe.Exec(ctx)
	return err
}
