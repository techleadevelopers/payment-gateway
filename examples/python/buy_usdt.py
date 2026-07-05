import os
import sys

sys.path.append(os.path.join(os.path.dirname(__file__), "..", "..", "sdk", "python"))

from chainfx import ChainFX


chainfx = ChainFX(
    api_key=os.getenv("CHAINFX_API_KEY", "sk_test_chainfx_local"),
    base_url=os.getenv("CHAINFX_API_BASE_URL", "http://localhost:8080"),
)

quote = chainfx.quote(side="buy", fiat="BRL", asset="USDT", amount=500)
print("quote", quote)

order = chainfx.buy(
    fiat="BRL",
    asset="USDT",
    amount=500,
    wallet="0x000000000000000000000000000000000000dEaD",
    customer={"email": "developer@example.com"},
)
print("order", order)
