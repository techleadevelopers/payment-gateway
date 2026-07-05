import { ChainFX } from "../../sdk/node/index.js";

const chainfx = new ChainFX({
  apiKey: process.env.CHAINFX_API_KEY || "sk_test_chainfx_local",
  baseUrl: process.env.CHAINFX_API_BASE_URL || "http://localhost:8080"
});

const quote = await chainfx.quote({
  side: "buy",
  fiat: "BRL",
  asset: "USDT",
  amount: 500
});

console.log("quote", quote);

const order = await chainfx.buy({
  fiat: "BRL",
  asset: "USDT",
  amount: 500,
  wallet: "0x000000000000000000000000000000000000dEaD",
  customer: {
    email: "developer@example.com"
  }
});

console.log("order", order);
