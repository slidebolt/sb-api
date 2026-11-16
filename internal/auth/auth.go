package auth

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"net"
	"net/http"
	"strings"
	"sync"
	"time"

	domain "github.com/slidebolt/sb-domain"
	storage "github.com/slidebolt/sb-storage-sdk"
)

const tokenKeyPrefix = "sb-api.tokens."

type Access string

const (
	AccessRead    Access = "read"
	AccessControl Access = "control"
	AccessAdmin   Access = "admin"
)

type Scope = Access

const (
	ScopeRead    = AccessRead
	ScopeControl = AccessControl
	ScopeWrite   = AccessAdmin
	ScopeAdmin   = AccessAdmin
)

type Token struct {
	ID             string    `json:"id"`
	Name           string    `json:"name"`
	Hash           string    `json:"hash"`
	Access         Access    `json:"access,omitempty"`
	SecurityGroups []string  `json:"securityGroups,omitempty"`
	CreatedAt      time.Time `json:"createdAt"`
	LastUsedAt     time.Time `json:"lastUsedAt,omitempty"`
	Scopes         []string  `json:"scopes,omitempty"`
}

func (t Token) Key() string                  { return tokenKeyPrefix + t.ID }
func (t Token) MarshalJSON() ([]byte, error) { return json.Marshal(tokenAlias(t)) }

type tokenAlias Token

func ValidAccess(access string) bool {
	switch Access(access) {
	case AccessRead, AccessControl, AccessAdmin:
		return true
	}
	return false
}
func ValidScope(scope string) bool { return ValidAccess(scope) || scope == "write" }

func accessRank(access Access) int {
	switch access {
	case AccessAdmin:
		return 3
	case AccessControl:
		return 2
	case AccessRead:
		return 1
	}
	return 0
}
func HasAccess(granted, required Access) bool       { return accessRank(granted) >= accessRank(required) }
func HasScope(scopes []string, required Scope) bool { return HasAccess(legacyAccess(scopes), required) }

func legacyAccess(scopes []string) Access {
	access := Access("")
	for _, scope := range scopes {
		switch scope {
		case "admin", "write":
			return AccessAdmin
		case "control":
			access = AccessControl
		case "read":
			if access == "" {
				access = AccessRead
			}
		}
	}
	return access
}

func normalizeToken(token Token) (Token, bool) {
	changed := false
	if !ValidAccess(string(token.Access)) {
		token.Access = legacyAccess(token.Scopes)
		if token.Access == "" {
			token.Access = AccessRead
		}
		changed = true
	}
	if len(token.SecurityGroups) == 0 {
		token.SecurityGroups = []string{"*"}
		changed = true
	}
	if token.Scopes != nil {
		token.Scopes = nil
		changed = true
	}
	return token, changed
}

func GenerateSecret() (string, error) {
	b := make([]byte, 32)
	_, err := rand.Read(b)
	return hex.EncodeToString(b), err
}
func GenerateID() (string, error) {
	b := make([]byte, 8)
	_, err := rand.Read(b)
	return hex.EncodeToString(b), err
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
	for _, entry := range entries {
		var token Token
		if json.Unmarshal(entry.Data, &token) != nil {
			continue
		}
		token, changed := normalizeToken(token)
		if changed {
			if err := store.Save(token); err != nil {
				return nil, err
			}
		}
		tokens = append(tokens, token)
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

func RequiredAccess(method, path string) Access {
	if strings.HasPrefix(path, "/tokens") || strings.HasPrefix(path, "/pairing/requests") || strings.HasPrefix(path, "/logs") || strings.HasPrefix(path, "/scripts") || strings.HasPrefix(path, "/schedules") || strings.HasPrefix(path, "/phrasebook") || path == "/query" {
		return AccessAdmin
	}
	if path == "/mcp" || strings.HasPrefix(path, "/mcp/") {
		return AccessRead
	}
	if method == http.MethodGet {
		return AccessRead
	}
	if method == http.MethodPost && strings.Contains(path, "/command/") {
		return AccessControl
	}
	return AccessAdmin
}
func RequiredScope(method, path string) Scope { return RequiredAccess(method, path) }

func writeError(w http.ResponseWriter, status int, detail string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(map[string]any{"status": status, "title": http.StatusText(status), "detail": detail})
}
func bearerSecret(r *http.Request) string {
	header := r.Header.Get("Authorization")
	if strings.HasPrefix(header, "Bearer ") {
		return strings.TrimPrefix(header, "Bearer ")
	}
	if strings.EqualFold(r.Header.Get("Upgrade"), "websocket") {
		return r.URL.Query().Get("access_token")
	}
	return ""
}

func Middleware(store storage.Storage) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			path := r.URL.Path
			if publicRequest(r.Method, path) {
				if strings.HasPrefix(path, "/pairing/requests") && !allowPairingRequest(r) {
					writeError(w, http.StatusTooManyRequests, "pairing rate limit exceeded")
					return
				}
				next.ServeHTTP(w, r)
				return
			}
			tokens, err := LoadTokens(store)
			if err != nil {
				writeError(w, http.StatusInternalServerError, "failed to load tokens")
				return
			}
			if len(tokens) == 0 {
				if r.Method == http.MethodPost && path == "/tokens" {
					next.ServeHTTP(w, r)
					return
				}
				writeError(w, http.StatusForbidden, "no access tokens exist — POST /tokens to bootstrap")
				return
			}
			secret := bearerSecret(r)
			if secret == "" {
				writeError(w, http.StatusUnauthorized, "missing bearer token")
				return
			}
			token := FindBySecret(tokens, secret)
			if token == nil {
				writeError(w, http.StatusUnauthorized, "invalid token")
				return
			}
			now := time.Now().UTC()
			if token.LastUsedAt.IsZero() || now.Sub(token.LastUsedAt) >= 5*time.Minute {
				token.LastUsedAt = now
				if err := store.Save(*token); err != nil {
					writeError(w, http.StatusInternalServerError, "failed to update token activity")
					return
				}
			}
			required := RequiredAccess(r.Method, path)
			if !HasAccess(token.Access, required) {
				writeError(w, http.StatusForbidden, "token requires access: "+string(required))
				return
			}
			principal := Principal{TokenID: token.ID, Access: token.Access, SecurityGroups: token.SecurityGroups}
			next.ServeHTTP(w, r.WithContext(WithPrincipal(r.Context(), principal)))
		})
	}
}

type rateWindow struct {
	Started time.Time
	Count   int
}

var pairingRate = struct {
	sync.Mutex
	Entries map[string]rateWindow
}{Entries: map[string]rateWindow{}}

func allowPairingRequest(r *http.Request) bool {
	key := strings.TrimSpace(strings.Split(r.Header.Get("X-Forwarded-For"), ",")[0])
	if key == "" {
		key, _, _ = net.SplitHostPort(r.RemoteAddr)
	}
	if key == "" {
		key = "unknown"
	}
	now := time.Now()
	pairingRate.Lock()
	defer pairingRate.Unlock()
	w := pairingRate.Entries[key]
	if w.Started.IsZero() || now.Sub(w.Started) >= time.Minute {
		w = rateWindow{Started: now}
	}
	w.Count++
	pairingRate.Entries[key] = w
	return w.Count <= 30
}

func publicRequest(method, path string) bool {
	if strings.HasPrefix(path, "/openapi") || strings.HasPrefix(path, "/docs") || strings.HasPrefix(path, "/schemas") || path == "/.well-known/slidebolt" {
		return true
	}
	if method == http.MethodPost && (path == "/pairing/requests" || strings.HasSuffix(path, "/exchange") && strings.HasPrefix(path, "/pairing/requests/")) {
		return true
	}
	return false
}

type Principal struct {
	TokenID        string
	Access         Access
	SecurityGroups []string
}
type principalKey struct{}

func WithPrincipal(ctx context.Context, principal Principal) context.Context {
	return context.WithValue(ctx, principalKey{}, principal)
}
func PrincipalFromContext(ctx context.Context) (Principal, bool) {
	principal, ok := ctx.Value(principalKey{}).(Principal)
	return principal, ok
}
func WithScopes(ctx context.Context, scopes []string) context.Context {
	return WithPrincipal(ctx, Principal{Access: legacyAccess(scopes), SecurityGroups: []string{"*"}})
}
func ScopesFromContext(ctx context.Context) ([]string, bool) {
	principal, ok := PrincipalFromContext(ctx)
	if !ok {
		return nil, false
	}
	return []string{string(principal.Access)}, true
}

func AllowsGroup(ctx context.Context, group string) bool {
	principal, ok := PrincipalFromContext(ctx)
	if !ok {
		return true
	}
	if group == "" {
		group = domain.SecurityGroupSystem
	}
	for _, allowed := range principal.SecurityGroups {
		if allowed == "*" || allowed == group {
			return true
		}
	}
	return false
}

func AllowsAllGroups(ctx context.Context) bool {
	principal, ok := PrincipalFromContext(ctx)
	if !ok {
		return true
	}
	for _, group := range principal.SecurityGroups {
		if group == "*" {
			return true
		}
	}
	return false
}

func ResourceSecurityGroup(data []byte) (string, bool) {
	var envelope struct {
		Type          string `json:"type"`
		SecurityGroup string `json:"securityGroup"`
	}
	if json.Unmarshal(data, &envelope) != nil || envelope.Type == "" {
		return "", false
	}
	if envelope.SecurityGroup != "" {
		return envelope.SecurityGroup, true
	}
	return domain.DefaultSecurityGroup(envelope.Type), true
}
