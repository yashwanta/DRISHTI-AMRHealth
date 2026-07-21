package handlers

import (
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"strconv"
	"strings"
	"time"

	"drishti-amr-health/internal/models"
	amrssh "drishti-amr-health/internal/ssh"
	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

type amrTCPDiagnosticRequest struct {
	IP    string `json:"ip"`
	Ports []int  `json:"ports"`
}

func (h *ServerHandler) DiagnoseAMRTCP(w http.ResponseWriter, r *http.Request) {
	var req amrTCPDiagnosticRequest
	if json.NewDecoder(r.Body).Decode(&req) != nil {
		jsonError(w, "invalid diagnostic request", http.StatusBadRequest)
		return
	}
	ip := net.ParseIP(strings.TrimSpace(req.IP)).To4()
	if ip == nil || ip[0] != 10 || ip[1] != 222 || ip[2] == 10 {
		jsonError(w, "diagnostics are limited to Springfield AMR addresses", http.StatusBadRequest)
		return
	}
	ports := []int{}
	seen := map[int]bool{}
	for _, port := range req.Ports {
		if port >= 1 && port <= 65535 && !seen[port] && len(ports) < 20 {
			ports = append(ports, port)
			seen[port] = true
		}
	}
	if len(ports) == 0 {
		jsonError(w, "at least one valid TCP port is required", http.StatusBadRequest)
		return
	}

	var serverID, sshPort int
	var name, host, username, authType, passwordEnc, privateKeyEnc string
	err := h.db.QueryRow(r.Context(), `SELECT id,name,host,port,username,auth_type,COALESCE(password_enc,''),COALESCE(private_key_enc,'') FROM servers WHERE host='10.222.10.76' OR name ILIKE '%springfield%' ORDER BY CASE WHEN host='10.222.10.76' THEN 0 ELSE 1 END LIMIT 1`).Scan(&serverID, &name, &host, &sshPort, &username, &authType, &passwordEnc, &privateKeyEnc)
	if err != nil {
		jsonError(w, "Springfield Fleet Manager SSH connection is not configured", http.StatusUnprocessableEntity)
		return
	}
	password, privateKey := "", ""
	if passwordEnc != "" {
		password, err = decrypt(h.encryptionKey, passwordEnc)
	}
	if err == nil && privateKeyEnc != "" {
		privateKey, err = decrypt(h.encryptionKey, privateKeyEnc)
	}
	if err != nil {
		jsonError(w, "could not decrypt Fleet Manager credentials", http.StatusInternalServerError)
		return
	}
	client, err := amrssh.Connect(amrssh.Config{Host: host, Port: sshPort, Username: username, AuthType: authType, Password: password, PrivateKey: privateKey})
	if err != nil {
		jsonError(w, "Fleet Manager SSH connection failed: "+err.Error(), http.StatusBadGateway)
		return
	}
	defer client.Close()

	ipText := ip.String()
	portText := make([]string, len(ports))
	for i, port := range ports {
		portText[i] = strconv.Itoa(port)
	}
	command := fmt.Sprintf(`echo '=== ICMP ==='; ping -c 2 -W 1 %s 2>&1 || true; echo '=== NEIGHBOR ==='; ip neigh show %s 2>&1 || true; echo '=== SOCKETS ==='; ss -Htan dst %s 2>&1 || true; echo '=== TCP PORTS ==='; for p in %s; do if timeout 3 bash -c "</dev/tcp/%s/$p" 2>/dev/null; then echo "$p OPEN"; else echo "$p NO RESPONSE"; fi; done`, ipText, ipText, ipText, strings.Join(portText, " "), ipText)
	output, runErr := client.Run(command)
	status := "success"
	errText := ""
	if runErr != nil {
		status, errText = "failed", runErr.Error()
	}
	if len(output) > 12000 {
		output = output[:12000] + "\n[output truncated]"
	}
	_, _ = h.db.Exec(r.Context(), `INSERT INTO action_runs(server_id,action,command,status,output,error,created_by) VALUES($1,'amr_tcp_diagnostic',$2,$3,$4,$5,$6)`, serverID, "read-only TCP diagnostic for "+ipText, status, output, errText, createdBy(r))
	jsonOK(w, map[string]any{"ok": runErr == nil, "ip": ipText, "server": name, "ports": ports, "output": output, "error": errText, "checked_at": time.Now().UTC()})
}

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
		          proxmox_host, proxmox_port, proxmox_username, proxmox_auth_type, vmid, app_log_paths, os_type,
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
