package handlers

import (
	"encoding/json"
	"net/http"
	"strconv"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"drishti-amr-health/internal/models"
)

type ServerHandler struct {
	db            *pgxpool.Pool
	encryptionKey string
}

func NewServerHandler(db *pgxpool.Pool, key string) *ServerHandler {
	return &ServerHandler{db: db, encryptionKey: key}
}

func (h *ServerHandler) List(w http.ResponseWriter, r *http.Request) {
	rows, err := h.db.Query(r.Context(), `
		SELECT id, name, host, port, username, auth_type, asset_type,
		       proxmox_host, proxmox_port, proxmox_username, proxmox_auth_type, vmid, app_log_paths, os_type,
		       last_sync_at, status, created_at
		FROM servers ORDER BY name`)
	if err != nil {
		internalError(w, err)
		return
	}
	defer rows.Close()

	var servers []models.Server
	for rows.Next() {
		var s models.Server
		if err := rows.Scan(&s.ID, &s.Name, &s.Host, &s.Port, &s.Username,
			&s.AuthType, &s.AssetType, &s.ProxmoxHost, &s.ProxmoxPort, &s.ProxmoxUsername, &s.ProxmoxAuthType,
			&s.VMID, &s.AppLogPaths, &s.OSType, &s.LastSyncAt, &s.Status, &s.CreatedAt); err != nil {
			internalError(w, err)
			return
		}
		servers = append(servers, s)
	}
	if servers == nil {
		servers = []models.Server{}
	}
	jsonOK(w, servers)
}

func (h *ServerHandler) Create(w http.ResponseWriter, r *http.Request) {
	var req models.ServerRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		jsonError(w, "invalid body", http.StatusBadRequest)
		return
	}
	if req.Name == "" || req.Host == "" || req.Username == "" {
		jsonError(w, "name, host, and username are required", http.StatusBadRequest)
		return
	}
	if req.Port == 0 {
		req.Port = 22
	}
	if req.ProxmoxPort == 0 {
		req.ProxmoxPort = 22
	}
	if req.ProxmoxAuthType == "" {
		req.ProxmoxAuthType = "password"
	}
	req.AssetType = normalizeAssetType(req.AssetType)

	var passEnc, keyEnc, proxPassEnc, proxKeyEnc string
	var err error
	if req.AuthType == "key" && req.PrivateKey != "" {
		keyEnc, err = encrypt(h.encryptionKey, req.PrivateKey)
		if err != nil {
			jsonError(w, "encryption error", http.StatusInternalServerError)
			return
		}
	} else if req.Password != "" {
		passEnc, err = encrypt(h.encryptionKey, req.Password)
		if err != nil {
			jsonError(w, "encryption error", http.StatusInternalServerError)
			return
		}
	}
	if req.ProxmoxAuthType == "key" && req.ProxmoxPrivateKey != "" {
		proxKeyEnc, err = encrypt(h.encryptionKey, req.ProxmoxPrivateKey)
		if err != nil {
			jsonError(w, "encryption error", http.StatusInternalServerError)
			return
		}
	} else if req.ProxmoxPassword != "" {
		proxPassEnc, err = encrypt(h.encryptionKey, req.ProxmoxPassword)
		if err != nil {
			jsonError(w, "encryption error", http.StatusInternalServerError)
			return
		}
	}

	var s models.Server
	err = h.db.QueryRow(r.Context(), `
		INSERT INTO servers (
			name, host, port, username, auth_type, password_enc, private_key_enc, asset_type,
			proxmox_host, proxmox_port, proxmox_username, proxmox_auth_type, proxmox_password_enc, proxmox_private_key_enc,
			vmid, app_log_paths, os_type
		)
		VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,$15,$16,$17)
		RETURNING id, name, host, port, username, auth_type, asset_type,
		          proxmox_host, proxmox_port, proxmox_username, proxmox_auth_type, vmid, app_log_paths, os_type,
		          last_sync_at, status, created_at`,
		req.Name, req.Host, req.Port, req.Username, req.AuthType, passEnc, keyEnc,
		req.AssetType,
		req.ProxmoxHost, req.ProxmoxPort, req.ProxmoxUsername, req.ProxmoxAuthType, proxPassEnc, proxKeyEnc,
		req.VMID, req.AppLogPaths, normalizeOSType(req.OSType),
	).Scan(&s.ID, &s.Name, &s.Host, &s.Port, &s.Username, &s.AuthType,
		&s.AssetType, &s.ProxmoxHost, &s.ProxmoxPort, &s.ProxmoxUsername, &s.ProxmoxAuthType, &s.VMID, &s.AppLogPaths, &s.OSType,
		&s.LastSyncAt, &s.Status, &s.CreatedAt)
	if err != nil {
		internalError(w, err)
		return
	}
	w.WriteHeader(http.StatusCreated)
	jsonOK(w, s)
}

func (h *ServerHandler) Update(w http.ResponseWriter, r *http.Request) {
	id, _ := strconv.Atoi(chi.URLParam(r, "id"))
	var req models.ServerRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		jsonError(w, "invalid body", http.StatusBadRequest)
		return
	}
	if req.Port == 0 {
		req.Port = 22
	}
	if req.ProxmoxPort == 0 {
		req.ProxmoxPort = 22
	}
	if req.ProxmoxAuthType == "" {
		req.ProxmoxAuthType = "password"
	}
	req.AssetType = normalizeAssetType(req.AssetType)

	var passEnc, keyEnc, proxPassEnc, proxKeyEnc string
	var err error
	if req.AuthType == "key" && req.PrivateKey != "" {
		keyEnc, err = encrypt(h.encryptionKey, req.PrivateKey)
		if err != nil {
			jsonError(w, "encryption error", http.StatusInternalServerError)
			return
		}
	} else if req.Password != "" {
		passEnc, err = encrypt(h.encryptionKey, req.Password)
		if err != nil {
			jsonError(w, "encryption error", http.StatusInternalServerError)
			return
		}
	}
	if req.ProxmoxAuthType == "key" && req.ProxmoxPrivateKey != "" {
		proxKeyEnc, err = encrypt(h.encryptionKey, req.ProxmoxPrivateKey)
		if err != nil {
			jsonError(w, "encryption error", http.StatusInternalServerError)
			return
		}
	} else if req.ProxmoxPassword != "" {
		proxPassEnc, err = encrypt(h.encryptionKey, req.ProxmoxPassword)
		if err != nil {
			jsonError(w, "encryption error", http.StatusInternalServerError)
			return
		}
	}

	var s models.Server
	err = h.db.QueryRow(r.Context(), `
		UPDATE servers SET name=$1, host=$2, port=$3, username=$4, auth_type=$5,
		password_enc=CASE WHEN $6='' THEN password_enc ELSE $6 END,
		private_key_enc=CASE WHEN $7='' THEN private_key_enc ELSE $7 END,
		asset_type=$8,
		proxmox_host=$9, proxmox_port=$10, proxmox_username=$11, proxmox_auth_type=$12,
		proxmox_password_enc=CASE WHEN $13='' THEN proxmox_password_enc ELSE $13 END,
		proxmox_private_key_enc=CASE WHEN $14='' THEN proxmox_private_key_enc ELSE $14 END,
		vmid=$15, app_log_paths=$16, os_type=$17
		WHERE id=$18
		RETURNING id, name, host, port, username, auth_type, asset_type,
		          proxmox_host, proxmox_port, proxmox_username, proxmox_auth_type, vmid, app_log_paths,
		          last_sync_at, status, created_at`,
		req.Name, req.Host, req.Port, req.Username, req.AuthType, passEnc, keyEnc,
		req.AssetType,
		req.ProxmoxHost, req.ProxmoxPort, req.ProxmoxUsername, req.ProxmoxAuthType, proxPassEnc, proxKeyEnc,
		req.VMID, req.AppLogPaths, normalizeOSType(req.OSType), id,
	).Scan(&s.ID, &s.Name, &s.Host, &s.Port, &s.Username, &s.AuthType,
		&s.AssetType, &s.ProxmoxHost, &s.ProxmoxPort, &s.ProxmoxUsername, &s.ProxmoxAuthType, &s.VMID, &s.AppLogPaths, &s.OSType,
		&s.LastSyncAt, &s.Status, &s.CreatedAt)
	if err != nil {
		internalError(w, err)
		return
	}
	jsonOK(w, s)
}

func (h *ServerHandler) Delete(w http.ResponseWriter, r *http.Request) {
	id, _ := strconv.Atoi(chi.URLParam(r, "id"))
	_, err := h.db.Exec(r.Context(), `DELETE FROM servers WHERE id=$1`, id)
	if err != nil {
		internalError(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (h *ServerHandler) GetCredentials(serverID int, ctx interface{ Done() <-chan struct{} }) (models.ServerRequest, error) {
	return models.ServerRequest{}, nil
}

func normalizeAssetType(value string) string {
	if value == "endpoint" {
		return "endpoint"
	}
	return "server"
}

func normalizeOSType(value string) string {
	if value == "windows" {
		return "windows"
	}
	return "linux"
}
