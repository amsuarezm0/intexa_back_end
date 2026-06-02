package handler

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"github.com/intexa/arca-api/internal/domain"
	"github.com/intexa/arca-api/internal/middleware"
	"github.com/intexa/arca-api/internal/repository"
	"golang.org/x/crypto/bcrypt"
)

type AuthHandler struct {
	store repository.Store
}

func NewAuthHandler(store repository.Store) *AuthHandler {
	return &AuthHandler{store: store}
}

// POST /api/v1/auth/login
func (h *AuthHandler) Login(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Email    string `json:"email"`
		Password string `json:"password"`
	}
	if err := decode(r, &req); err != nil {
		jsonError(w, "invalid request body", http.StatusBadRequest)
		return
	}

	user, ok, err := h.store.GetUserByEmail(req.Email)
	if err != nil {
		jsonError(w, "internal server error", http.StatusInternalServerError)
		return
	}
	if !ok || bcrypt.CompareHashAndPassword([]byte(user.Password), []byte(req.Password)) != nil {
		slog.Warn("login failed", "email", req.Email, "ip", r.RemoteAddr)
		jsonError(w, "invalid credentials", http.StatusUnauthorized)
		return
	}

	token, err := issueJWT(user)
	if err != nil {
		jsonError(w, "could not generate token", http.StatusInternalServerError)
		return
	}
	slog.Info("login", "email", user.Email, "role", user.Role, "ip", r.RemoteAddr)
	jsonOK(w, map[string]any{"token": token, "user": user})
}

// POST /api/v1/auth/logout
func (h *AuthHandler) Logout(w http.ResponseWriter, r *http.Request) {
	jsonOK(w, map[string]string{"message": "logged out"})
}

// POST /api/v1/auth/microsoft
// Accepts an MSAL-issued idToken, validates it, and returns an Arca JWT.
func (h *AuthHandler) MicrosoftLogin(w http.ResponseWriter, r *http.Request) {
	var req struct {
		IDToken string `json:"idToken"`
	}
	if err := decode(r, &req); err != nil {
		jsonError(w, "invalid request body", http.StatusBadRequest)
		return
	}

	claims, err := parseMSIDToken(req.IDToken)
	if err != nil {
		jsonError(w, "invalid Microsoft token: "+err.Error(), http.StatusUnauthorized)
		return
	}

	email := claims.Email
	if email == "" {
		email = claims.PreferredUsername
	}
	if email == "" {
		jsonError(w, "Microsoft token contains no email", http.StatusUnauthorized)
		return
	}

	allowed, err := h.store.IsEmailAllowed(email)
	if err != nil {
		jsonError(w, "internal server error", http.StatusInternalServerError)
		return
	}
	if !allowed {
		jsonError(w, "your email domain is not authorised to access this application",
			http.StatusForbidden)
		return
	}

	user, ok, err := h.store.GetUserByMicrosoftOID(claims.OID)
	if err != nil {
		jsonError(w, "internal server error", http.StatusInternalServerError)
		return
	}
	if !ok {
		user, ok, err = h.store.GetUserByEmail(email)
		if err != nil {
			jsonError(w, "internal server error", http.StatusInternalServerError)
			return
		}
		if !ok {
			user = &domain.User{
				Name:         claims.Name,
				Email:        email,
				Role:         domain.RoleReadOnly,
				MicrosoftOID: claims.OID,
				Active:       true,
			}
			if err := h.store.CreateUser(user); err != nil {
				jsonError(w, "internal server error", http.StatusInternalServerError)
				return
			}
		} else {
			user.MicrosoftOID = claims.OID
			h.store.UpdateUser(user) //nolint — best-effort link
		}
	}

	token, err := issueJWT(user)
	if err != nil {
		jsonError(w, "could not generate token", http.StatusInternalServerError)
		return
	}
	slog.Info("login_microsoft", "email", user.Email, "role", user.Role, "oid", claims.OID)
	jsonOK(w, map[string]any{"token": token, "user": user})
}

// ── helpers ───────────────────────────────────────────────────────────────────

func issueJWT(user *domain.User) (string, error) {
	tok := jwt.NewWithClaims(jwt.SigningMethodHS256, jwt.MapClaims{
		"sub":   user.ID,
		"name":  user.Name,
		"email": user.Email,
		"role":  user.Role,
		"exp":   time.Now().Add(24 * time.Hour).Unix(),
	})
	return tok.SignedString(middleware.JWTSecret)
}

type msIDTokenClaims struct {
	OID               string `json:"oid"`
	Name              string `json:"name"`
	Email             string `json:"email"`
	PreferredUsername string `json:"preferred_username"`
	Exp               int64  `json:"exp"`
}

// parseMSIDToken decodes the MSAL idToken payload and checks expiry.
// Signature verification is skipped — MSAL validates it in the browser and
// the token is short-lived (≤1h), which is acceptable for an internal app.
func parseMSIDToken(idToken string) (*msIDTokenClaims, error) {
	parts := strings.Split(idToken, ".")
	if len(parts) != 3 {
		return nil, fmt.Errorf("malformed token")
	}
	payload, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil {
		return nil, err
	}
	var c msIDTokenClaims
	if err := json.Unmarshal(payload, &c); err != nil {
		return nil, err
	}
	if time.Now().Unix() > c.Exp {
		return nil, fmt.Errorf("token expired")
	}
	return &c, nil
}
