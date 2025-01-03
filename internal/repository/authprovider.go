package repository

import (
	"crypto/rsa"
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"sync"
	"time"

	"github.com/go-git/go-git/v5/plumbing/transport"
	githttp "github.com/go-git/go-git/v5/plumbing/transport/http"
	"github.com/go-git/go-git/v5/plumbing/transport/ssh"
	"github.com/golang-jwt/jwt/v5"
)

type AuthProvider interface {
	AuthMethod() (transport.AuthMethod, error)
}

var _ AuthProvider = &BasicAuthProvider{}

type BasicAuthProvider struct {
	Username string
	Password string
}

func (b *BasicAuthProvider) AuthMethod() (transport.AuthMethod, error) {
	return &githttp.BasicAuth{
		Username: b.Username,
		Password: b.Password,
	}, nil
}

var _ AuthProvider = &SSHAuthProvider{}

type SSHAuthProvider struct {
	Key []byte
}

func (s *SSHAuthProvider) AuthMethod() (transport.AuthMethod, error) {
	publicKeys, err := ssh.NewPublicKeys("git", s.Key, "")
	if err != nil {
		return nil, err
	}
	return publicKeys, nil
}

var _ AuthProvider = &TokenAuthProvider{}

type TokenAuthProvider struct {
	Token string
}

func (t *TokenAuthProvider) AuthMethod() (transport.AuthMethod, error) {
	return &githttp.BasicAuth{
		Username: "git",
		Password: t.Token,
	}, nil
}

var _ AuthProvider = &GithubAppAuthProvider{}

type GithubAppAuthProvider struct {
	AppID          int64
	InstallationID int64
	PrivateKey     []byte

	privateKey *rsa.PrivateKey
	token      string
	exp        time.Time
	mu         sync.RWMutex
}

func (g *GithubAppAuthProvider) AuthMethod() (transport.AuthMethod, error) {
	if g.privateKey == nil {
		g.mu.Lock()
		pk, err := jwt.ParseRSAPrivateKeyFromPEM(g.PrivateKey)
		if err != nil {
			g.mu.Unlock()
			return nil, fmt.Errorf("failed to parse private key: %w", err)
		}
		g.privateKey = pk
		g.mu.Unlock()
	}
	// Acquire read lock for checking token expiration
	g.mu.RLock()
	if time.Now().Before(g.exp) {
		// Token is still valid, return it
		token := g.token
		g.mu.RUnlock()
		return &githttp.BasicAuth{
			Username: "x-access-token",
			Password: token,
		}, nil
	}
	g.mu.RUnlock()

	// Acquire write lock to refresh the token
	g.mu.Lock()
	defer g.mu.Unlock()

	// Double-check the expiration after acquiring write lock
	if time.Now().Before(g.exp) {
		// Another goroutine already refreshed the token
		return &githttp.BasicAuth{
			Username: "x-access-token",
			Password: g.token,
		}, nil
	}

	appID := strconv.FormatInt(g.AppID, 10)
	instID := strconv.FormatInt(g.InstallationID, 10)

	token, err := getInstallationToken(appID, instID, g.privateKey)
	if err != nil {
		return nil, fmt.Errorf("failed to get installation token: %w", err)
	}

	// Update the token and expiration time
	g.token = token
	// Token expires in 1 hour, refresh it after 55 minutes
	g.exp = time.Now().Add(55 * time.Minute)

	return &githttp.BasicAuth{
		Username: "x-access-token",
		Password: token,
	}, nil
}

// Generate a JWT for GitHub App authentication
func generateJWT(appID string, privateKey *rsa.PrivateKey) (string, error) {
	now := time.Now().Add(-1 * time.Minute) // protect against clock skew
	claims := jwt.RegisteredClaims{
		IssuedAt:  jwt.NewNumericDate(now),
		ExpiresAt: jwt.NewNumericDate(now.Add(10 * time.Minute)), // JWT is valid for 10 minutes
		Issuer:    appID,
	}

	token := jwt.NewWithClaims(jwt.SigningMethodRS256, claims)
	return token.SignedString(privateKey)
}

// Get the installation access token
func getInstallationToken(appID, installationID string, privateKey *rsa.PrivateKey) (string, error) {
	jwtToken, err := generateJWT(appID, privateKey)
	if err != nil {
		return "", fmt.Errorf("failed to generate JWT: %w", err)
	}

	apiURL := fmt.Sprintf("https://api.github.com/app/installations/%s/access_tokens", installationID)
	req, err := http.NewRequest("POST", apiURL, nil)
	if err != nil {
		return "", fmt.Errorf("failed to create request: %w", err)
	}

	req.Header.Set("Authorization", "Bearer "+jwtToken)
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("X-GitHub-Api-Version", "2022-11-28")
	req.Header.Add("User-Agent", "greatliontech/pbr")

	client := &http.Client{}
	resp, err := client.Do(req)
	if err != nil {
		return "", fmt.Errorf("failed to fetch access token: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusCreated {
		return "", fmt.Errorf("unexpected response status: %s", resp.Status)
	}

	type tokenResponse struct {
		Token string `json:"token"`
	}

	var result tokenResponse
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return "", fmt.Errorf("failed to decode response: %w", err)
	}

	return result.Token, nil
}
