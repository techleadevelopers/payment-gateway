package mcp

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestTierLimitSupportsMCPAvailabilityLoad(t *testing.T) {
	if got := tierLimit("sk_test_cfx_probe"); got != 600 {
		t.Fatalf("expected test MCP limit 600/min, got %d", got)
	}
	if got := tierLimit("sk_live_cfx_probe"); got != 2000 {
		t.Fatalf("expected live MCP limit 2000/min, got %d", got)
	}
	if got := tierLimit(""); got != 60 {
		t.Fatalf("expected anonymous MCP limit 60/min, got %d", got)
	}
}

func TestMCPStaticDiscoveryUsesCachedJSON(t *testing.T) {
	s := New(nil, nil, nil, nil, nil)
	req := httptest.NewRequest(http.MethodPost, "/mcp/tools/list", strings.NewReader(`{}`))
	rec := httptest.NewRecorder()

	s.handleToolsList(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec.Code)
	}
	if !strings.Contains(rec.Body.String(), `"tools"`) || !strings.Contains(rec.Body.String(), `"get_rates"`) {
		t.Fatalf("expected tools list response, got %s", rec.Body.String())
	}
}
