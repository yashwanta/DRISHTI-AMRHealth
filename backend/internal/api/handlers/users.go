package handlers

import (
	"encoding/json"
	"net/http"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"golang.org/x/crypto/bcrypt"
)

var allowedUserRoles = map[string]bool{
	"Super Admin": true, "Global Admin": true, "Global Admin Read Only": true,
	"Location Admin": true, "IT User": true,
}

type appUserResponse struct {
	ID          int64     `json:"id"`
	Username    string    `json:"username"`
	Role        string    `json:"role"`
	Location    string    `json:"location"`
	Status      string    `json:"status"`
	CreatedAt   time.Time `json:"created_at"`
	UpdatedAt   time.Time `json:"updated_at"`
	Permissions []string  `json:"permissions"`
}

type appUserRequest struct {
	Username    string   `json:"username"`
	Password    string   `json:"password"`
	Role        string   `json:"role"`
	Location    string   `json:"location"`
	Status      string   `json:"status"`
	Permissions []string `json:"permissions"`
}

func validPermissions(values []string) bool {
	allowed := map[string]bool{"users": true, "discovery": true, "heatmap": true, "servers": true, "sync": true, "change_password": true}
	for _, value := range values {
		if !allowed[value] {
			return false
		}
	}
	return true
}

func (h *AuthHandler) ListUsers(w http.ResponseWriter, r *http.Request) {
	rows, err := h.db.Query(r.Context(), `SELECT id, username, role, location, status, created_at, updated_at, COALESCE(permissions,'[]'::jsonb) FROM app_users ORDER BY username`)
	if err != nil {
		jsonError(w, "could not load users", http.StatusInternalServerError)
		return
	}
	defer rows.Close()
	users := []appUserResponse{}
	for rows.Next() {
		var user appUserResponse
		var rawPermissions []byte
		if err := rows.Scan(&user.ID, &user.Username, &user.Role, &user.Location, &user.Status, &user.CreatedAt, &user.UpdatedAt, &rawPermissions); err != nil {
			jsonError(w, "could not load users", http.StatusInternalServerError)
			return
		}
		_ = json.Unmarshal(rawPermissions, &user.Permissions)
		if user.Role == "Super Admin" {
			user.Permissions = allAdminPermissions()
		}
		users = append(users, user)
	}
	jsonOK(w, users)
}

func (h *AuthHandler) CreateUser(w http.ResponseWriter, r *http.Request) {
	var req appUserRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		jsonError(w, "invalid body", http.StatusBadRequest)
		return
	}
	req.Username = strings.TrimSpace(req.Username)
	req.Location = strings.TrimSpace(req.Location)
	if len(req.Username) < 3 {
		jsonError(w, "username must be at least 3 characters", http.StatusBadRequest)
		return
	}
	if !allowedUserRoles[req.Role] {
		jsonError(w, "invalid role", http.StatusBadRequest)
		return
	}
	if !validPermissions(req.Permissions) {
		jsonError(w, "invalid permission", http.StatusBadRequest)
		return
	}
	actorRole, _ := roleFromRequest(r)
	if req.Role == "Super Admin" && actorRole != "Super Admin" {
		jsonError(w, "only a Super Admin can assign the Super Admin role", http.StatusForbidden)
		return
	}
	if err := validatePasswordComplexity(req.Password); err != nil {
		jsonError(w, err.Error(), http.StatusBadRequest)
		return
	}
	if req.Status == "" {
		req.Status = "active"
	}
	if req.Status != "active" && req.Status != "disabled" {
		jsonError(w, "invalid status", http.StatusBadRequest)
		return
	}
	hash, err := bcrypt.GenerateFromPassword([]byte(req.Password), bcrypt.DefaultCost)
	if err != nil {
		jsonError(w, "could not secure password", http.StatusInternalServerError)
		return
	}
	var user appUserResponse
	permissionsJSON, _ := json.Marshal(req.Permissions)
	err = h.db.QueryRow(r.Context(), `INSERT INTO app_users (username,password_hash,role,location,status,permissions) VALUES ($1,$2,$3,$4,$5,$6) RETURNING id,username,role,location,status,created_at,updated_at`, req.Username, string(hash), req.Role, req.Location, req.Status, permissionsJSON).Scan(&user.ID, &user.Username, &user.Role, &user.Location, &user.Status, &user.CreatedAt, &user.UpdatedAt)
	if err != nil {
		jsonError(w, "username already exists or user could not be created", http.StatusConflict)
		return
	}
	jsonOK(w, user)
}

func (h *AuthHandler) UpdateUser(w http.ResponseWriter, r *http.Request) {
	var req appUserRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		jsonError(w, "invalid body", http.StatusBadRequest)
		return
	}
	if !allowedUserRoles[req.Role] {
		jsonError(w, "invalid role", http.StatusBadRequest)
		return
	}
	if !validPermissions(req.Permissions) {
		jsonError(w, "invalid permission", http.StatusBadRequest)
		return
	}
	actorRole, _ := roleFromRequest(r)
	if req.Role == "Super Admin" && actorRole != "Super Admin" {
		jsonError(w, "only a Super Admin can assign the Super Admin role", http.StatusForbidden)
		return
	}
	if req.Status != "active" && req.Status != "disabled" {
		jsonError(w, "invalid status", http.StatusBadRequest)
		return
	}
	currentUsername, _ := usernameFromRequest(r)
	var targetUsername, targetRole string
	if err := h.db.QueryRow(r.Context(), `SELECT username, role FROM app_users WHERE id=$1`, chi.URLParam(r, "id")).Scan(&targetUsername, &targetRole); err != nil {
		jsonError(w, "user not found", http.StatusNotFound)
		return
	}
	if targetRole == "Super Admin" && actorRole != "Super Admin" {
		jsonError(w, "only a Super Admin can modify a Super Admin account", http.StatusForbidden)
		return
	}
	if targetUsername == currentUsername && (req.Status != "active" || !canAdmin(req.Role)) {
		jsonError(w, "you cannot disable your own account or remove your own admin access", http.StatusBadRequest)
		return
	}
	var user appUserResponse
	permissionsJSON, _ := json.Marshal(req.Permissions)
	if strings.TrimSpace(req.Password) == "" {
		err := h.db.QueryRow(r.Context(), `UPDATE app_users SET role=$1,location=$2,status=$3,permissions=$4,updated_at=NOW() WHERE id=$5 RETURNING id,username,role,location,status,created_at,updated_at`, req.Role, strings.TrimSpace(req.Location), req.Status, permissionsJSON, chi.URLParam(r, "id")).Scan(&user.ID, &user.Username, &user.Role, &user.Location, &user.Status, &user.CreatedAt, &user.UpdatedAt)
		if err != nil {
			jsonError(w, "could not update user", http.StatusInternalServerError)
			return
		}
	} else {
		if err := validatePasswordComplexity(req.Password); err != nil {
			jsonError(w, err.Error(), http.StatusBadRequest)
			return
		}
		hash, err := bcrypt.GenerateFromPassword([]byte(req.Password), bcrypt.DefaultCost)
		if err != nil {
			jsonError(w, "could not secure password", http.StatusInternalServerError)
			return
		}
		err = h.db.QueryRow(r.Context(), `UPDATE app_users SET password_hash=$1,role=$2,location=$3,status=$4,permissions=$5,updated_at=NOW() WHERE id=$6 RETURNING id,username,role,location,status,created_at,updated_at`, string(hash), req.Role, strings.TrimSpace(req.Location), req.Status, permissionsJSON, chi.URLParam(r, "id")).Scan(&user.ID, &user.Username, &user.Role, &user.Location, &user.Status, &user.CreatedAt, &user.UpdatedAt)
		if err != nil {
			jsonError(w, "could not update user", http.StatusInternalServerError)
			return
		}
	}
	jsonOK(w, user)
}
