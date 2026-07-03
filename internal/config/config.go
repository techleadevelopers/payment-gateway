package config

import (
	"log"
	"os"
	"strconv"

	"github.com/joho/godotenv"
)

// Config centraliza todas as variáveis do seu .env de forma tipada e segura
type Config struct {
	DatabaseURL            string
	AllowedOrigins         string
	WebhookSecret          string
	Port                   string
	OrderMinBrl            float64
	OrderMaxBrl            float64
	RateLockSec            int
	RateLimitWindowMs      int
	RateLimitMax           int
	OrderRateLimitWindowMs int
	OrderRateLimitMax      int
	FeeBps                 int
	FeeMinBrl              float64
	BuyHotDerivationIndex  int

	// Tron / USDT TRC20
	TronFullNodeURL   string
	TronFullNodeUrl   string
	TronSolidityURL   string
	TronUsdtContract  string
	TronUsdtDecimals  int
	TronConfirmations int
	TronXPub          string
	TronHmacSecret    string

	// Regras de Limite e Fraude
	PixMaxOrdersPer24h      int
	PixMaxBrlPer24h         float64
	OrderHoldSecForNewDest  int
	TronDepositTolerancePct float64

	// PagBank
	PagSeguroApiToken   string
	PagSeguroApiBaseUrl string
	PixWebhookSecret    string

	// Tesouraria / signer / sweep
	TreasuryHot       string
	TreasuryCold      string
	SignerUrl         string
	SignerHmacSecret  string
	EnableSweepWorker bool
	EnableSweepStub   bool
	SweepBatchUsdtMin float64
	SweepBatchUsdtMax float64
	SweepFrequencyMs  int
	TronGasReserveTrx float64

	// SMTP / mensagens
	SMTPHost      string
	SMTPPort      int
	SMTPUser      string
	SMTPPass      string
	SMTPSecure    bool
	SMTPFromEmail string
	SMTPFromName  string
	OpsEmail      string
}

// LoadConfig é o cara que lê o .env e joga para dentro da estrutura acima
func LoadConfig() *Config {
	if err := godotenv.Load(); err != nil {
		log.Println("Aviso: Arquivo .env não encontrado, usando variáveis de ambiente do sistema")
	}

	return &Config{
		DatabaseURL:            getEnv("DATABASE_URL", ""),
		AllowedOrigins:         getEnv("ALLOWED_ORIGINS", "http://localhost:5173"),
		WebhookSecret:          getEnv("WEBHOOK_SECRET", ""),
		Port:                   getEnv("PORT", "3000"),
		OrderMinBrl:            getEnvAsFloat("ORDER_MIN_BRL", 10.0),
		OrderMaxBrl:            getEnvAsFloat("ORDER_MAX_BRL", 10000.0),
		RateLockSec:            getEnvAsInt("RATE_LOCK_SEC", 600),
		RateLimitWindowMs:      getEnvAsInt("RATE_LIMIT_WINDOW_MS", 60000),
		RateLimitMax:           getEnvAsInt("RATE_LIMIT_MAX", 100),
		OrderRateLimitWindowMs: getEnvAsInt("ORDER_RATE_LIMIT_WINDOW_MS", 60000),
		OrderRateLimitMax:      getEnvAsInt("ORDER_RATE_LIMIT_MAX", 20),
		FeeBps:                 getEnvAsInt("FEE_BPS", 0),
		FeeMinBrl:              getEnvAsFloat("FEE_MIN_BRL", 0),
		BuyHotDerivationIndex:  getEnvAsInt("BUY_HOT_DERIVATION_INDEX", 0),

		TronFullNodeURL:   getEnv("TRON_FULLNODE_URL", ""),
		TronFullNodeUrl:   getEnv("TRON_FULLNODE_URL", ""),
		TronSolidityURL:   getEnv("TRON_SOLIDITY_URL", ""),
		TronUsdtContract:  getEnv("TRON_USDT_CONTRACT", ""),
		TronUsdtDecimals:  getEnvAsInt("TRON_USDT_DECIMALS", 6),
		TronConfirmations: getEnvAsInt("TRON_CONFIRMATIONS", 20),
		TronXPub:          getEnv("TRON_XPUB", ""),
		TronHmacSecret:    getEnv("TRON_HMAC_SECRET", ""),

		PixMaxOrdersPer24h:      getEnvAsInt("PIX_MAX_ORDERS_PER_24H", 5),
		PixMaxBrlPer24h:         getEnvAsFloat("PIX_MAX_BRL_PER_24H", 20000.0),
		OrderHoldSecForNewDest:  getEnvAsInt("ORDER_HOLD_SEC_FOR_NEW_DEST", 180),
		TronDepositTolerancePct: getEnvAsFloat("TRON_DEPOSIT_TOLERANCE_PCT", 0.02),

		PagSeguroApiToken:   getEnv("PAGSEGURO_API_TOKEN", ""),
		PagSeguroApiBaseUrl: getEnv("PAGSEGURO_API_BASE_URL", "https://api.pagseguro.com"),
		PixWebhookSecret:    getEnv("PIX_WEBHOOK_SECRET", ""),

		TreasuryHot:       getEnv("TREASURY_HOT", ""),
		TreasuryCold:      getEnv("TREASURY_COLD", ""),
		SignerUrl:         getEnv("SIGNER_URL", ""),
		SignerHmacSecret:  getEnv("SIGNER_HMAC_SECRET", ""),
		EnableSweepWorker: getEnvAsBool("ENABLE_SWEEP_WORKER", false),
		EnableSweepStub:   getEnvAsBool("ENABLE_SWEEP_STUB", false),
		SweepBatchUsdtMin: getEnvAsFloat("SWEEP_BATCH_USDT_MIN", 0),
		SweepBatchUsdtMax: getEnvAsFloat("SWEEP_BATCH_USDT_MAX", 1_000_000),
		SweepFrequencyMs:  getEnvAsInt("SWEEP_FREQUENCY_MS", 30000),
		TronGasReserveTrx: getEnvAsFloat("TRON_GAS_RESERVE_TRX", 5),

		SMTPHost:      getEnv("SMTP_HOST", ""),
		SMTPPort:      getEnvAsInt("SMTP_PORT", 587),
		SMTPUser:      getEnv("SMTP_USER", ""),
		SMTPPass:      getEnv("SMTP_PASS", ""),
		SMTPSecure:    getEnvAsBool("SMTP_SECURE", false),
		SMTPFromEmail: getEnv("SMTP_FROM_EMAIL", ""),
		SMTPFromName:  getEnv("SMTP_FROM_NAME", "Swappy Financial"),
		OpsEmail:      getEnv("OPS_EMAIL", getEnv("SMTP_FROM_EMAIL", "")),
	}
}

// Auxiliares para leitura e conversão de tipos
func getEnv(key, defaultValue string) string {
	if value, exists := os.LookupEnv(key); exists {
		return value
	}
	return defaultValue
}

func getEnvAsInt(key string, defaultValue int) int {
	valueStr := getEnv(key, "")
	if value, err := strconv.Atoi(valueStr); err == nil {
		return value
	}
	return defaultValue
}

func getEnvAsFloat(key string, defaultValue float64) float64 {
	valueStr := getEnv(key, "")
	if value, err := strconv.ParseFloat(valueStr, 64); err == nil {
		return value
	}
	return defaultValue
}

func getEnvAsBool(key string, defaultValue bool) bool {
	valueStr := getEnv(key, "")
	if valueStr == "" {
		return defaultValue
	}
	value, err := strconv.ParseBool(valueStr)
	if err != nil {
		return defaultValue
	}
	return value
}
