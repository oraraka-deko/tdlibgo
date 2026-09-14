package auth

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/go-pkgz/auth/v2/token"
)

func TestOAuthManager_TokensAndMiddleware(t *testing.T) {
	cfg := Config{
		Enabled:        true,
		URL:            "http://localhost:22816",
		Secret:         "test-secret-key-at-least-32-bytes-long",
		TokenDuration:  15 * time.Minute,
		CookieDuration: 30 * 24 * time.Hour,
		GoogleClientID: "google-dummy-id",
		GoogleClientSecret: "google-dummy-secret",
	}

	mgr, err := NewOAuthManager(cfg)
	if err != nil {
		t.Fatalf("failed to create OAuthManager: %v", err)
	}

	if !mgr.Enabled() {
		t.Errorf("expected manager to be enabled")
	}

	providers := mgr.AvailableProviders()
	if len(providers) == 0 || providers[0] != "google" {
		t.Errorf("expected google provider to be available, got: %v", providers)
	}

	// Test issuing an access token
	user := token.User{
		ID:   "google_12345",
		Name: "Test User",
	}

	accessToken, ttl, err := mgr.IssueAccessToken(user, "google")
	if err != nil {
		t.Fatalf("failed to issue access token: %v", err)
	}

	if accessToken == "" || ttl <= 0 {
		t.Errorf("invalid access token or ttl: %s (%d)", accessToken, ttl)
	}

	// Verify the access token
	parsedClaims, err := mgr.tokenService.Parse(accessToken)
	if err != nil {
		t.Fatalf("failed to parse access token: %v", err)
	}
	if parsedClaims.User == nil || parsedClaims.User.Name != "Test User" {
		t.Errorf("unexpected parsed user: %+v", parsedClaims.User)
	}

	// Test Middleware rejection without token
	handler := mgr.Middleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("ok"))
	}))

	req := httptest.NewRequest("GET", "/api/chats", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusUnauthorized {
		t.Errorf("expected status 401 Unauthorized without token, got %d", rec.Code)
	}

	// Test Middleware acceptance with Bearer token
	reqAuth := httptest.NewRequest("GET", "/api/chats", nil)
	reqAuth.Header.Set("Authorization", "Bearer "+accessToken)
	recAuth := httptest.NewRecorder()
	handler.ServeHTTP(recAuth, reqAuth)

	if recAuth.Code != http.StatusOK {
		t.Errorf("expected status 200 OK with Bearer token, got %d", recAuth.Code)
	}

	// Test Middleware acceptance with query token
	reqQuery := httptest.NewRequest("GET", "/ws?token="+accessToken, nil)
	recQuery := httptest.NewRecorder()
	handler.ServeHTTP(recQuery, reqQuery)

	if recQuery.Code != http.StatusOK {
		t.Errorf("expected status 200 OK with query token, got %d", recQuery.Code)
	}
}
