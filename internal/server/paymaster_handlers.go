package server

import (
        "encoding/json"
        "net/http"
        "strings"

        "payment-gateway/internal/paymaster"
)

// ── GET /v1/gas/status ─────────────────────────────────────────────────────────

func (s *Server) handleGasStatus(w http.ResponseWriter, r *http.Request) {
        if s.paymaster == nil {
                writeJSON(w, http.StatusServiceUnavailable, map[string]any{
                        "enabled": false,
                        "reason":  "gas station not initialised",
                })
                return
        }
        writeJSON(w, s.paymaster.HTTPStatus(), s.paymaster.StatusJSON(r.Context()))
}

// ── POST /v1/gas/quote ─────────────────────────────────────────────────────────

func (s *Server) handleGasQuote(w http.ResponseWriter, r *http.Request) {
        if !s.gasStationReady(w) {
                return
        }

        var body struct {
                UserAddress string `json:"user_address"`
                TxTo        string `json:"tx_to"`
                TxData      string `json:"tx_data"`
        }
        if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
                writeJSON(w, http.StatusBadRequest, map[string]any{"error": "invalid JSON"})
                return
        }
        if body.UserAddress == "" || body.TxTo == "" {
                writeJSON(w, http.StatusBadRequest, map[string]any{"error": "user_address and tx_to are required"})
                return
        }

        quote, err := s.paymaster.Quote(r.Context(), body.UserAddress, body.TxTo, nil)
        if err != nil {
                writeJSON(w, http.StatusBadRequest, map[string]any{"error": err.Error()})
                return
        }
        writeJSON(w, http.StatusOK, quote)
}

// ── POST /v1/gas/relay ─────────────────────────────────────────────────────────
// Rate-limited by tier:
//   no key      → 401
//   sk_test_*   → 10 req/min
//   sk_live_*   → 60 req/min

func (s *Server) handleGasRelay(w http.ResponseWriter, r *http.Request) {
        if !s.gasStationReady(w) {
                return
        }

        // Resolve API key from Authorization header.
        apiKey, keyMode := s.resolveAPIKey(r)
        if apiKey == "" {
                writeJSON(w, http.StatusUnauthorized, map[string]any{
                        "error": "Authorization header with Bearer sk_test_* or sk_live_* required",
                })
                return
        }

        // Tier-specific rate limit.
        var limit int
        switch keyMode {
        case "test":
                limit = 10
        case "live":
                limit = 60
        default:
                writeJSON(w, http.StatusUnauthorized, map[string]any{"error": "unrecognised API key prefix"})
                return
        }

        rlKey := "paymaster:relay:" + apiKey + ":" + keyMode
        if allowed, _, _ := s.limiter.AllowN(rlKey, limit); !allowed {
                w.Header().Set("X-RateLimit-Limit", itoa(limit))
                w.Header().Set("X-RateLimit-Window", "60")
                w.Header().Set("Retry-After", "60")
                writeJSON(w, http.StatusTooManyRequests, map[string]any{
                        "error": "rate limit exceeded",
                        "code":  "RATE_LIMITED",
                })
                return
        }

        var req paymaster.RelayRequest
        if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
                writeJSON(w, http.StatusBadRequest, map[string]any{"error": "invalid JSON"})
                return
        }
        if err := validateRelayRequest(&req); err != nil {
                writeJSON(w, http.StatusUnprocessableEntity, map[string]any{"error": err.Error(), "code": "VALIDATION_ERROR"})
                return
        }

        resp, err := s.paymaster.SubmitRelay(r.Context(), &req)
        if err != nil {
                if err == paymaster.ErrDuplicateSig {
                        writeJSON(w, http.StatusConflict, map[string]any{
                                "error": "relay already submitted with this signature",
                                "code":  "DUPLICATE_SIG",
                        })
                        return
                }
                writeJSON(w, http.StatusBadRequest, map[string]any{"error": err.Error()})
                return
        }

        writeJSON(w, http.StatusAccepted, resp)
}

// ── GET /v1/gas/relay/{id} ─────────────────────────────────────────────────────

func (s *Server) handleGasRelayGet(w http.ResponseWriter, r *http.Request) {
        if !s.gasStationReady(w) {
                return
        }
        id := r.PathValue("id")
        if id == "" {
                writeJSON(w, http.StatusBadRequest, map[string]any{"error": "id is required"})
                return
        }
        relay, err := s.paymaster.GetRelay(r.Context(), id)
        if err != nil {
                writeJSON(w, http.StatusNotFound, map[string]any{"error": err.Error()})
                return
        }
        writeJSON(w, http.StatusOK, relay)
}

// ── GET /api/admin/gas-station ─────────────────────────────────────────────────

func (s *Server) handleAdminGasStation(w http.ResponseWriter, r *http.Request) {
        if _, _, ok := s.authorizeAdmin(w, r); !ok {
                return
        }
        if s.paymaster == nil {
                writeJSON(w, http.StatusOK, map[string]any{"enabled": false})
                return
        }
        stats, err := s.paymaster.Stats(r.Context())
        if err != nil {
                writeJSON(w, http.StatusInternalServerError, map[string]any{"error": err.Error()})
                return
        }
        writeJSON(w, http.StatusOK, map[string]any{
                "status": s.paymaster.StatusJSON(r.Context()),
                "stats":  stats,
        })
}

// ── GET /api/admin/sweeper ─────────────────────────────────────────────────────

func (s *Server) handleAdminSweeper(w http.ResponseWriter, r *http.Request) {
        if _, _, ok := s.authorizeAdmin(w, r); !ok {
                return
        }
        runs, err := s.db.ListAutoSweeperRuns(r.Context(), 50)
        if err != nil {
                writeJSON(w, http.StatusInternalServerError, map[string]any{"error": err.Error()})
                return
        }
        stats, err := s.db.AutoSweeperStats(r.Context())
        if err != nil {
                writeJSON(w, http.StatusInternalServerError, map[string]any{"error": err.Error()})
                return
        }
        writeJSON(w, http.StatusOK, map[string]any{
                "runs":  runs,
                "stats": stats,
        })
}

// ── helpers ────────────────────────────────────────────────────────────────────

func (s *Server) gasStationReady(w http.ResponseWriter) bool {
        if s.paymaster == nil || !s.paymaster.IsEnabled() {
                writeJSON(w, http.StatusServiceUnavailable, map[string]any{
                        "error":   "gas station is not enabled",
                        "code":    "GAS_STATION_DISABLED",
                        "hint":    "Set GAS_STATION_ENABLED=true and restart the server",
                })
                return false
        }
        return true
}

// resolveAPIKey extracts the Bearer token and classifies it as "test" or "live".
func (s *Server) resolveAPIKey(r *http.Request) (string, string) {
        auth := r.Header.Get("Authorization")
        if auth == "" {
                return "", ""
        }
        key := strings.TrimPrefix(auth, "Bearer ")
        key = strings.TrimSpace(key)
        if key == "" {
                return "", ""
        }
        if strings.HasPrefix(key, "sk_test_") {
                return key, "test"
        }
        if strings.HasPrefix(key, "sk_live_") {
                return key, "live"
        }
        return "", ""
}

func validateRelayRequest(req *paymaster.RelayRequest) error {
        if req.UserAddress == "" {
                return errField("user_address")
        }
        if req.TxTo == "" {
                return errField("tx_to")
        }
        if req.SigR == "" || req.SigS == "" {
                return errField("sig_r / sig_s")
        }
        if req.Amount == "" {
                return errField("amount")
        }
        return nil
}

func errField(name string) error {
        return &fieldError{name: name}
}

type fieldError struct{ name string }

func (e *fieldError) Error() string {
        return "missing required field: " + e.name
}

func itoa(i int) string {
        return string(rune('0' + i%10))
}
