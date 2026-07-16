package playbook

import (
	"context"
	"fmt"
	"strings"

	"github.com/jackc/pgx/v5/pgxpool"
)

// HostFilter selects which hosts a batch job targets.
// If ServerIDs is non-empty it is used directly. Otherwise the tag/asset-type
// filters expand to all matching servers.
type HostFilter struct {
	ServerIDs []int   `json:"server_ids"`
	Tags      []string `json:"tags"`
	AssetType string  `json:"asset_type"` // "server" | "endpoint" | "" (any)
	OSType    string  `json:"os_type"`    // "linux" | "windows" | "" (any)
}

// ResolveTargets loads matching hosts from the DB and decrypts their
// credentials, returning ready-to-use SSH targets.
func ResolveTargets(ctx context.Context, db *pgxpool.Pool, key string, f HostFilter) ([]HostTarget, error) {
	rows, err := db.Query(ctx, `
		SELECT id, name, host, port, username, auth_type,
		       COALESCE(password_enc,''), COALESCE(private_key_enc,''),
		       asset_type, COALESCE(tags,''), COALESCE(os_type,'linux')
		FROM servers ORDER BY name`)
	if err != nil {
		return nil, fmt.Errorf("query servers: %w", err)
	}
	defer rows.Close()

	idSet := map[int]bool{}
	for _, id := range f.ServerIDs {
		idSet[id] = true
	}

	tagSet := map[string]bool{}
	for _, t := range f.Tags {
		tagSet[strings.ToLower(strings.TrimSpace(t))] = true
	}

	var out []HostTarget
	for rows.Next() {
		var (
			id          int
			name        string
			host        string
			port        int
			username    string
			authType    string
			passEnc     string
			keyEnc      string
			assetType   string
			tagsRaw     string
			osType      string
		)
		if err := rows.Scan(&id, &name, &host, &port, &username, &authType, &passEnc, &keyEnc, &assetType, &tagsRaw, &osType); err != nil {
			return nil, fmt.Errorf("scan server row: %w", err)
		}

		// ID filter takes priority.
		if len(idSet) > 0 {
			if !idSet[id] {
				continue
			}
		} else {
			// Apply asset-type filter.
			if f.AssetType != "" && assetType != f.AssetType {
				continue
			}
			// Apply OS filter.
			if f.OSType != "" && osType != f.OSType {
				continue
			}
			// Apply tag filter (ALL specified tags must be present).
			if len(tagSet) > 0 {
				serverTags := parseTags(tagsRaw)
				matched := true
				for want := range tagSet {
					if !serverTags[want] {
						matched = false
						break
					}
				}
				if !matched {
					continue
				}
			}
		}

		ht := HostTarget{ServerID: id, ServerName: name, Host: host, Port: port, Username: username, AuthType: authType}

		if passEnc != "" {
			pw, derr := decryptString(key, passEnc)
			if derr != nil {
				return nil, fmt.Errorf("decrypt password for %s: %w", name, derr)
			}
			ht.Password = pw
		}
		if keyEnc != "" {
			pk, derr := decryptString(key, keyEnc)
			if derr != nil {
				return nil, fmt.Errorf("decrypt private key for %s: %w", name, derr)
			}
			ht.PrivateKey = pk
		}
		out = append(out, ht)
	}
	return out, nil
}

// parseTags splits the comma-separated tags column into a lowercase set.
func parseTags(raw string) map[string]bool {
	out := map[string]bool{}
	for _, part := range strings.Split(raw, ",") {
		part = strings.ToLower(strings.TrimSpace(part))
		if part != "" {
			out[part] = true
		}
	}
	return out
}
