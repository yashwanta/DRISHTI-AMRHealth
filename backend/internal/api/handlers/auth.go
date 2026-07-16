package handlers

import (
	"crypto/hmac"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"golang.org/x/crypto/bcrypt"
)

const (
	loginRateLimitMax     = 10
	loginRateLimitWindow  = 10 * time.Minute
	loginRateLimitCleanup = 20 * time.Minute
)

type AuthHandler struct {
	db       *pgxpool.Pool
	username string
	password string
	secret   []byte
}

type loginRequest struct {
	Username string `json:"username"`
	Password string `json:"password"`
}

type loginResponse struct {
	Token     string `json:"token"`
	Username  string `json:"username"`
	Role      string `json:"role"`
	ExpiresAt string `json:"expires_at"`
}

type loginRateEntry struct {
	mu          sync.Mutex
	count       int
	windowStart time.Time
	lastSeen    time.Time
}

var (
	loginRateEntries sync.Map
	loginCleanupOnce sync.Once
)

func NewAuthHandler(db *pgxpool.Pool, username, password, secret string) *AuthHandler {
	if secret == "" || secret == "change-this-session-secret" || secret == "change-this-session-secret!!" {
		panic("SESSION_SECRET is missing or still using a placeholder default")
	}
	return &AuthHandler{db: db, username: username, password: password, secret: []byte(secret)}
}

func (h *AuthHandler) Login(w http.ResponseWriter, r *http.Request) {
	if !allowLoginAttempt(r) {
		jsonError(w, "too many login attempts; try again later", http.StatusTooManyRequests)
		return
	}

	var req loginRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		jsonError(w, "invalid body", http.StatusBadRequest)
		return
	}

	role, ok := h.validateLogin(r, req.Username, req.Password)
	if !ok {
		jsonError(w, "invalid username or password", http.StatusUnauthorized)
		return
	}

	expires := time.Now().Add(12 * time.Hour).UTC()
	token := h.sign(req.Username, role, expires)
	http.SetCookie(w, &http.Cookie{
		Name:     "siteops_token",
		Value:    token,
		Path:     "/",
		MaxAge:   int((12 * time.Hour).Seconds()),
		HttpOnly: true,
		SameSite: http.SameSiteStrictMode,
	})
	jsonOK(w, loginResponse{
		Token:     token,
		Username:  req.Username,
		Role:      role,
		ExpiresAt: expires.Format(time.RFC3339),
	})
}

func (h *AuthHandler) Logout(w http.ResponseWriter, r *http.Request) {
	http.SetCookie(w, &http.Cookie{
		Name:     "siteops_token",
		Value:    "",
		Path:     "/",
		MaxAge:   -1,
		HttpOnly: true,
		SameSite: http.SameSiteStrictMode,
	})
	jsonOK(w, map[string]string{"status": "ok"})
}

func (h *AuthHandler) Me(w http.ResponseWriter, r *http.Request) {
	username, ok := usernameFromRequest(r)
	if !ok {
		jsonError(w, "unauthorized", http.StatusUnauthorized)
		return
	}
	role, _ := roleFromRequest(r)
	jsonOK(w, map[string]string{"username": username, "role": role})
}

func (h *AuthHandler) Middleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		token := ""
		header := r.Header.Get("Authorization")
		if strings.HasPrefix(header, "Bearer ") {
			token = strings.TrimPrefix(header, "Bearer ")
		} else if cookie, err := r.Cookie("siteops_token"); err == nil {
			token = cookie.Value
		}

		if token == "" {
			jsonError(w, "missing authorization token", http.StatusUnauthorized)
			return
		}

		username, role, ok := h.verify(token)
		if !ok {
			jsonError(w, "invalid or expired token", http.StatusUnauthorized)
			return
		}

		next.ServeHTTP(w, withUser(r, username, role))
	})
}

func (h *AuthHandler) AdminOnly(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		role, ok := roleFromRequest(r)
		if !ok || !canAdmin(role) {
			jsonError(w, "admin role required", http.StatusForbidden)
			return
		}
		next.ServeHTTP(w, r)
	})
}

func (h *AuthHandler) validateLogin(r *http.Request, username, password string) (string, bool) {
	var hash, role, status string
	err := h.db.QueryRow(r.Context(), `SELECT password_hash, role, status FROM app_users WHERE username=$1`, username).Scan(&hash, &role, &status)
	if err == nil && status == "active" && bcrypt.CompareHashAndPassword([]byte(hash), []byte(password)) == nil {
		return role, true
	}
	if err != nil && err != pgx.ErrNoRows {
		return "", false
	}

	userOK := subtle.ConstantTimeCompare([]byte(username), []byte(h.username)) == 1
	passOK := subtle.ConstantTimeCompare([]byte(password), []byte(h.password)) == 1
	if userOK && passOK {
		return "Super Admin", true
	}
	return "", false
}

func (h *AuthHandler) sign(username, role string, expires time.Time) string {
	payload := fmt.Sprintf("%s|%s|%d", username, role, expires.Unix())
	sig := hmac.New(sha256.New, h.secret)
	sig.Write([]byte(payload))
	raw := payload + "|" + base64.RawURLEncoding.EncodeToString(sig.Sum(nil))
	return base64.RawURLEncoding.EncodeToString([]byte(raw))
}

func (h *AuthHandler) verify(token string) (string, string, bool) {
	raw, err := base64.RawURLEncoding.DecodeString(token)
	if err != nil {
		return "", "", false
	}
	parts := strings.Split(string(raw), "|")
	if len(parts) != 4 {
		return "", "", false
	}

	username, role, expiresRaw, gotSig := parts[0], parts[1], parts[2], parts[3]
	payload := username + "|" + role + "|" + expiresRaw
	sig := hmac.New(sha256.New, h.secret)
	sig.Write([]byte(payload))
	wantSig := base64.RawURLEncoding.EncodeToString(sig.Sum(nil))
	if subtle.ConstantTimeCompare([]byte(gotSig), []byte(wantSig)) != 1 {
		return "", "", false
	}

	expiresUnix, err := parseUnix(expiresRaw)
	if err != nil || time.Now().Unix() > expiresUnix {
		return "", "", false
	}
	return username, role, true
}

func allowLoginAttempt(r *http.Request) bool {
	loginCleanupOnce.Do(startLoginRateCleanup)

	ip := loginClientIP(r)
	now := time.Now()
	value, _ := loginRateEntries.LoadOrStore(ip, &loginRateEntry{windowStart: now, lastSeen: now})
	entry := value.(*loginRateEntry)

	entry.mu.Lock()
	defer entry.mu.Unlock()

	entry.lastSeen = now
	if now.Sub(entry.windowStart) > loginRateLimitWindow {
		entry.windowStart = now
		entry.count = 0
	}
	if entry.count >= loginRateLimitMax {
		return false
	}
	entry.count++
	return true
}

func startLoginRateCleanup() {
	go func() {
		ticker := time.NewTicker(5 * time.Minute)
		defer ticker.Stop()
		for now := range ticker.C {
			loginRateEntries.Range(func(key, value any) bool {
				entry := value.(*loginRateEntry)
				entry.mu.Lock()
				lastSeen := entry.lastSeen
				if lastSeen.IsZero() {
					lastSeen = entry.windowStart
				}
				entry.mu.Unlock()
				if now.Sub(lastSeen) > loginRateLimitCleanup {
					loginRateEntries.Delete(key)
				}
				return true
			})
		}
	}()
}

func loginClientIP(r *http.Request) string {
	if realIP := strings.TrimSpace(r.Header.Get("X-Real-IP")); realIP != "" {
		return realIP
	}
	if host, _, err := net.SplitHostPort(r.RemoteAddr); err == nil {
		return host
	}
	return r.RemoteAddr
}

func parseUnix(raw string) (int64, error) {
	var out int64
	_, err := fmt.Sscanf(raw, "%d", &out)
	return out, err
}

func canAdmin(role string) bool {
	return role == "Super Admin" || role == "Global Admin" || role == "Location Admin"
}
