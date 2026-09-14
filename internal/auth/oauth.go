package auth

import (
	"context"
	"crypto/rand"
	"crypto/sha1"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/go-pkgz/auth/v2"
	"github.com/go-pkgz/auth/v2/avatar"
	"github.com/go-pkgz/auth/v2/token"
	"github.com/golang-jwt/jwt/v5"
	"golang.org/x/oauth2"
	oauth2github "golang.org/x/oauth2/github"
	oauth2google "golang.org/x/oauth2/google"

	"tdlibgo/internal/logger"
)

// Config holds configuration parameters for the OAuth2 / JWT stateless session service.
type Config struct {
	Enabled            bool
	URL                string // Base application URL, e.g. "http://localhost:22816"
	Secret             string // HMAC JWT secret
	TokenDuration      time.Duration
	CookieDuration     time.Duration
	SameSite           http.SameSite
	SecureCookies      bool
	GoogleClientID     string
	GoogleClientSecret string
	GoogleRedirectURL  string // e.g. "http://localhost:22816/oauth/v1/redirect/res"
	GithubClientID     string
	GithubClientSecret string
	GithubRedirectURL  string
}

// OAuthManager manages social OAuth providers, JWT token lifecycle, and session cookies.
type OAuthManager struct {
	cfg          Config
	service      *auth.Service
	tokenService *token.Service
	googleConf   *oauth2.Config
	githubConf   *oauth2.Config
}

// CustomOAuthProvider wraps standard oauth2.Config to allow overriding the redirect callback URL
// (e.g. for Google Cloud Console requiring http://localhost:22816/oauth/v1/redirect/res).
type CustomOAuthProvider struct {
	name        string
	conf        *oauth2.Config
	infoURL     string
	mapUser     func(data map[string]any) token.User
	tokenSvc    *token.Service
	issuer      string
	redirectURL string
}

func (c *CustomOAuthProvider) Name() string {
	return c.name
}

func (c *CustomOAuthProvider) LoginHandler(w http.ResponseWriter, r *http.Request) {
	stateBytes := make([]byte, 16)
	if _, err := rand.Read(stateBytes); err != nil {
		http.Error(w, "Failed to generate state", http.StatusInternalServerError)
		return
	}
	state := hex.EncodeToString(stateBytes)

	from := r.URL.Query().Get("from")
	if from == "" {
		from = "/"
	}

	claims := token.Claims{
		RegisteredClaims: jwt.RegisteredClaims{
			Issuer:   c.issuer,
			Audience: jwt.ClaimStrings{"tdlibgo"},
		},
		Handshake: &token.Handshake{
			State: state,
			From:  from,
		},
		SessionOnly: true,
	}

	if _, err := c.tokenSvc.Set(w, claims); err != nil {
		http.Error(w, "Failed to set handshake token: "+err.Error(), http.StatusInternalServerError)
		return
	}

	conf := *c.conf
	if c.redirectURL != "" {
		conf.RedirectURL = c.redirectURL
	}
	authURL := conf.AuthCodeURL(state, oauth2.AccessTypeOffline)
	logger.Info("AUTH", "[OAuth] Redirecting to %s for login, redirect_uri=%s", c.name, conf.RedirectURL)
	http.Redirect(w, r, authURL, http.StatusFound)
}

func (c *CustomOAuthProvider) AuthHandler(w http.ResponseWriter, r *http.Request) {
	c.CompleteAuth(w, r)
}

func (c *CustomOAuthProvider) LogoutHandler(w http.ResponseWriter, r *http.Request) {
	c.tokenSvc.Reset(w)
	http.Redirect(w, r, "/", http.StatusFound)
}

// CompleteAuth exchanges the authorization code for tokens and issues a long-lived session.
func (c *CustomOAuthProvider) CompleteAuth(w http.ResponseWriter, r *http.Request) {
	claims, _, err := c.tokenSvc.Get(r)
	if err != nil || claims.Handshake == nil {
		logger.Error("AUTH", "[OAuth] Invalid or missing handshake token for %s: %v", c.name, err)
		http.Error(w, "Invalid or expired login session", http.StatusForbidden)
		return
	}

	state := r.URL.Query().Get("state")
	if claims.Handshake.State == "" || claims.Handshake.State != state {
		logger.Error("AUTH", "[OAuth] State mismatch for %s: expected %s, got %s", c.name, claims.Handshake.State, state)
		http.Error(w, "State mismatch during authorization", http.StatusForbidden)
		return
	}

	code := r.URL.Query().Get("code")
	if code == "" {
		http.Error(w, "Missing authorization code", http.StatusBadRequest)
		return
	}

	conf := *c.conf
	if c.redirectURL != "" {
		conf.RedirectURL = c.redirectURL
	}

	tok, err := conf.Exchange(context.Background(), code)
	if err != nil {
		logger.Error("AUTH", "[OAuth] Token exchange failed for %s: %v", c.name, err)
		http.Error(w, "Token exchange failed: "+err.Error(), http.StatusInternalServerError)
		return
	}

	client := conf.Client(context.Background(), tok)
	resp, err := client.Get(c.infoURL)
	if err != nil {
		logger.Error("AUTH", "[OAuth] Failed to fetch user info from %s: %v", c.name, err)
		http.Error(w, "Failed to fetch user info", http.StatusServiceUnavailable)
		return
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		logger.Error("AUTH", "[OAuth] User info returned non-200 status %d from %s", resp.StatusCode, c.name)
		http.Error(w, fmt.Sprintf("User info returned status %d", resp.StatusCode), http.StatusServiceUnavailable)
		return
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		http.Error(w, "Failed to read user info", http.StatusInternalServerError)
		return
	}

	var data map[string]any
	if err := json.Unmarshal(body, &data); err != nil {
		http.Error(w, "Failed to parse user info", http.StatusInternalServerError)
		return
	}

	user := c.mapUser(data)

	// Create long-lived session claims (Refresh Token)
	cidBytes := make([]byte, 16)
	_, _ = rand.Read(cidBytes)
	cid := hex.EncodeToString(cidBytes)

	sessionClaims := token.Claims{
		User: &user,
		RegisteredClaims: jwt.RegisteredClaims{
			Issuer:   c.issuer,
			ID:       cid,
			Subject:  user.ID,
			Audience: jwt.ClaimStrings{"tdlibgo"},
		},
		AuthProvider: &token.AuthProvider{
			Name: c.name,
		},
		SessionOnly: false,
	}

	// Write the long-lived refresh session cookie (HttpOnly, SameSite=Strict/Lax, 30 days)
	if _, err := c.tokenSvc.Set(w, sessionClaims); err != nil {
		logger.Error("AUTH", "[OAuth] Failed to set session cookie: %v", err)
		http.Error(w, "Failed to issue session token", http.StatusInternalServerError)
		return
	}

	logger.Info("AUTH", "[OAuth] Successfully logged in %s user %s (%s)", c.name, user.Name, user.ID)

	target := claims.Handshake.From
	if target == "" {
		target = "/"
	}
	http.Redirect(w, r, target, http.StatusSeeOther)
}

// NewOAuthManager constructs and initializes OAuth providers and token services.
func NewOAuthManager(cfg Config) (*OAuthManager, error) {
	if cfg.Secret == "" {
		cfg.Secret = "tdlibgo-secret-token-key-2026-auth-session-32bytes"
	}
	if cfg.URL == "" {
		cfg.URL = "http://localhost:22816"
	}
	if cfg.TokenDuration == 0 {
		cfg.TokenDuration = 15 * time.Minute
	}
	if cfg.CookieDuration == 0 {
		cfg.CookieDuration = 30 * 24 * time.Hour
	}
	if cfg.SameSite == 0 {
		// Use Lax by default to permit incoming redirects from OAuth providers
		cfg.SameSite = http.SameSiteLaxMode
	}

	tokenOpts := token.Opts{
		SecretReader: token.SecretFunc(func(aud string) (string, error) {
			return cfg.Secret, nil
		}),
		TokenDuration:  cfg.TokenDuration,
		CookieDuration: cfg.CookieDuration,
		JWTCookieName:  "JWT",
		JWTHeaderKey:   "X-JWT",
		SendJWTHeader:  true,
		DisableXSRF:    true,
		SecureCookies:  cfg.SecureCookies,
		SameSite:       cfg.SameSite,
		Issuer:         "tdlibgo",
	}
	tokenSvc := token.NewService(tokenOpts)

	authOpts := auth.Opts{
		SecretReader: token.SecretFunc(func(aud string) (string, error) {
			return cfg.Secret, nil
		}),
		TokenDuration:  cfg.TokenDuration,
		CookieDuration: cfg.CookieDuration,
		JWTCookieName:  "JWT",
		JWTHeaderKey:   "X-JWT",
		SendJWTHeader:  true,
		DisableXSRF:    true,
		SecureCookies:  cfg.SecureCookies,
		SameSiteCookie: cfg.SameSite,
		Issuer:         "tdlibgo",
		URL:            cfg.URL,
		AvatarStore:    avatar.NewNoOp(),
	}

	service := auth.NewService(authOpts)

	mgr := &OAuthManager{
		cfg:          cfg,
		service:      service,
		tokenService: tokenSvc,
	}

	// Setup Google Provider
	if cfg.GoogleClientID != "" && cfg.GoogleClientSecret != "" {
		redirectURL := cfg.GoogleRedirectURL
		if redirectURL == "" {
			redirectURL = strings.TrimSuffix(cfg.URL, "/") + "/oauth/v1/redirect/res"
		}

		mgr.googleConf = &oauth2.Config{
			ClientID:     cfg.GoogleClientID,
			ClientSecret: cfg.GoogleClientSecret,
			Endpoint:     oauth2google.Endpoint,
			Scopes:       []string{"https://www.googleapis.com/auth/userinfo.profile", "https://www.googleapis.com/auth/userinfo.email"},
			RedirectURL:  redirectURL,
		}

		googleProv := &CustomOAuthProvider{
			name:        "google",
			conf:        mgr.googleConf,
			infoURL:     "https://www.googleapis.com/oauth2/v3/userinfo",
			tokenSvc:    tokenSvc,
			issuer:      "tdlibgo",
			redirectURL: redirectURL,
			mapUser: func(data map[string]any) token.User {
				sub, _ := data["sub"].(string)
				name, _ := data["name"].(string)
				picture, _ := data["picture"].(string)
				email, _ := data["email"].(string)
				if name == "" {
					name = email
				}
				if name == "" {
					name = "Google User"
				}
				return token.User{
					ID:      "google_" + token.HashID(sha1.New(), sub),
					Name:    name,
					Picture: picture,
				}
			},
		}
		service.AddCustomHandler(googleProv)
	}

	// Setup GitHub Provider
	if cfg.GithubClientID != "" && cfg.GithubClientSecret != "" {
		redirectURL := cfg.GithubRedirectURL
		if redirectURL == "" {
			redirectURL = strings.TrimSuffix(cfg.URL, "/") + "/oauth/v1/redirect/res"
		}

		mgr.githubConf = &oauth2.Config{
			ClientID:     cfg.GithubClientID,
			ClientSecret: cfg.GithubClientSecret,
			Endpoint:     oauth2github.Endpoint,
			Scopes:       []string{"read:user", "user:email"},
			RedirectURL:  redirectURL,
		}

		githubProv := &CustomOAuthProvider{
			name:        "github",
			conf:        mgr.githubConf,
			infoURL:     "https://api.github.com/user",
			tokenSvc:    tokenSvc,
			issuer:      "tdlibgo",
			redirectURL: redirectURL,
			mapUser: func(data map[string]any) token.User {
				login, _ := data["login"].(string)
				name, _ := data["name"].(string)
				picture, _ := data["avatar_url"].(string)
				if name == "" {
					name = login
				}
				if name == "" {
					name = "GitHub User"
				}
				return token.User{
					ID:      "github_" + token.HashID(sha1.New(), login),
					Name:    name,
					Picture: picture,
				}
			},
		}
		service.AddCustomHandler(githubProv)
	}

	return mgr, nil
}

// Handlers returns http.Handlers for oauth routes and avatar routes.
func (m *OAuthManager) Handlers() (http.Handler, http.Handler) {
	return m.service.Handlers()
}

// HandleRedirectRes handles the custom OAuth callback endpoint /oauth/v1/redirect/res.
func (m *OAuthManager) HandleRedirectRes(w http.ResponseWriter, r *http.Request) {
	claims, _, err := m.tokenService.Get(r)
	if err != nil || claims.Handshake == nil {
		logger.Error("AUTH", "[OAuth] Missing or invalid handshake token on /oauth/v1/redirect/res: %v", err)
		http.Error(w, "Invalid or expired login session", http.StatusForbidden)
		return
	}

	state := r.URL.Query().Get("state")
	if claims.Handshake.State != state {
		logger.Error("AUTH", "[OAuth] State mismatch on /oauth/v1/redirect/res")
		http.Error(w, "State mismatch", http.StatusForbidden)
		return
	}

	// Try providers registered with matching redirectURL
	providers := m.service.Providers()
	for _, p := range providers {
		if cp, ok := p.Provider.(*CustomOAuthProvider); ok {
			cp.CompleteAuth(w, r)
			return
		}
		// Also support standard provider if applicable
		if p.Name() != "" {
			p.AuthHandler(w, r)
			return
		}
	}

	http.Error(w, "No matching OAuth provider found", http.StatusBadRequest)
}

// IssueAccessToken creates a short-lived (15 min) JWT Access Token for application memory storage.
func (m *OAuthManager) IssueAccessToken(u token.User, providerName string) (string, int64, error) {
	cidBytes := make([]byte, 16)
	_, _ = rand.Read(cidBytes)
	cid := hex.EncodeToString(cidBytes)

	now := time.Now()
	expiresAt := now.Add(m.cfg.TokenDuration)

	accessClaims := token.Claims{
		User: &u,
		RegisteredClaims: jwt.RegisteredClaims{
			Issuer:    "tdlibgo",
			ID:        cid,
			Subject:   u.ID,
			Audience:  jwt.ClaimStrings{"tdlibgo"},
			ExpiresAt: jwt.NewNumericDate(expiresAt),
			IssuedAt:  jwt.NewNumericDate(now),
		},
		AuthProvider: &token.AuthProvider{
			Name: providerName,
		},
		SessionOnly: false,
	}

	tokenString, err := m.tokenService.Token(accessClaims)
	if err != nil {
		return "", 0, err
	}
	return tokenString, int64(m.cfg.TokenDuration.Seconds()), nil
}

// GetSession reads and validates the current session (from cookie or authorization header).
func (m *OAuthManager) GetSession(r *http.Request) (*token.Claims, error) {
	claims, _, err := m.tokenService.Get(r)
	if err != nil {
		return nil, err
	}
	if claims.Handshake != nil {
		return nil, errors.New("handshake token cannot be used as session")
	}
	if claims.User == nil {
		return nil, errors.New("no user in session claims")
	}
	return &claims, nil
}

// RefreshSession checks the long-lived refresh token cookie, rotates or keeps it,
// and issues a fresh short-lived access token into response.
func (m *OAuthManager) RefreshSession(w http.ResponseWriter, r *http.Request) (string, *token.User, int64, error) {
	claims, err := m.GetSession(r)
	if err != nil {
		return "", nil, 0, err
	}

	// Ensure refresh cookie remains fresh
	claims.ExpiresAt = nil
	if _, err := m.tokenService.Set(w, *claims); err != nil {
		return "", nil, 0, fmt.Errorf("failed to refresh session cookie: %w", err)
	}

	provName := "oauth"
	if claims.AuthProvider != nil && claims.AuthProvider.Name != "" {
		provName = claims.AuthProvider.Name
	}

	accessToken, ttl, err := m.IssueAccessToken(*claims.User, provName)
	if err != nil {
		return "", nil, 0, fmt.Errorf("failed to issue access token: %w", err)
	}

	return accessToken, claims.User, ttl, nil
}

// Logout clears the refresh token cookie.
func (m *OAuthManager) Logout(w http.ResponseWriter) {
	m.tokenService.Reset(w)
}

// Enabled returns true if OAuth authentication is enabled.
func (m *OAuthManager) Enabled() bool {
	return m.cfg.Enabled
}

// AvailableProviders returns a list of configured and enabled OAuth provider names.
func (m *OAuthManager) AvailableProviders() []string {
	var list []string
	if m.googleConf != nil {
		list = append(list, "google")
	}
	if m.githubConf != nil {
		list = append(list, "github")
	}
	return list
}

// Middleware returns an HTTP middleware verifying that a valid OAuth session or Bearer token exists.
func (m *OAuthManager) Middleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !m.cfg.Enabled {
			next.ServeHTTP(w, r)
			return
		}

		// First, check Authorization: Bearer <access_token>
		authHeader := r.Header.Get("Authorization")
		if strings.HasPrefix(authHeader, "Bearer ") {
			tokenStr := strings.TrimPrefix(authHeader, "Bearer ")
			claims, err := m.tokenService.Parse(tokenStr)
			if err == nil && claims.User != nil && !m.tokenService.IsExpired(claims) {
				r = token.SetUserInfo(r, *claims.User)
				next.ServeHTTP(w, r)
				return
			}
		}

		// Second, check token query parameter (e.g. for WebSockets)
		if tkQuery := r.URL.Query().Get("token"); tkQuery != "" {
			claims, err := m.tokenService.Parse(tkQuery)
			if err == nil && claims.User != nil && !m.tokenService.IsExpired(claims) {
				r = token.SetUserInfo(r, *claims.User)
				next.ServeHTTP(w, r)
				return
			}
		}

		// Third, fallback to HttpOnly refresh cookie
		claims, _, err := m.tokenService.Get(r)
		if err == nil && claims.User != nil && claims.Handshake == nil {
			r = token.SetUserInfo(r, *claims.User)
			next.ServeHTTP(w, r)
			return
		}

		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusUnauthorized)
		_ = json.NewEncoder(w).Encode(map[string]any{
			"error":        "unauthorized",
			"message":      "OAuth authentication required",
			"auth_enabled": true,
		})
	})
}
