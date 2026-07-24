package auth

import (
	"crypto/hmac"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"encoding/json"
	"errors"
	"strings"
	"sync"
	"time"

	"golang.org/x/crypto/bcrypt"
)

var (
	ErrInvalidCredentials = errors.New("invalid credentials")
	ErrRateLimited        = errors.New("login rate limited")
)

const (
	defaultMaxFailures   = 5
	defaultFailureWindow = 15 * time.Minute
)

type Config struct {
	Username      string
	PasswordHash  string
	SessionSecret string
	SessionTTL    time.Duration
	MaxFailures   int
	FailureWindow time.Duration
}

type Manager struct {
	username      string
	passwordHash  []byte
	secret        []byte
	ttl           time.Duration
	maxFailures   int
	failureWindow time.Duration
	now           func() time.Time
	mu            sync.Mutex
	failures      map[string][]time.Time
}

type sessionPayload struct {
	Username  string `json:"username"`
	ExpiresAt int64  `json:"expires_at"`
}

func New(cfg Config, now func() time.Time) *Manager {
	if now == nil {
		now = time.Now
	}
	if cfg.MaxFailures <= 0 {
		cfg.MaxFailures = defaultMaxFailures
	}
	if cfg.FailureWindow <= 0 {
		cfg.FailureWindow = defaultFailureWindow
	}
	return &Manager{
		username:      cfg.Username,
		passwordHash:  []byte(cfg.PasswordHash),
		secret:        []byte(cfg.SessionSecret),
		ttl:           cfg.SessionTTL,
		maxFailures:   cfg.MaxFailures,
		failureWindow: cfg.FailureWindow,
		now:           now,
		failures:      make(map[string][]time.Time),
	}
}

func (m *Manager) Login(username, password, ip string) (string, time.Time, error) {
	now := m.now()
	if m.rateLimited(ip, now) {
		return "", time.Time{}, ErrRateLimited
	}

	expectedUsername := sha256.Sum256([]byte(m.username))
	providedUsername := sha256.Sum256([]byte(username))
	usernameValid := subtle.ConstantTimeCompare(expectedUsername[:], providedUsername[:]) == 1
	passwordValid := bcrypt.CompareHashAndPassword(m.passwordHash, []byte(password)) == nil
	if !usernameValid || !passwordValid {
		m.recordFailure(ip, now)
		return "", time.Time{}, ErrInvalidCredentials
	}

	m.clearFailures(ip)
	expires := now.Add(m.ttl)
	return m.sign(sessionPayload{Username: m.username, ExpiresAt: expires.Unix()}), expires, nil
}

func (m *Manager) Validate(token string) (string, bool) {
	payloadPart, signaturePart, ok := strings.Cut(token, ".")
	if !ok || payloadPart == "" || signaturePart == "" || strings.Contains(signaturePart, ".") {
		return "", false
	}

	providedSignature, err := base64.RawURLEncoding.DecodeString(signaturePart)
	if err != nil {
		return "", false
	}
	expectedSignature := m.signature(payloadPart)
	if !hmac.Equal(expectedSignature, providedSignature) {
		return "", false
	}

	payloadJSON, err := base64.RawURLEncoding.DecodeString(payloadPart)
	if err != nil {
		return "", false
	}
	var payload sessionPayload
	if err := json.Unmarshal(payloadJSON, &payload); err != nil {
		return "", false
	}
	expectedUsername := sha256.Sum256([]byte(m.username))
	providedUsername := sha256.Sum256([]byte(payload.Username))
	if subtle.ConstantTimeCompare(expectedUsername[:], providedUsername[:]) != 1 {
		return "", false
	}
	if payload.ExpiresAt <= m.now().Unix() {
		return "", false
	}
	return payload.Username, true
}

func (m *Manager) sign(payload sessionPayload) string {
	payloadJSON, _ := json.Marshal(payload)
	payloadPart := base64.RawURLEncoding.EncodeToString(payloadJSON)
	signaturePart := base64.RawURLEncoding.EncodeToString(m.signature(payloadPart))
	return payloadPart + "." + signaturePart
}

func (m *Manager) signature(payloadPart string) []byte {
	mac := hmac.New(sha256.New, m.secret)
	_, _ = mac.Write([]byte(payloadPart))
	return mac.Sum(nil)
}

func (m *Manager) rateLimited(ip string, now time.Time) bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	key := normalizeIP(ip)
	recent := prune(m.failures[key], now.Add(-m.failureWindow))
	if len(recent) == 0 {
		delete(m.failures, key)
	} else {
		m.failures[key] = recent
	}
	return len(recent) >= m.maxFailures
}

func (m *Manager) recordFailure(ip string, now time.Time) {
	m.mu.Lock()
	defer m.mu.Unlock()
	key := normalizeIP(ip)
	recent := prune(m.failures[key], now.Add(-m.failureWindow))
	m.failures[key] = append(recent, now)
}

func (m *Manager) clearFailures(ip string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	delete(m.failures, normalizeIP(ip))
}

func prune(values []time.Time, cutoff time.Time) []time.Time {
	first := 0
	for first < len(values) && values[first].Before(cutoff) {
		first++
	}
	return values[first:]
}

func normalizeIP(ip string) string {
	ip = strings.TrimSpace(ip)
	if ip == "" {
		return "unknown"
	}
	return ip
}
