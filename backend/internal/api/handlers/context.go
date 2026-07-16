package handlers

import (
	"context"
	"net/http"
)

type contextKey string

const usernameContextKey contextKey = "username"
const roleContextKey contextKey = "role"

func withUser(r *http.Request, username, role string) *http.Request {
	ctx := context.WithValue(r.Context(), usernameContextKey, username)
	ctx = context.WithValue(ctx, roleContextKey, role)
	return r.WithContext(ctx)
}

func usernameFromRequest(r *http.Request) (string, bool) {
	username, ok := r.Context().Value(usernameContextKey).(string)
	return username, ok && username != ""
}

func roleFromRequest(r *http.Request) (string, bool) {
	role, ok := r.Context().Value(roleContextKey).(string)
	return role, ok && role != ""
}
