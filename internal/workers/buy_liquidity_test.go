package workers

import (
	"context"
	"testing"

	"payment-gateway/internal/config"
	"payment-gateway/internal/database"
)

func TestResolveLiquidityPairRequiresContractsForEVMTokens(t *testing.T) {
	cfg := &config.Config{
		LiquidityAllowedPairs: "AVAX:BSC,AVAX:POLYGON:0x0000000000000000000000000000000000000001:18,BNB:BSC,BTC:BITCOIN",
	}

	if _, ok := resolveLiquidityPair(cfg, "AVAX", "BSC"); ok {
		t.Fatal("expected AVAX/BSC without contract to be blocked")
	}
	avaxPolygon, ok := resolveLiquidityPair(cfg, "AVAX", "POLYGON")
	if !ok {
		t.Fatal("expected AVAX/POLYGON with contract to be allowed")
	}
	if avaxPolygon.ContractAddress == "" || avaxPolygon.Decimals != 18 {
		t.Fatalf("unexpected AVAX/POLYGON pair: %+v", avaxPolygon)
	}
	if _, ok := resolveLiquidityPair(cfg, "AVAX", "AVALANCHE"); ok {
		t.Fatal("expected AVAX native network to be blocked")
	}
	if _, ok := resolveLiquidityPair(cfg, "BNB", "BSC"); !ok {
		t.Fatal("expected native BNB/BSC to be allowed without contract")
	}
	if _, ok := resolveLiquidityPair(cfg, "BTC", "BITCOIN"); !ok {
		t.Fatal("expected native BTC/BITCOIN to be allowed without contract")
	}
}

func TestResolveLiquidityPairHydratesConfiguredUSDTContracts(t *testing.T) {
	cfg := &config.Config{
		LiquidityAllowedPairs: "USDT:BSC,USDT:POLYGON",
		BscUsdtContract:       "0x55d398326f99059ff775485246999027b3197955",
		PolygonUsdtContract:   "0xc2132d05d31c914a87c6611c10748aeb04b58e8f",
	}

	bsc, ok := resolveLiquidityPair(cfg, "USDT", "BEP-20")
	if !ok {
		t.Fatal("expected USDT/BSC to be allowed")
	}
	if bsc.ContractAddress != cfg.BscUsdtContract || bsc.Decimals != 18 {
		t.Fatalf("unexpected USDT/BSC pair: %+v", bsc)
	}

	polygon, ok := resolveLiquidityPair(cfg, "USDT", "MATIC")
	if !ok {
		t.Fatal("expected USDT/POLYGON to be allowed")
	}
	if polygon.ContractAddress != cfg.PolygonUsdtContract || polygon.Decimals != 6 {
		t.Fatalf("unexpected USDT/POLYGON pair: %+v", polygon)
	}
}

func TestTryLiquidityExecutionSkipsUSDTByPolicy(t *testing.T) {
	worker := &BuySendWorker{
		cfg: &config.Config{
			LiquidityRouterEnabled:    true,
			LiquidityRouterSkipAssets: "USDT",
			LiquidityAllowedPairs:     "USDT:BSC",
			BscUsdtContract:           "0x55d398326f99059ff775485246999027b3197955",
		},
	}
	if worker.tryLiquidityExecution(context.Background(), &database.BuyOrder{ID: "buy-1", Asset: "USDT", Network: "BSC"}) {
		t.Fatal("USDT must stay on hot-wallet/signer flow and skip liquidity router")
	}
}
