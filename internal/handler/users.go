package handler

import (
	"net/http"
	"strings"

	"github.com/go-chi/chi/v5"
	"github.com/intexa/arca-api/internal/domain"
	"github.com/intexa/arca-api/internal/middleware"
	"github.com/intexa/arca-api/internal/repository"
	"golang.org/x/crypto/bcrypt"
)

type UsersHandler struct {
	store repository.Store
}

func NewUsersHandler(store repository.Store) *UsersHandler {
	return &UsersHandler{store: store}
}

func (h *UsersHandler) List(w http.ResponseWriter, r *http.Request) {
	users, err := h.store.GetAllUsers()
	if err != nil {
		jsonError(w, "internal server error", http.StatusInternalServerError)
		return
	}
	jsonOK(w, users)
}

type createUserRequest struct {
	Name     string `json:"name"`
	Email    string `json:"email"`
	Role     string `json:"role"`
	Password string `json:"password"`
}

func (h *UsersHandler) Create(w http.ResponseWriter, r *http.Request) {
	var req createUserRequest
	if err := decode(r, &req); err != nil {
		jsonError(w, "invalid request body", http.StatusBadRequest)
		return
	}
	if req.Password == "" {
		jsonError(w, "password is required", http.StatusBadRequest)
		return
	}
	hashed, err := bcrypt.GenerateFromPassword([]byte(req.Password), bcrypt.DefaultCost)
	if err != nil {
		jsonError(w, "internal server error", http.StatusInternalServerError)
		return
	}
	u := domain.User{
		Name:     req.Name,
		Email:    req.Email,
		Role:     domain.UserRole(req.Role),
		Password: string(hashed),
	}
	if err := h.store.CreateUser(&u); err != nil {
		jsonError(w, "internal server error", http.StatusInternalServerError)
		return
	}
	actor, initial := actorFrom(r)
	h.store.AddActivityLog(domain.ActivityLog{ //nolint
		UserName: actor, Initial: initial, Action: "Creó usuario",
		Module: "Usuarios", Color: "bg-green-500",
	})
	jsonCreated(w, u)
}

type updateUserRequest struct {
	Name     string `json:"name"`
	Role     string `json:"role"`
	Password string `json:"password"` // optional — when set, the password is changed
}

func (h *UsersHandler) Update(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	var req updateUserRequest
	if err := decode(r, &req); err != nil {
		jsonError(w, "invalid request body", http.StatusBadRequest)
		return
	}

	// Self-service path: non-admins may update only their own record, and only
	// the password — never name or role (which would allow privilege escalation).
	claims, _ := middleware.ClaimsFromContext(r.Context())
	role, _ := claims["role"].(string)
	if !strings.EqualFold(role, string(domain.RoleAdmin)) {
		selfID, _ := claims["sub"].(string)
		if id != selfID {
			jsonError(w, "forbidden", http.StatusForbidden)
			return
		}
		if req.Password == "" {
			jsonError(w, "password is required", http.StatusBadRequest)
			return
		}
		req.Name = ""
		req.Role = ""
	}

	// Load the existing user so unspecified fields (e.g. active) are preserved.
	user, ok, err := h.store.GetUserByID(id)
	if err != nil {
		jsonError(w, "internal server error", http.StatusInternalServerError)
		return
	}
	if !ok {
		jsonError(w, "user not found", http.StatusNotFound)
		return
	}
	if req.Name != "" {
		user.Name = req.Name
	}
	if req.Role != "" {
		user.Role = domain.UserRole(req.Role)
	}
	if _, err := h.store.UpdateUser(user); err != nil {
		jsonError(w, "internal server error", http.StatusInternalServerError)
		return
	}

	if req.Password != "" {
		hashed, err := bcrypt.GenerateFromPassword([]byte(req.Password), bcrypt.DefaultCost)
		if err != nil {
			jsonError(w, "internal server error", http.StatusInternalServerError)
			return
		}
		if _, err := h.store.UpdatePassword(id, string(hashed)); err != nil {
			jsonError(w, "internal server error", http.StatusInternalServerError)
			return
		}
	}

	actor, initial := actorFrom(r)
	h.store.AddActivityLog(domain.ActivityLog{ //nolint
		UserName: actor, Initial: initial, Action: "Editó usuario",
		Module: "Usuarios", Color: "bg-yellow-500",
	})
	jsonOK(w, user)
}

func (h *UsersHandler) Delete(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	ok, err := h.store.DeleteUser(id)
	if err != nil {
		jsonError(w, "internal server error", http.StatusInternalServerError)
		return
	}
	if !ok {
		jsonError(w, "user not found", http.StatusNotFound)
		return
	}
	actor, initial := actorFrom(r)
	h.store.AddActivityLog(domain.ActivityLog{ //nolint
		UserName: actor, Initial: initial, Action: "Desactivó usuario",
		Module: "Usuarios", Color: "bg-red-500",
	})
	jsonOK(w, map[string]string{"message": "deleted"})
}
