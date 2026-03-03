package twitter

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"
)

// TokenProvider returns a valid OAuth2 bearer token.
type TokenProvider interface {
	GetToken(ctx context.Context) (string, error)
}

type tokenResponse struct {
	AccessToken  string `json:"access_token"`
	RefreshToken string `json:"refresh_token"`
	ExpiresIn    int    `json:"expires_in"`
	TokenType    string `json:"token_type"`
}

// OAuth2TokenProvider manages OAuth2 bearer tokens with automatic refresh.
// onRotate is called whenever Twitter returns a new refresh token — use it
// to persist the rotated token (e.g. write back to Keychain).
// This targets public clients (PKCE flow) — no client secret required.
type OAuth2TokenProvider struct {
	clientID string
	onRotate func(newRefreshToken string) // called after each rotation; may be nil

	mu           sync.Mutex
	refreshToken string
	accessToken  string
	expiresAt    time.Time
}

// NewOAuth2TokenProvider creates a new provider with the given credentials.
// onRotate is called with the new refresh token whenever Twitter rotates it.
func NewOAuth2TokenProvider(clientID, refreshToken string, onRotate func(string)) *OAuth2TokenProvider {
	return &OAuth2TokenProvider{
		clientID:     clientID,
		refreshToken: refreshToken,
		onRotate:     onRotate,
	}
}

// GetToken returns a valid access token, refreshing if needed.
// It uses a 5-minute buffer before expiry to avoid last-second failures.
func (p *OAuth2TokenProvider) GetToken(ctx context.Context) (string, error) {
	p.mu.Lock()
	defer p.mu.Unlock()

	if p.accessToken != "" && time.Now().Add(5*time.Minute).Before(p.expiresAt) {
		return p.accessToken, nil
	}
	return p.refresh(ctx)
}


func (p *OAuth2TokenProvider) refresh(ctx context.Context) (string, error) {
	form := url.Values{}
	form.Set("grant_type", "refresh_token")
	form.Set("refresh_token", p.refreshToken)
	form.Set("client_id", p.clientID)

	req, err := http.NewRequestWithContext(ctx, http.MethodPost,
		"https://api.x.com/2/oauth2/token",
		strings.NewReader(form.Encode()))
	if err != nil {
		return "", fmt.Errorf("build token request: %w", err)
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return "", fmt.Errorf("token request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("token endpoint returned %d", resp.StatusCode)
	}

	var tr tokenResponse
	if err := json.NewDecoder(resp.Body).Decode(&tr); err != nil {
		return "", fmt.Errorf("decode token response: %w", err)
	}

	// Default to 2 hours if Twitter omits or returns 0 for expires_in,
	// preventing every goroutine from seeing the token as immediately expired
	// and triggering a refresh cascade that invalidates earlier access tokens.
	expiresIn := tr.ExpiresIn
	if expiresIn <= 0 {
		expiresIn = 7200
	}
	p.accessToken = tr.AccessToken
	p.expiresAt = time.Now().Add(time.Duration(expiresIn) * time.Second)

	// Log so we can confirm what Twitter actually returns.
	fmt.Printf("{\"level\":\"debug\",\"expires_in_from_twitter\":%d,\"effective_expires_in\":%d,\"message\":\"token refreshed\"}\n", tr.ExpiresIn, expiresIn)

	// Rotate refresh token if Twitter returned a new one, then persist it.
	if tr.RefreshToken != "" {
		p.refreshToken = tr.RefreshToken
		if p.onRotate != nil {
			go p.onRotate(tr.RefreshToken)
		}
	}

	return p.accessToken, nil
}
