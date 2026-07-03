package settlement

import "testing"

func TestPixWebhookStatus(t *testing.T) {
	tests := []struct {
		name   string
		input  string
		expect string
	}{
		{name: "concluido", input: "concluido", expect: StatusPaidFiat},
		{name: "concluida", input: "CONCLUIDA", expect: StatusPaidFiat},
		{name: "pending", input: "pending", expect: StatusError},
		{name: "empty", input: "", expect: StatusError},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := PixWebhookStatus(tt.input); got != tt.expect {
				t.Fatalf("expected %s, got %s", tt.expect, got)
			}
		})
	}
}

func TestStripeWebhookStatus(t *testing.T) {
	paidEvents := []string{"checkout.session.completed", "payment_intent.succeeded", "charge.succeeded"}
	for _, event := range paidEvents {
		if got := StripeWebhookStatus(event); got != StatusPaidFiat {
			t.Fatalf("expected %s for %s, got %s", StatusPaidFiat, event, got)
		}
	}
	if got := StripeWebhookStatus("payment_intent.payment_failed"); got != StatusError {
		t.Fatalf("expected %s, got %s", StatusError, got)
	}
}

func TestShouldPublishBuyPaid(t *testing.T) {
	if !ShouldPublishBuyPaid(StatusPaidFiat) {
		t.Fatal("expected pago_fiat to publish buy.paid")
	}
	if ShouldPublishBuyPaid(StatusError) {
		t.Fatal("expected erro not to publish buy.paid")
	}
}
