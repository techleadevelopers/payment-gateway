package server

import (
	"crypto/ed25519"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"sort"
	"strings"
	"time"
)

const (
	agentIdentityAlg        = "EdDSA"
	agentIdentityKeyUse     = "sig"
	agentIdentityMaxEpisode = 1000
)

type agentEpisode struct {
	EpisodeID        string         `json:"episode_id"`
	AgentID          string         `json:"agent_id"`
	Protocol         string         `json:"protocol"`
	Skill            string         `json:"skill"`
	InputHash        string         `json:"input_hash"`
	PaymentIntentID  string         `json:"payment_intent_id,omitempty"`
	SettlementStatus string         `json:"settlement_status,omitempty"`
	LatencyMS        int64          `json:"latency_ms"`
	Cost             map[string]any `json:"cost,omitempty"`
	ResultHash       string         `json:"result_hash,omitempty"`
	ErrorTree        map[string]any `json:"error_tree,omitempty"`
	Status           string         `json:"status"`
	StatusCode       int            `json:"status_code"`
	CreatedAt        time.Time      `json:"created_at"`
}

func (s *Server) handleAgentJWKS(w http.ResponseWriter, r *http.Request) {
	base := publicBaseURL(r)
	s.writeCachedDiscoveryJSON(w, r, "agent-jwks:"+base, time.Minute, func() (any, error) {
		kid, pub, _ := s.agentSigningMaterial(base)
		return map[string]any{
			"keys": []map[string]any{
				{
					"kty": "OKP",
					"crv": "Ed25519",
					"use": agentIdentityKeyUse,
					"alg": agentIdentityAlg,
					"kid": kid,
					"x":   base64.RawURLEncoding.EncodeToString(pub),
				},
			},
		}, nil
	})
}

func (s *Server) handleAgentCardSignature(w http.ResponseWriter, r *http.Request) {
	base := publicBaseURL(r)
	s.writeCachedDiscoveryJSON(w, r, "agent-card-signature:"+base, time.Minute, func() (any, error) {
		kid, _, priv := s.agentSigningMaterial(base)
		card := s.a2aAgentCard(base)
		hashHex, canonical, err := canonicalJSONHash(card)
		if err != nil {
			return nil, err
		}
		signature := ed25519.Sign(priv, canonical)
		now := time.Now().UTC()
		return map[string]any{
			"agent":              "ChainFX Agent Pay",
			"agent_id":           "did:web:" + strings.TrimPrefix(strings.TrimPrefix(base, "https://"), "http://") + ":chainfx-agent-pay",
			"verified_by":        "ChainFX",
			"public_key_id":      kid,
			"algorithm":          agentIdentityAlg,
			"card_url":           base + "/.well-known/agent-card.json",
			"jwks_url":           base + "/.well-known/jwks.json",
			"card_hash":          hashHex,
			"signature_encoding": "base64url",
			"signature":          base64.RawURLEncoding.EncodeToString(signature),
			"signed_at":          now.Format(time.RFC3339),
			"expires_at":         now.Add(24 * time.Hour).Format(time.RFC3339),
			"verification": map[string]any{
				"message": "Verify Ed25519 signature over the canonical JSON bytes of the Agent Card response.",
			},
		}, nil
	})
}

func (s *Server) handleAgentReputationWellKnown(w http.ResponseWriter, r *http.Request) {
	s.handleAgentReputation(w, r)
}

func (s *Server) handleAgentSLAWellKnown(w http.ResponseWriter, r *http.Request) {
	s.handleAgentSLA(w, r)
}

func (s *Server) handleAgentReputation(w http.ResponseWriter, r *http.Request) {
	base := publicBaseURL(r)
	s.writeCachedDiscoveryJSON(w, r, "agent-reputation:"+base, 30*time.Second, func() (any, error) {
		return s.agentReputationDocument(base), nil
	})
}

func (s *Server) handleAgentSLA(w http.ResponseWriter, r *http.Request) {
	base := publicBaseURL(r)
	s.writeCachedDiscoveryJSON(w, r, "agent-sla:"+base, time.Minute, func() (any, error) {
		return s.agentSLADocument(base), nil
	})
}

func (s *Server) handleAgentEpisodes(w http.ResponseWriter, r *http.Request) {
	if _, ok := s.authorizeChainFX(w, r); !ok {
		return
	}
	limit := 50
	if raw := strings.TrimSpace(r.URL.Query().Get("limit")); raw != "" {
		var parsed int
		if _, err := fmt.Sscanf(raw, "%d", &parsed); err == nil && parsed > 0 && parsed <= 200 {
			limit = parsed
		}
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"episodes": s.recentAgentEpisodes(limit),
		"limit":    limit,
	})
}

func (s *Server) handleAgentEpisode(w http.ResponseWriter, r *http.Request) {
	if _, ok := s.authorizeChainFX(w, r); !ok {
		return
	}
	id := strings.TrimSpace(r.PathValue("id"))
	for _, episode := range s.recentAgentEpisodes(agentIdentityMaxEpisode) {
		if episode.EpisodeID == id {
			writeJSON(w, http.StatusOK, episode)
			return
		}
	}
	writeJSON(w, http.StatusNotFound, map[string]any{"error": "episode not found"})
}

func (s *Server) agentIdentityMetadata(base string) map[string]any {
	kid, pub, _ := s.agentSigningMaterial(base)
	return map[string]any{
		"agent_id":      "did:web:" + strings.TrimPrefix(strings.TrimPrefix(base, "https://"), "http://") + ":chainfx-agent-pay",
		"verified_by":   "ChainFX",
		"public_key_id": kid,
		"algorithm":     agentIdentityAlg,
		"public_key": map[string]any{
			"kty": "OKP",
			"crv": "Ed25519",
			"kid": kid,
			"x":   base64.RawURLEncoding.EncodeToString(pub),
		},
		"jwks_url":      base + "/.well-known/jwks.json",
		"signature_url": base + "/.well-known/agent-card.signature",
		"reputation":    base + "/.well-known/agent-reputation.json",
		"sla":           base + "/.well-known/agent-sla.json",
		"issued_at":     "2026-07-17T00:00:00Z",
		"expires_at":    "2027-07-17T00:00:00Z",
	}
}

func (s *Server) agentSigningMaterial(base string) (string, ed25519.PublicKey, ed25519.PrivateKey) {
	seedSource := strings.TrimSpace(os.Getenv("CHAINFX_AGENT_IDENTITY_SEED"))
	if seedSource == "" && s != nil && s.cfg != nil {
		seedSource = firstNonEmpty(
			s.cfg.WebhookSecret,
			s.cfg.LGPDSecret,
			s.cfg.SignerHmacSecret,
			s.cfg.ChainFXLiveSecretKeys,
			s.cfg.ChainFXTestSecretKeys,
		)
	}
	if seedSource == "" {
		seedSource = "chainfx-local-agent-identity-development-seed"
	}
	seedHash := sha256.Sum256([]byte(seedSource + "|" + base + "|chainfx-agent-pay"))
	priv := ed25519.NewKeyFromSeed(seedHash[:])
	pub := priv.Public().(ed25519.PublicKey)
	kidHash := sha256.Sum256(pub)
	return "chainfx-agent-pay-ed25519-" + hex.EncodeToString(kidHash[:6]), pub, priv
}

func (s *Server) agentReputationDocument(base string) map[string]any {
	episodes := s.recentAgentEpisodes(agentIdentityMaxEpisode)
	total := len(episodes)
	success := 0
	failures := 0
	latencies := make([]int64, 0, total)
	bySkill := map[string]map[string]int{}
	for _, episode := range episodes {
		if episode.Status == "completed" && episode.StatusCode >= 200 && episode.StatusCode < 400 {
			success++
		} else {
			failures++
		}
		if episode.LatencyMS > 0 {
			latencies = append(latencies, episode.LatencyMS)
		}
		if _, ok := bySkill[episode.Skill]; !ok {
			bySkill[episode.Skill] = map[string]int{"total": 0, "success": 0, "failed": 0}
		}
		bySkill[episode.Skill]["total"]++
		if episode.Status == "completed" && episode.StatusCode >= 200 && episode.StatusCode < 400 {
			bySkill[episode.Skill]["success"]++
		} else {
			bySkill[episode.Skill]["failed"]++
		}
	}
	successRate := 1.0
	if total > 0 {
		successRate = float64(success) / float64(total)
	}
	score := "A"
	if successRate >= 0.999 {
		score = "AAA"
	} else if successRate >= 0.99 {
		score = "AA"
	} else if successRate < 0.95 {
		score = "B"
	}
	return map[string]any{
		"agent":                   "ChainFX Agent Pay",
		"agent_card":              base + "/.well-known/agent-card.json",
		"reputation_score":        score,
		"sample_window":           "in_memory_recent_episodes",
		"total_episodes":          total,
		"successful_episodes":     success,
		"failed_episodes":         failures,
		"success_rate":            fmt.Sprintf("%.4f", successRate),
		"latency_ms":              percentileSummary(latencies),
		"success_by_skill":        bySkill,
		"settlement_success_rate": "pending_live_settlement_sample",
		"fraud_reports":           0,
		"updated_at":              time.Now().UTC().Format(time.RFC3339),
	}
}

func (s *Server) agentSLADocument(base string) map[string]any {
	return map[string]any{
		"agent":       "ChainFX Agent Pay",
		"agent_card":  base + "/.well-known/agent-card.json",
		"sla_version": "2026-07-17",
		"availability": map[string]any{
			"target":        "99.9%",
			"measurement":   "rolling_30d",
			"health_check":  base + "/readyz",
			"public_status": base + "/.well-known/agent-reputation.json",
		},
		"latency_objectives_ms": map[string]any{
			"discovery_p95":      3000,
			"a2a_sync_p95":       15000,
			"quote_p95":          5000,
			"status_p95":         3000,
			"settlement_started": "after exact USDT deposit confirmation",
		},
		"payment_terms": map[string]any{
			"funding_network": "BSC",
			"funding_assets":  []string{"USDT", "USDC"},
			"payment_methods": []string{"pix", "credit_card"},
			"fees_bps":        map[string]int{"pix": s.cfg.M2MPixFeeBps, "credit_card": s.cfg.M2MCreditFeeBps},
			"intent_ttl":      "15m",
			"risk_rule":       "deposit exactly required_usdt to the returned BSC payment_address before expires_at",
		},
		"rate_limit_headers": []string{"X-RateLimit-Limit", "X-RateLimit-Remaining", "X-RateLimit-Reset"},
		"support": map[string]any{
			"email": s.cfg.SupportEmail,
		},
		"updated_at": time.Now().UTC().Format(time.RFC3339),
	}
}

func (s *Server) recordAgentEpisode(episode agentEpisode) {
	if s == nil {
		return
	}
	if episode.EpisodeID == "" {
		episode.EpisodeID = newAgentEpisodeID(episode.Skill, episode.InputHash, episode.CreatedAt)
	}
	s.agentEpisodesMu.Lock()
	defer s.agentEpisodesMu.Unlock()
	s.agentEpisodes = append(s.agentEpisodes, episode)
	if len(s.agentEpisodes) > agentIdentityMaxEpisode {
		s.agentEpisodes = append([]agentEpisode(nil), s.agentEpisodes[len(s.agentEpisodes)-agentIdentityMaxEpisode:]...)
	}
}

func (s *Server) recentAgentEpisodes(limit int) []agentEpisode {
	if s == nil || limit <= 0 {
		return nil
	}
	s.agentEpisodesMu.Lock()
	defer s.agentEpisodesMu.Unlock()
	start := len(s.agentEpisodes) - limit
	if start < 0 {
		start = 0
	}
	out := append([]agentEpisode(nil), s.agentEpisodes[start:]...)
	for i, j := 0, len(out)-1; i < j; i, j = i+1, j-1 {
		out[i], out[j] = out[j], out[i]
	}
	return out
}

func canonicalJSONHash(value any) (string, []byte, error) {
	raw, err := json.Marshal(value)
	if err != nil {
		return "", nil, err
	}
	var normalized any
	if err := json.Unmarshal(raw, &normalized); err != nil {
		return "", nil, err
	}
	canonical, err := json.Marshal(normalized)
	if err != nil {
		return "", nil, err
	}
	sum := sha256.Sum256(canonical)
	return hex.EncodeToString(sum[:]), canonical, nil
}

func hashAny(value any) string {
	if value == nil {
		return ""
	}
	hash, _, err := canonicalJSONHash(value)
	if err != nil {
		return ""
	}
	return hash
}

func paymentIntentIDFromAny(value any) string {
	return firstNestedString(value,
		"payment.intent_id",
		"payment.id",
		"result.payment.intent_id",
		"result.payment.id",
		"intent_id",
		"id",
	)
}

func settlementStatusFromAny(value any) string {
	return firstNestedString(value,
		"payment.settlement_status",
		"payment.status",
		"result.payment.settlement_status",
		"result.payment.status",
		"settlement_status",
		"status",
	)
}

func firstNestedString(value any, paths ...string) string {
	for _, path := range paths {
		current := value
		ok := true
		for _, part := range strings.Split(path, ".") {
			asMap, isMap := current.(map[string]any)
			if !isMap {
				ok = false
				break
			}
			current, ok = asMap[part]
			if !ok {
				break
			}
		}
		if ok {
			if text := strings.TrimSpace(fmt.Sprint(current)); text != "" && text != "<nil>" {
				return text
			}
		}
	}
	return ""
}

func newAgentEpisodeID(skill, inputHash string, createdAt time.Time) string {
	sum := sha256.Sum256([]byte(skill + "|" + inputHash + "|" + createdAt.Format(time.RFC3339Nano)))
	return "ep_" + hex.EncodeToString(sum[:12])
}

func percentileSummary(values []int64) map[string]any {
	if len(values) == 0 {
		return map[string]any{"p50": nil, "p95": nil, "p99": nil}
	}
	sort.Slice(values, func(i, j int) bool { return values[i] < values[j] })
	return map[string]any{
		"p50": percentile(values, 0.50),
		"p95": percentile(values, 0.95),
		"p99": percentile(values, 0.99),
	}
}

func percentile(values []int64, p float64) int64 {
	if len(values) == 0 {
		return 0
	}
	idx := int(float64(len(values)-1) * p)
	if idx < 0 {
		idx = 0
	}
	if idx >= len(values) {
		idx = len(values) - 1
	}
	return values[idx]
}
