package server

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"sort"
	"strings"
	"time"

	"payment-gateway/internal/security"
)

type signerProbeRequest struct {
	Samples   int `json:"samples"`
	TimeoutMS int `json:"timeoutMs"`
}

type signerProbeSample struct {
	Area      string         `json:"area"`
	Endpoint  string         `json:"endpoint"`
	Method    string         `json:"method"`
	Status    int            `json:"status"`
	OK        bool           `json:"ok"`
	Expected  []int          `json:"expected"`
	LatencyMS int64          `json:"latencyMs"`
	Error     string         `json:"error,omitempty"`
	Response  map[string]any `json:"response,omitempty"`
	At        string         `json:"at"`
}

type signerProbeEndpointSummary struct {
	Area      string  `json:"area"`
	Endpoint  string  `json:"endpoint"`
	Count     int     `json:"count"`
	OK        int     `json:"ok"`
	Errors    int     `json:"errors"`
	Available float64 `json:"availability"`
	P50       int64   `json:"p50"`
	P55       int64   `json:"p55"`
	P95       int64   `json:"p95"`
	P99       int64   `json:"p99"`
	Avg       int64   `json:"avg"`
	Max       int64   `json:"max"`
	LastError string  `json:"lastError,omitempty"`
}

type signerProbeSpec struct {
	area     string
	method   string
	path     string
	body     []byte
	signed   bool
	expected []int
}

func (s *Server) handleAdminSignerProbe(w http.ResponseWriter, r *http.Request) {
	if _, _, ok := s.authorizeAdmin(w, r); !ok {
		return
	}
	var req signerProbeRequest
	if r.Body != nil {
		_ = decodeJSON(r, &req)
	}
	if req.Samples <= 0 {
		req.Samples = 3
	}
	if req.Samples > 25 {
		req.Samples = 25
	}
	if req.TimeoutMS <= 0 {
		req.TimeoutMS = 3500
	}
	if req.TimeoutMS > 15000 {
		req.TimeoutMS = 15000
	}

	signerURL := strings.TrimRight(strings.TrimSpace(s.cfg.SignerUrl), "/")
	if signerURL == "" {
		writeJSON(w, http.StatusOK, map[string]any{
			"ok":          false,
			"configured":  false,
			"generatedAt": time.Now().UTC().Format(time.RFC3339Nano),
			"error":       "SIGNER_URL not configured",
		})
		return
	}

	specs := []signerProbeSpec{
		{area: "Availability", method: http.MethodGet, path: "/healthz", expected: []int{http.StatusOK}},
		{area: "Readiness", method: http.MethodGet, path: "/readyz", expected: []int{http.StatusOK}},
		{area: "Security", method: http.MethodPost, path: "/hd/transfer", body: []byte(`{}`), expected: []int{http.StatusUnauthorized}},
		{area: "Security", method: http.MethodPost, path: "/hd/contract-call", body: []byte(`{}`), expected: []int{http.StatusUnauthorized}},
	}
	if strings.TrimSpace(s.cfg.SignerHmacSecret) != "" {
		specs = append(specs, signerProbeSpec{
			area:     "Signed path",
			method:   http.MethodPost,
			path:     "/hd/transfer",
			body:     []byte(`{`),
			signed:   true,
			expected: []int{http.StatusBadRequest},
		})
	}

	client := &http.Client{Timeout: time.Duration(req.TimeoutMS) * time.Millisecond}
	samples := make([]signerProbeSample, 0, req.Samples*len(specs))
	for i := 0; i < req.Samples; i++ {
		for _, spec := range specs {
			samples = append(samples, s.runSignerProbe(r.Context(), client, signerURL, spec))
		}
	}

	summary := summarizeSignerProbeSamples(samples)
	ok := len(samples) > 0
	for _, sample := range samples {
		if !sample.OK {
			ok = false
			break
		}
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"ok":          ok,
		"configured":  true,
		"generatedAt": time.Now().UTC().Format(time.RFC3339Nano),
		"target":      maskSignerTarget(signerURL),
		"samples":     samples,
		"summary":     summary,
		"overall":     summarizeSignerProbeEndpoint("overall", "all", samples),
	})
}

func (s *Server) runSignerProbe(parent context.Context, client *http.Client, baseURL string, spec signerProbeSpec) signerProbeSample {
	ctx, cancel := context.WithTimeout(parent, client.Timeout)
	defer cancel()
	started := time.Now()
	sample := signerProbeSample{
		Area:     spec.area,
		Endpoint: spec.path,
		Method:   spec.method,
		Expected: spec.expected,
		At:       started.UTC().Format(time.RFC3339Nano),
	}
	req, err := http.NewRequestWithContext(ctx, spec.method, baseURL+spec.path, bytes.NewReader(spec.body))
	if err != nil {
		sample.Error = err.Error()
		sample.LatencyMS = time.Since(started).Milliseconds()
		return sample
	}
	req.Header.Set("Accept", "application/json")
	if len(spec.body) > 0 {
		req.Header.Set("Content-Type", "application/json")
	}
	if spec.signed {
		security.SignRawBodyHeaders(req, s.cfg.SignerHmacSecret, spec.body)
	}

	resp, err := client.Do(req)
	sample.LatencyMS = time.Since(started).Milliseconds()
	if err != nil {
		sample.Error = err.Error()
		return sample
	}
	defer resp.Body.Close()
	sample.Status = resp.StatusCode
	sample.OK = statusIn(resp.StatusCode, spec.expected)
	raw, _ := io.ReadAll(io.LimitReader(resp.Body, 8192))
	if len(raw) > 0 {
		var decoded map[string]any
		if err := json.Unmarshal(raw, &decoded); err == nil {
			sample.Response = decoded
		} else {
			sample.Response = map[string]any{"raw": string(raw)}
		}
	}
	if !sample.OK {
		sample.Error = fmt.Sprintf("unexpected status %d", resp.StatusCode)
	}
	return sample
}

func summarizeSignerProbeSamples(samples []signerProbeSample) []signerProbeEndpointSummary {
	groups := map[string][]signerProbeSample{}
	for _, sample := range samples {
		key := sample.Area + "|" + sample.Method + " " + sample.Endpoint
		groups[key] = append(groups[key], sample)
	}
	out := make([]signerProbeEndpointSummary, 0, len(groups))
	for key, group := range groups {
		parts := strings.SplitN(key, "|", 2)
		out = append(out, summarizeSignerProbeEndpoint(parts[0], parts[1], group))
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].P95 == out[j].P95 {
			return out[i].Endpoint < out[j].Endpoint
		}
		return out[i].P95 > out[j].P95
	})
	return out
}

func summarizeSignerProbeEndpoint(area, endpoint string, samples []signerProbeSample) signerProbeEndpointSummary {
	latencies := make([]int64, 0, len(samples))
	ok := 0
	lastError := ""
	for _, sample := range samples {
		latencies = append(latencies, sample.LatencyMS)
		if sample.OK {
			ok++
		} else if sample.Error != "" {
			lastError = sample.Error
		}
	}
	sort.Slice(latencies, func(i, j int) bool { return latencies[i] < latencies[j] })
	sum := int64(0)
	for _, latency := range latencies {
		sum += latency
	}
	avg := int64(0)
	if len(latencies) > 0 {
		avg = sum / int64(len(latencies))
	}
	availability := 0.0
	if len(samples) > 0 {
		availability = (float64(ok) / float64(len(samples))) * 100
	}
	return signerProbeEndpointSummary{
		Area:      area,
		Endpoint:  endpoint,
		Count:     len(samples),
		OK:        ok,
		Errors:    len(samples) - ok,
		Available: availability,
		P50:       signerProbePercentile(latencies, 50),
		P55:       signerProbePercentile(latencies, 55),
		P95:       signerProbePercentile(latencies, 95),
		P99:       signerProbePercentile(latencies, 99),
		Avg:       avg,
		Max:       signerProbePercentile(latencies, 100),
		LastError: lastError,
	}
}

func signerProbePercentile(sortedValues []int64, p int) int64 {
	if len(sortedValues) == 0 {
		return 0
	}
	if p >= 100 {
		return sortedValues[len(sortedValues)-1]
	}
	idx := ((p * len(sortedValues)) + 99) / 100
	if idx <= 0 {
		idx = 1
	}
	if idx > len(sortedValues) {
		idx = len(sortedValues)
	}
	return sortedValues[idx-1]
}

func statusIn(status int, expected []int) bool {
	for _, value := range expected {
		if status == value {
			return true
		}
	}
	return false
}

func maskSignerTarget(raw string) string {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return ""
	}
	if strings.Contains(raw, "railway.internal") || strings.Contains(raw, ".internal") {
		return "private signer"
	}
	return raw
}
