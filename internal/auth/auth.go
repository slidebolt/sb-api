package auth

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"strings"
	"time"

	storage "github.com/slidebolt/sb-storage-sdk"
)

const tokenKeyPrefix = "sb-api.tokens."

type Token struct {
	ID        string    `json:"id"`
	Name      string    `json:"name"`
	Hash      string    `json:"hash"`
	Scopes    []string  `json:"scopes"`
	CreatedAt time.Time `json:"createdAt"`
}

func (t Token) Key() string                  { return tokenKeyPrefix + t.ID }
func (t Token) MarshalJSON() ([]byte, error) { return json.Marshal(tokenAlias(t)) }

type tokenAlias Token

type Scope string

const (
	ScopeRead    Scope = "read"
	ScopeControl Scope = "control"
	ScopeWrite   Scope = "write"
	ScopeAdmin   Scope = "admin"
)

var validScopes = map[Scope]bool{
	ScopeRead:    true,
	ScopeControl: true,
	ScopeWrite:   true,
	ScopeAdmin:   true,
}

func ValidScope(s string) bool {
	return validScopes[Scope(s)]
}

func GenerateSecret() (string, error) {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return hex.EncodeToString(b), nil
}

func GenerateID() (string, error) {
	b := make([]byte, 8)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return hex.EncodeToString(b), nil
}

func HashSecret(secret string) string {
	h := sha256.Sum256([]byte(secret))
	return hex.EncodeToString(h[:])
}

func LoadTokens(store storage.Storage) ([]Token, error) {
	entries, err := store.Search(tokenKeyPrefix + ">")
	if err != nil {
		return nil, err
	}
	tokens := make([]Token, 0, len(entries))
	for _, e := range entries {
		var t Token
		if err := json.Unmarshal(e.Data, &t); err != nil {
			continue
		}
		tokens = append(tokens, t)
	}
	return tokens, nil
}

func FindBySecret(tokens []Token, secret string) *Token {
	hash := HashSecret(secret)
	for i := range tokens {
		if tokens[i].Hash == hash {
			return &tokens[i]
		}
	}
	return nil
}

func HasScope(scopes []string, required Scope) bool {
	for _, s := range scopes {
		if Scope(s) == required {
			return true
		}
	}
	return false
}

func RequiredScope(method, path string) Scope {
	if strings.HasPrefix(path, "/tokens") {
		return ScopeAdmin
	}
	if method == http.MethodGet {
		return ScopeRead
	}
	if method == http.MethodPost && path == "/query" {
		return ScopeRead
	}
	if method == http.MethodPost && strings.Contains(path, "/command/") {
		return ScopeControl
	}
	return ScopeWrite
}

func writeError(w http.ResponseWriter, status int, detail string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(map[string]any{
		"status": status,
		"title":  http.StatusText(status),
		"detail": detail,
	})
}

func bearerSecret(r *http.Request) string {
	header := r.Header.Get("Authorization")
	if strings.HasPrefix(header, "Bearer ") {
		return strings.TrimPrefix(header, "Bearer ")
	}
	if isWebSocketRequest(r) {
		return r.URL.Query().Get("access_token")
	}
	return ""
}

func isWebSocketRequest(r *http.Request) bool {
	return strings.EqualFold(r.Header.Get("Upgrade"), "websocket")
}

func Middleware(store storage.Storage) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			// Skip auth for OpenAPI docs and schema endpoints.
			p := r.URL.Path
			if strings.HasPrefix(p, "/openapi") || strings.HasPrefix(p, "/docs") || strings.HasPrefix(p, "/schemas") {
				next.ServeHTTP(w, r)
				return
			}

			tokens, err := LoadTokens(store)
			if err != nil {
				writeError(w, http.StatusInternalServerError, "failed to load tokens")
				return
			}

			// Bootstrap mode: no tokens exist yet.
			if len(tokens) == 0 {
				if r.Method == http.MethodPost && p == "/tokens" {
					next.ServeHTTP(w, r)
					return
				}
				writeError(w, http.StatusForbidden, "no access tokens exist — POST /tokens to bootstrap")
				return
			}

			// Require bearer token.
			secret := bearerSecret(r)
			if secret == "" {
				writeError(w, http.StatusUnauthorized, "missing bearer token")
				return
			}

			tok := FindBySecret(tokens, secret)
			if tok == nil {
				writeError(w, http.StatusUnauthorized, "invalid token")
				return
			}

			required := RequiredScope(r.Method, p)
			if !HasScope(tok.Scopes, required) {
				writeError(w, http.StatusForbidden, "token missing required scope: "+string(required))
				return
			}

			next.ServeHTTP(w, r)
		})
	}
}
