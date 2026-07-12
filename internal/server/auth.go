package server

import (
	"net/http"
	"strings"
)

func (s *Server) chainFXAuthContext(r *http.Request) chainFXAuth {
	return s.chainFXAuthForKey(chainFXAPIKey(r))
}

func (s *Server) chainFXAuthForKey(key string) chainFXAuth {
	if key == "" {
		return chainFXAuth{}
	}
	if strings.HasPrefix(key, "sk_test_") || csvContains(s.cfg.ChainFXTestSecretKeys, key) {
		return chainFXAuth{Valid: true, Sandbox: true, Mode: "test"}
	}
	if csvContains(s.cfg.ChainFXLiveSecretKeys, key) {
		return chainFXAuth{Valid: true, Mode: "live"}
	}
	if strings.HasPrefix(key, "sk_live_") && !s.cfg.ChainFXRequireAPIKey && !s.cfg.IsProduction() {
		return chainFXAuth{Valid: true, Mode: "live-dev"}
	}
	return chainFXAuth{}
}

func chainFXAPIKey(r *http.Request) string {
	auth := strings.TrimSpace(r.Header.Get("Authorization"))
	if strings.HasPrefix(strings.ToLower(auth), "bearer ") {
		return strings.TrimSpace(auth[7:])
	}
	if key := strings.TrimSpace(r.Header.Get("X-Api-Key")); key != "" {
		return key
	}
	return strings.TrimSpace(r.URL.Query().Get("apiKey"))
}

func chainFXAPIKeyFromHeader(r *http.Request) string {
	auth := strings.TrimSpace(r.Header.Get("Authorization"))
	if strings.HasPrefix(strings.ToLower(auth), "bearer ") {
		return strings.TrimSpace(auth[7:])
	}
	return strings.TrimSpace(r.Header.Get("X-Api-Key"))
}
