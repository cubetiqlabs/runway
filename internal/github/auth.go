package github

import (
	"bytes"
	"context"
	"crypto/rsa"
	"crypto/x509"
	"encoding/json"
	"encoding/pem"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"golang.org/x/sync/singleflight"
)

type cachedToken struct {
	token     string
	expiresAt time.Time
}

// AppAuth manages GitHub App RS256 JWT minting and TTL-cached installation tokens.
type AppAuth struct {
	appID      int64
	privateKey *rsa.PrivateKey
	apiBaseURL string
	httpClient *http.Client

	mu     sync.RWMutex
	tokens map[int64]cachedToken
	sf     singleflight.Group
}

// NewAppAuth initializes GitHub App authentication if AppID and PrivateKey are configured.
func NewAppAuth(appID int64, pemInline, keyPath, apiBaseURL string, httpClient *http.Client) (*AppAuth, error) {
	if appID <= 0 {
		return nil, nil
	}
	rawPEM := strings.TrimSpace(pemInline)
	if rawPEM == "" && strings.TrimSpace(keyPath) != "" {
		data, err := os.ReadFile(strings.TrimSpace(keyPath))
		if err != nil {
			return nil, fmt.Errorf("read github private key %q: %w", keyPath, err)
		}
		rawPEM = string(data)
	}
	if rawPEM == "" {
		return nil, nil
	}
	// Support \n-escaped env vars
	rawPEM = strings.ReplaceAll(rawPEM, `\n`, "\n")

	key, err := parseRSAPrivateKey([]byte(rawPEM))
	if err != nil {
		return nil, fmt.Errorf("parse github private key: %w", err)
	}
	if httpClient == nil {
		httpClient = &http.Client{Timeout: 15 * time.Second}
	}
	base := strings.TrimRight(strings.TrimSpace(apiBaseURL), "/")
	if base == "" {
		base = "https://api.github.com"
	}

	return &AppAuth{
		appID:      appID,
		privateKey: key,
		apiBaseURL: base,
		httpClient: httpClient,
		tokens:     make(map[int64]cachedToken),
	}, nil
}

func parseRSAPrivateKey(pemBytes []byte) (*rsa.PrivateKey, error) {
	block, _ := pem.Decode(pemBytes)
	if block == nil {
		return nil, errors.New("no PEM block found")
	}
	if key, err := x509.ParsePKCS1PrivateKey(block.Bytes); err == nil {
		return key, nil
	}
	parsed, err := x509.ParsePKCS8PrivateKey(block.Bytes)
	if err != nil {
		return nil, fmt.Errorf("failed PKCS1 and PKCS8 parse: %w", err)
	}
	rsaKey, ok := parsed.(*rsa.PrivateKey)
	if !ok {
		return nil, errors.New("private key is not RSA")
	}
	return rsaKey, nil
}

// MintAppJWT creates a short-lived RS256 JWT for GitHub App endpoints.
func (a *AppAuth) MintAppJWT(now time.Time) (string, error) {
	claims := jwt.RegisteredClaims{
		IssuedAt:  jwt.NewNumericDate(now.Add(-60 * time.Second)),
		ExpiresAt: jwt.NewNumericDate(now.Add(9 * time.Minute)),
		Issuer:    fmt.Sprintf("%d", a.appID),
	}
	tok := jwt.NewWithClaims(jwt.SigningMethodRS256, claims)
	return tok.SignedString(a.privateKey)
}

// InstallationToken returns a cached installation access token or exchanges a new one via singleflight.
func (a *AppAuth) InstallationToken(ctx context.Context, installationID int64) (string, error) {
	a.mu.RLock()
	ct, ok := a.tokens[installationID]
	a.mu.RUnlock()
	if ok && time.Until(ct.expiresAt) > 5*time.Minute {
		return ct.token, nil
	}

	key := fmt.Sprintf("inst:%d", installationID)
	val, err, _ := a.sf.Do(key, func() (any, error) {
		a.mu.RLock()
		ct2, ok2 := a.tokens[installationID]
		a.mu.RUnlock()
		if ok2 && time.Until(ct2.expiresAt) > 5*time.Minute {
			return ct2.token, nil
		}

		appJWT, err := a.MintAppJWT(time.Now().UTC())
		if err != nil {
			return "", fmt.Errorf("mint app jwt: %w", err)
		}

		url := fmt.Sprintf("%s/app/installations/%d/access_tokens", a.apiBaseURL, installationID)
		req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader([]byte(`{}`)))
		if err != nil {
			return "", err
		}
		req.Header.Set("Authorization", "Bearer "+appJWT)
		req.Header.Set("Accept", "application/vnd.github+json")
		req.Header.Set("X-GitHub-Api-Version", "2022-11-28")
		req.Header.Set("User-Agent", "runway/1.0")

		resp, err := a.httpClient.Do(req)
		if err != nil {
			return "", fmt.Errorf("request installation token: %w", err)
		}
		defer resp.Body.Close()

		body, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
		if err != nil {
			return "", err
		}
		if resp.StatusCode < 200 || resp.StatusCode >= 300 {
			return "", fmt.Errorf("github installation token error (%d): %s", resp.StatusCode, strings.TrimSpace(string(body)))
		}

		var parsed struct {
			Token     string    `json:"token"`
			ExpiresAt time.Time `json:"expires_at"`
		}
		if err := json.Unmarshal(body, &parsed); err != nil {
			return "", fmt.Errorf("decode installation token response: %w", err)
		}

		a.mu.Lock()
		a.tokens[installationID] = cachedToken{token: parsed.Token, expiresAt: parsed.ExpiresAt}
		a.mu.Unlock()
		return parsed.Token, nil
	})
	if err != nil {
		return "", err
	}
	return val.(string), nil
}
