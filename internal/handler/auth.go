package handler

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"strings"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"github.com/intexa/arca-api/internal/domain"
	"github.com/intexa/arca-api/internal/middleware"
	"github.com/intexa/arca-api/internal/repository"
	"golang.org/x/crypto/bcrypt"
)

// msTenant controls which Microsoft accounts are accepted.
//   "common"          → any Microsoft account (personal + corporate) — current default
//   "<tenant-id>"     → only accounts from your specific Azure AD tenant (corporate-only)
//   "organizations"   → any corporate/school Microsoft account, no personal
//
// Set MS_TENANT_ID in the environment to restrict to your company's tenant.
// Find your tenant ID in Azure Portal → Azure Active Directory → Overview.
func msTenant() string {
	if t := os.Getenv("MS_TENANT_ID"); t != "" {
		return t
	}
	return "common"
}

func msAuthURL() string {
	return "https://login.microsoftonline.com/" + msTenant() + "/oauth2/v2.0/authorize"
}
func msTokenURL() string {
	return "https://login.microsoftonline.com/" + msTenant() + "/oauth2/v2.0/token"
}

const (
	msGraphURL = "https://graph.microsoft.com/v1.0/me"
	msScopes   = "openid email profile User.Read"
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
		jsonError(w, "invalid credentials", http.StatusUnauthorized)
		return
	}

	token, err := issueJWT(user)
	if err != nil {
		jsonError(w, "could not generate token", http.StatusInternalServerError)
		return
	}
	jsonOK(w, map[string]any{"token": token, "user": user})
}

// POST /api/v1/auth/logout
func (h *AuthHandler) Logout(w http.ResponseWriter, r *http.Request) {
	jsonOK(w, map[string]string{"message": "logged out"})
}

// GET /api/v1/auth/microsoft
// Returns the Microsoft OAuth redirect URL — the frontend navigates the user there.
func (h *AuthHandler) MicrosoftRedirect(w http.ResponseWriter, r *http.Request) {
	clientID := os.Getenv("MS_CLIENT_ID")
	redirectURI := os.Getenv("MS_REDIRECT_URI")
	if clientID == "" || redirectURI == "" {
		jsonError(w, "Microsoft OAuth not configured (MS_CLIENT_ID / MS_REDIRECT_URI missing)",
			http.StatusNotImplemented)
		return
	}

	state := randomHex(16)
	if err := h.store.SaveOAuthState(state, time.Now().Add(10*time.Minute)); err != nil {
		jsonError(w, "internal server error", http.StatusInternalServerError)
		return
	}

	params := url.Values{
		"client_id":     {clientID},
		"response_type": {"code"},
		"redirect_uri":  {redirectURI},
		"response_mode": {"query"},
		"scope":         {msScopes},
		"state":         {state},
	}
	jsonOK(w, map[string]string{
		"redirectUrl": msAuthURL() + "?" + params.Encode(),
	})
}

// GET /api/v1/auth/microsoft/callback
// Microsoft redirects here after the user authenticates.
func (h *AuthHandler) MicrosoftCallback(w http.ResponseWriter, r *http.Request) {
	clientID := os.Getenv("MS_CLIENT_ID")
	clientSecret := os.Getenv("MS_CLIENT_SECRET")
	redirectURI := os.Getenv("MS_REDIRECT_URI")

	q := r.URL.Query()
	if errParam := q.Get("error"); errParam != "" {
		jsonError(w, "Microsoft auth error: "+q.Get("error_description"), http.StatusBadRequest)
		return
	}

	valid, err := h.store.ConsumeOAuthState(q.Get("state"))
	if err != nil {
		jsonError(w, "internal server error", http.StatusInternalServerError)
		return
	}
	if !valid {
		jsonError(w, "invalid or expired OAuth state", http.StatusBadRequest)
		return
	}

	tokenResp, err := exchangeCode(clientID, clientSecret, redirectURI, q.Get("code"))
	if err != nil {
		jsonError(w, fmt.Sprintf("token exchange failed: %v", err), http.StatusBadGateway)
		return
	}

	msUser, err := fetchMSUser(tokenResp.AccessToken)
	if err != nil {
		jsonError(w, fmt.Sprintf("failed to fetch MS profile: %v", err), http.StatusBadGateway)
		return
	}

	email := msUser.Mail
	if email == "" {
		email = msUser.UserPrincipalName
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

	// Find or create the user record
	user, ok, err := h.store.GetUserByMicrosoftOID(msUser.ID)
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
				Name:         msUser.DisplayName,
				Email:        email,
				Role:         domain.RoleReadOnly,
				MicrosoftOID: msUser.ID,
				Active:       true,
			}
			if err := h.store.CreateUser(user); err != nil {
				jsonError(w, "internal server error", http.StatusInternalServerError)
				return
			}
		} else {
			user.MicrosoftOID = msUser.ID
			h.store.UpdateUser(user) //nolint — best-effort link
		}
	}

	token, err := issueJWT(user)
	if err != nil {
		jsonError(w, "could not generate token", http.StatusInternalServerError)
		return
	}

	// Redirect browser back to the frontend with the JWT in the query string.
	frontendURL := os.Getenv("FRONTEND_URL")
	if frontendURL == "" {
		frontendURL = "http://localhost:5173"
	}
	redir := url.Values{"token": {token}}
	http.Redirect(w, r, frontendURL+"?"+redir.Encode(), http.StatusFound)
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

func randomHex(n int) string {
	b := make([]byte, n)
	rand.Read(b) //nolint
	return hex.EncodeToString(b)
}

type msTokenResponse struct {
	AccessToken string `json:"access_token"`
}

func exchangeCode(clientID, clientSecret, redirectURI, code string) (*msTokenResponse, error) {
	form := url.Values{
		"client_id":     {clientID},
		"client_secret": {clientSecret},
		"grant_type":    {"authorization_code"},
		"code":          {code},
		"redirect_uri":  {redirectURI},
		"scope":         {msScopes},
	}
	resp, err := http.PostForm(msTokenURL(), form)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("status %d: %s", resp.StatusCode, body)
	}
	var t msTokenResponse
	return &t, json.NewDecoder(resp.Body).Decode(&t)
}

type msGraphUser struct {
	ID                string `json:"id"`
	DisplayName       string `json:"displayName"`
	Mail              string `json:"mail"`
	UserPrincipalName string `json:"userPrincipalName"`
}

func fetchMSUser(accessToken string) (*msGraphUser, error) {
	req, _ := http.NewRequest(http.MethodGet, msGraphURL, nil)
	req.Header.Set("Authorization", "Bearer "+accessToken)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("graph status %d: %s", resp.StatusCode, body)
	}
	var u msGraphUser
	if err := json.NewDecoder(resp.Body).Decode(&u); err != nil {
		return nil, err
	}
	if u.Mail == "" && strings.Contains(u.UserPrincipalName, "@") {
		u.Mail = u.UserPrincipalName
	}
	return &u, nil
}
