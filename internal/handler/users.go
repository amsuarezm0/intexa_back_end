package handler

import (
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/intexa/arca-api/internal/domain"
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

func (h *UsersHandler) Create(w http.ResponseWriter, r *http.Request) {
	var u domain.User
	if err := decode(r, &u); err != nil {
		jsonError(w, "invalid request body", http.StatusBadRequest)
		return
	}
	if u.Password == "" {
		jsonError(w, "password is required", http.StatusBadRequest)
		return
	}
	hashed, err := bcrypt.GenerateFromPassword([]byte(u.Password), bcrypt.DefaultCost)
	if err != nil {
		jsonError(w, "internal server error", http.StatusInternalServerError)
		return
	}
	u.Password = string(hashed)
	if err := h.store.CreateUser(&u); err != nil {
		jsonError(w, "internal server error", http.StatusInternalServerError)
		return
	}
	jsonCreated(w, u)
}

func (h *UsersHandler) Update(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	var u domain.User
	if err := decode(r, &u); err != nil {
		jsonError(w, "invalid request body", http.StatusBadRequest)
		return
	}
	u.ID = id
	ok, err := h.store.UpdateUser(&u)
	if err != nil {
		jsonError(w, "internal server error", http.StatusInternalServerError)
		return
	}
	if !ok {
		jsonError(w, "user not found", http.StatusNotFound)
		return
	}
	jsonOK(w, u)
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
	jsonOK(w, map[string]string{"message": "deleted"})
}
