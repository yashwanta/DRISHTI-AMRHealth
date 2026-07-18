package handlers

import (
	"context"
	"net/http"
)

type contextKey string

const usernameContextKey contextKey = "username"
const roleContextKey contextKey = "role"
const permissionsContextKey contextKey = "permissions"

func withUser(r *http.Request, username, role string) *http.Request {
	ctx := context.WithValue(r.Context(), usernameContextKey, username)
	ctx = context.WithValue(ctx, roleContextKey, role)
	return r.WithContext(ctx)
}

func withPermissions(r *http.Request, permissions []string) *http.Request {
	return r.WithContext(context.WithValue(r.Context(), permissionsContextKey, permissions))
}

func permissionsFromRequest(r *http.Request) []string {
	permissions, _ := r.Context().Value(permissionsContextKey).([]string)
	return permissions
}

func usernameFromRequest(r *http.Request) (string, bool) {
	username, ok := r.Context().Value(usernameContextKey).(string)
	return username, ok && username != ""
}

func roleFromRequest(r *http.Request) (string, bool) {
	role, ok := r.Context().Value(roleContextKey).(string)
	return role, ok && role != ""
}
