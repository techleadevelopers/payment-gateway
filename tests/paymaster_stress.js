/**
 * ChainFX Gas Station — k6 stress test
 *
 * Scenarios:
 *   1. gas_status   — light, no auth, 50 VUs, sanity checks enabled/disabled response
 *   2. gas_quote    — medium, no auth, 20 VUs, checks fee fields
 *   3. gas_relay    — heavy, Bearer sk_test_*, 10 VUs, rate-limit handling
 *   4. gas_relay_id — medium, no auth, 10 VUs, 404 behaviour
 *
 * Run:
 *   k6 run tests/paymaster_stress.js \
 *     -e BASE_URL=http://localhost:8080 \
 *     -e API_KEY=sk_test_chainfx_local
 *
 * Thresholds (production-grade SLOs):
 *   - p95 latency < 800ms for quote/status
 *   - p95 latency < 2000ms for relay submission
 *   - error rate < 1% (excluding expected 429/503)
 */

import http from "k6/http";
import { check, sleep, group } from "k6";
import { Rate, Trend } from "k6/metrics";

// ── Custom metrics ─────────────────────────────────────────────────────────────
const relayErrorRate   = new Rate("relay_errors");
const quoteDuration    = new Trend("quote_duration_ms", true);
const relayDuration    = new Trend("relay_duration_ms", true);

// ── Config ────────────────────────────────────────────────────────────────────
const BASE_URL = __ENV.BASE_URL || "http://localhost:8080";
const API_KEY  = __ENV.API_KEY  || "sk_test_chainfx_local";

// ── Options ───────────────────────────────────────────────────────────────────
export const options = {
  scenarios: {
    gas_status: {
      executor: "constant-vus",
      vus: 50,
      duration: "30s",
      exec: "statusScenario",
      tags: { scenario: "gas_status" },
    },
    gas_quote: {
      executor: "constant-vus",
      vus: 20,
      duration: "30s",
      exec: "quoteScenario",
      tags: { scenario: "gas_quote" },
      startTime: "5s",
    },
    gas_relay: {
      executor: "constant-vus",
      vus: 10,
      duration: "30s",
      exec: "relayScenario",
      tags: { scenario: "gas_relay" },
      startTime: "10s",
    },
    gas_relay_id: {
      executor: "constant-vus",
      vus: 10,
      duration: "20s",
      exec: "relayGetScenario",
      tags: { scenario: "gas_relay_id" },
      startTime: "15s",
    },
  },
  thresholds: {
    // Status endpoint: very fast
    "http_req_duration{scenario:gas_status}":  ["p(95)<500"],
    // Quote endpoint: within 800 ms p95
    "http_req_duration{scenario:gas_quote}":   ["p(95)<800"],
    // Relay submission: within 2000 ms p95 (includes signer roundtrip or stub)
    "http_req_duration{scenario:gas_relay}":   ["p(95)<2000"],
    // GET relay: fast DB lookup
    "http_req_duration{scenario:gas_relay_id}": ["p(95)<400"],
    // Custom error rate: < 1% actual errors (429/503 are expected, not errors)
    relay_errors: ["rate<0.01"],
  },
};

const HEADERS_JSON = { "Content-Type": "application/json" };
const HEADERS_AUTH = {
  "Content-Type": "application/json",
  "Authorization": `Bearer ${API_KEY}`,
};

// ── Scenario: GET /v1/gas/status ───────────────────────────────────────────────
export function statusScenario() {
  group("GET /v1/gas/status", () => {
    const res = http.get(`${BASE_URL}/v1/gas/status`, { tags: { name: "gas_status" } });
    check(res, {
      "status is 200 or 503": (r) => r.status === 200 || r.status === 503,
      "response has enabled field": (r) => {
        try {
          const body = JSON.parse(r.body);
          return typeof body.enabled === "boolean";
        } catch {
          return false;
        }
      },
    });
  });
  sleep(0.1 + Math.random() * 0.3);
}

// ── Scenario: POST /v1/gas/quote ───────────────────────────────────────────────
export function quoteScenario() {
  group("POST /v1/gas/quote", () => {
    const payload = JSON.stringify({
      user_address: "0xd8dA6BF26964aF9D7eEd9e03E53415D37aA96045",
      tx_to:        "0x55d398326f99059fF775485246999027B3197955",
      tx_data:      "",
    });

    const start = Date.now();
    const res = http.post(`${BASE_URL}/v1/gas/quote`, payload, {
      headers: HEADERS_JSON,
      tags: { name: "gas_quote" },
    });
    quoteDuration.add(Date.now() - start);

    check(res, {
      "quote: acceptable status": (r) =>
        r.status === 200 || r.status === 503 || r.status === 400,
      "quote: fee_usdt present when 200": (r) => {
        if (r.status !== 200) return true;
        try {
          const body = JSON.parse(r.body);
          return typeof body.fee_usdt === "number" && body.fee_usdt >= 0;
        } catch {
          return false;
        }
      },
      "quote: valid_until_ms is future when 200": (r) => {
        if (r.status !== 200) return true;
        try {
          const body = JSON.parse(r.body);
          return body.valid_until_ms > Date.now();
        } catch {
          return false;
        }
      },
    });
  });
  sleep(0.2 + Math.random() * 0.5);
}

// ── Scenario: POST /v1/gas/relay ───────────────────────────────────────────────
// Each VU uses a unique (r, s) pair so sig hash never collides.
export function relayScenario() {
  group("POST /v1/gas/relay", () => {
    const vuID = __VU;
    const iter = __ITER;
    // Fake hex values — unique per (VU, iteration)
    const r = `0x${"a".repeat(60)}${String(vuID).padStart(2, "0")}${String(iter % 100).padStart(2, "0")}`;
    const s = `0x${"b".repeat(60)}${String(iter % 100).padStart(2, "0")}${String(vuID).padStart(2, "0")}`;

    const payload = JSON.stringify({
      user_address: "0xd8dA6BF26964aF9D7eEd9e03E53415D37aA96045",
      tx_to:        "0x55d398326f99059fF775485246999027B3197955",
      tx_data:      "",
      sig_r:        r,
      sig_s:        s,
      sig_v:        "0x1b",
      amount:       "10.000000",
      token_addr:   "0x55d398326f99059fF775485246999027B3197955",
      network:      "BSC",
    });

    const start = Date.now();
    const res = http.post(`${BASE_URL}/v1/gas/relay`, payload, {
      headers: HEADERS_AUTH,
      tags: { name: "gas_relay" },
    });
    relayDuration.add(Date.now() - start);

    const ok = check(res, {
      "relay: accepted or expected error": (r) =>
        r.status === 202 ||   // happy path
        r.status === 503 ||   // gas station disabled
        r.status === 429 ||   // rate limited (expected under load)
        r.status === 400 ||   // validation error
        r.status === 409,     // duplicate sig
    });

    // Only count as error if it's a true 5xx (not 503 which means disabled)
    if (res.status >= 500 && res.status !== 503) {
      relayErrorRate.add(1);
    } else {
      relayErrorRate.add(0);
    }

    if (!ok) {
      console.error(`relay unexpected status=${res.status} body=${res.body.substring(0, 200)}`);
    }
  });
  sleep(0.5 + Math.random() * 1.0);
}

// ── Scenario: GET /v1/gas/relay/{id} ──────────────────────────────────────────
export function relayGetScenario() {
  group("GET /v1/gas/relay/{id}", () => {
    // Use a fake UUID — should return 404
    const fakeID = "00000000-0000-0000-0000-000000000001";
    const res = http.get(`${BASE_URL}/v1/gas/relay/${fakeID}`, {
      tags: { name: "gas_relay_id" },
    });

    check(res, {
      "relay GET: 404 or 503 for unknown id": (r) =>
        r.status === 404 ||   // gas station enabled, relay not found
        r.status === 503,     // gas station disabled
    });
  });
  sleep(0.1 + Math.random() * 0.2);
}
