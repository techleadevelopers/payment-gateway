package mcp

import (
	"context"
	"fmt"
	"net/http"
	"strings"

	"payment-gateway/internal/database"
	"payment-gateway/internal/webhooks"
)

// Resource describes an MCP resource: read-only context an agent can fetch.
type Resource struct {
	URI         string `json:"uri"`
	Name        string `json:"name"`
	Description string `json:"description"`
	MimeType    string `json:"mimeType"`
}

func (s *Server) resources() []Resource {
	return []Resource{
		{URI: "chainfx://rates/latest", Name: "Current rates", Description: "USDT/BRL, USDT/USD, BTC/USDT and supported pairs.", MimeType: "application/json"},
		{URI: "chainfx://marketplace/capabilities", Name: "Capability Network", Description: "Capability-first network for AI agents to discover, execute, meter, bill and settle digital capabilities.", MimeType: "application/json"},
		{URI: "chainfx://capability-contracts/{id}", Name: "Capability Contract", Description: "Versioned input/output contract for a capability. Replace {id} with document_ocr, llm_chat, etc.", MimeType: "application/json"},
		{URI: "chainfx://marketplace/products", Name: "Marketplace Products", Description: "Premium marketplace products and plans kept for product-level compatibility.", MimeType: "application/json"},
		{URI: "chainfx://agent/assets", Name: "Agent Rail Assets", Description: "Stablecoin assets enabled for Agent Rail and capability payments.", MimeType: "application/json"},
		{URI: "chainfx://webhooks/events", Name: "Automation events", Description: "Automation webhook events available to n8n/Zapier/Make including M2M and capability lifecycle events.", MimeType: "application/json"},
		{URI: "chainfx://webhooks/subscriptions", Name: "Webhook subscriptions", Description: "Currently configured automation subscriptions.", MimeType: "application/json"},
		{URI: "chainfx://orders/{id}", Name: "Order by id", Description: "Details for a buy/sell order. Replace {id} with the real id.", MimeType: "application/json"},
		{URI: "chainfx://agent/grants/{wallet}", Name: "Agent active grants", Description: "Active access grants (capability tokens with quota) for an agent wallet. Replace {wallet} with EVM address.", MimeType: "application/json"},
		{URI: "chainfx://agent/policy/{wallet}", Name: "Agent policy", Description: "Execution policy, spend limits and pricing overrides for an agent wallet. Replace {wallet} with EVM address.", MimeType: "application/json"},
		{URI: "chainfx://agent/intents/{wallet}", Name: "Agent payment intents", Description: "Recent M2M payment intents for an agent wallet. Replace {wallet} with EVM address.", MimeType: "application/json"},
		{URI: "chainfx://mcp/registry", Name: "MCP Capability Registry", Description: "Machine-readable capability catalog for MCP client discovery.", MimeType: "application/json"},
	}
}

func (s *Server) handleResourcesList(w http.ResponseWriter, r *http.Request) {
	writeCachedJSON(w, http.StatusOK, s.resourcesJSON)
}

type resourceReadRequest struct {
	URI string `json:"uri"`
}

func (s *Server) handleResourcesRead(w http.ResponseWriter, r *http.Request) {
	var req resourceReadRequest
	if err := decodeJSON(r, &req); err != nil {
		writeMCPError(w, http.StatusBadRequest, "JSON invalido")
		return
	}
	content, err := s.readResource(r.Context(), req.URI)
	if err != nil {
		writeMCPError(w, http.StatusNotFound, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"contents": []map[string]any{{"uri": req.URI, "mimeType": "application/json", "json": content}},
	})
}

func (s *Server) readResource(ctx context.Context, uri string) (any, error) {
	switch {
	case uri == "chainfx://rates/latest":
		return s.toolGetRates(), nil
	case uri == "chainfx://marketplace/capabilities":
		return s.db.ListMarketplaceCapabilities(ctx, database.MarketplaceProductFilters{})
	case strings.HasPrefix(uri, "chainfx://capability-contracts/"):
		id := strings.TrimPrefix(uri, "chainfx://capability-contracts/")
		contract, err := s.db.GetMarketplaceCapabilityContract(ctx, id, "v1")
		if err != nil {
			return nil, err
		}
		if contract == nil {
			return nil, fmt.Errorf("contrato de capability nao encontrado: %s", id)
		}
		return contract, nil
	case uri == "chainfx://marketplace/products":
		return s.db.ListMarketplaceProducts(ctx, database.MarketplaceProductFilters{})
	case uri == "chainfx://agent/assets":
		return s.db.ListAgentSupportedAssets(ctx)
	case uri == "chainfx://webhooks/events":
		return webhooks.AllEvents(), nil
	case uri == "chainfx://webhooks/subscriptions":
		return s.db.ListWebhookSubscriptions(ctx)
	case strings.HasPrefix(uri, "chainfx://orders/"):
		id := strings.TrimPrefix(uri, "chainfx://orders/")
		return s.toolGetOrderStatus(ctx, map[string]any{"orderId": id})
	case strings.HasPrefix(uri, "chainfx://agent/grants/"):
		wallet := strings.TrimPrefix(uri, "chainfx://agent/grants/")
		return s.toolListAgentGrants(ctx, map[string]any{"agentWallet": wallet})
	case strings.HasPrefix(uri, "chainfx://agent/policy/"):
		wallet := strings.TrimPrefix(uri, "chainfx://agent/policy/")
		return s.toolGetAgentPolicy(ctx, map[string]any{"agentWallet": wallet})
	case strings.HasPrefix(uri, "chainfx://agent/intents/"):
		wallet := strings.TrimPrefix(uri, "chainfx://agent/intents/")
		return s.toolListAgentPaymentIntents(ctx, map[string]any{"agentWallet": wallet})
	case uri == "chainfx://mcp/registry":
		return s.db.ListMarketplaceCapabilities(ctx, database.MarketplaceProductFilters{})
	default:
		return nil, fmt.Errorf("recurso desconhecido: %s", uri)
	}
}
