package httpapi

import (
	"context"
	"net/http"
	"strings"
)

type TokenVerifier interface {
	Verify(token string) (string, error)
}

type userIDKey struct{}

const AuthCookieName = "access_token"

func UserIDFromContext(ctx context.Context) (string, bool) {
	id, ok := ctx.Value(userIDKey{}).(string)
	return id, ok && id != ""
}

func WithAuth(verifier TokenVerifier, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		cookies := r.CookiesNamed(AuthCookieName)
		if len(cookies) != 1 {
			unauthorized(w)
			return
		}
		userID, err := verifier.Verify(cookies[0].Value)
		if err != nil || strings.TrimSpace(userID) == "" {
			unauthorized(w)
			return
		}
		ctx := context.WithValue(r.Context(), userIDKey{}, userID)
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

func unauthorized(w http.ResponseWriter) {
	w.Header().Set("Cache-Control", "no-store")
	writeError(w, http.StatusUnauthorized, "Требуется авторизация")
}
