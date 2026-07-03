package settlement

import "strings"

const (
	StatusError    = "erro"
	StatusPaidFiat = "pago_fiat"
)

func PixWebhookStatus(providerStatus string) string {
	if strings.HasPrefix(strings.ToLower(strings.TrimSpace(providerStatus)), "conclu") {
		return StatusPaidFiat
	}
	return StatusError
}

func StripeWebhookStatus(eventType string) string {
	switch strings.TrimSpace(eventType) {
	case "checkout.session.completed", "payment_intent.succeeded", "charge.succeeded":
		return StatusPaidFiat
	default:
		return StatusError
	}
}

func ShouldPublishBuyPaid(status string) bool {
	return status == StatusPaidFiat
}
