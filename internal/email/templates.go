package email

import (
	"fmt"
	"html"
	"strings"
	"time"
)

type Brand struct {
	Name         string
	LogoURL      string
	SiteURL      string
	Address      string
	SupportEmail string
	Year         int
}

type Receipt struct {
	Brand        Brand
	Kind         string
	OrderID      string
	Asset        string
	Network      string
	AmountFiat   float64
	FeeFiat      float64
	PayoutFiat   float64
	CryptoAmount float64
	Rate         float64
	Wallet       string
	TxHash       string
	CompletedAt  time.Time
}

type MarketingCampaign struct {
	Brand       Brand
	Subject     string
	Headline    string
	Intro       string
	Body        string
	CTA         string
	CTAURL      string
	Unsubscribe string
}

func BuildReceiptMessage(to, subject string, r Receipt) Message {
	action := "Compra finalizada"
	intro := "Seu pagamento foi confirmado e seus ativos digitais foram enviados para a wallet informada."
	primary := "Valor pago"
	secondary := "USDT enviado"
	if r.Kind == "sell" {
		action = "Venda finalizada"
		intro = "Seu deposito foi confirmado e o PIX foi enviado para a chave informada."
		primary = "PIX enviado"
		secondary = "USDT recebido"
	}
	when := r.CompletedAt
	if when.IsZero() {
		when = time.Now()
	}
	rows := []detailRow{
		{primary, moneyBRL(firstPositive(r.AmountFiat, r.PayoutFiat))},
		{secondary, fmt.Sprintf("%.8f %s", r.CryptoAmount, fallback(r.Asset, "USDT"))},
		{"Rede", fallback(r.Network, "BSC")},
		{"Cotacao", moneyBRL(r.Rate)},
		{"Taxas ChainFX", moneyBRL(r.FeeFiat)},
		{"Hash/ID da transacao", fallback(r.TxHash, "processado")},
		{"Ordem", r.OrderID},
		{"Concluido em", when.Format("02/01/2006 15:04 MST")},
	}
	if strings.TrimSpace(r.Wallet) != "" {
		rows = append(rows[:3], append([]detailRow{{"Wallet", r.Wallet}}, rows[3:]...)...)
	}
	htmlBody := shell(r.Brand, action, intro, "Ver detalhes", orderURL(r.Brand.SiteURL, r.OrderID), rows, "")
	textBody := textReceipt(action, intro, rows)
	return Message{To: to, Subject: subject, TextBody: textBody, HTMLBody: htmlBody}
}

func BuildMarketingMessage(to string, c MarketingCampaign) Message {
	subject := fallback(c.Subject, "Conheca a ChainFX")
	rows := []detailRow{}
	body := strings.TrimSpace(c.Intro)
	if strings.TrimSpace(c.Body) != "" {
		body = strings.TrimSpace(body + "\n\n" + c.Body)
	}
	htmlBody := shell(c.Brand, fallback(c.Headline, subject), body, fallback(c.CTA, "Abrir ChainFX"), fallback(c.CTAURL, c.Brand.SiteURL), rows, c.Unsubscribe)
	textBody := fallback(c.Headline, subject) + "\n\n" + body + "\n\n" + fallback(c.CTAURL, c.Brand.SiteURL)
	if c.Unsubscribe != "" {
		textBody += "\n\nDescadastro: " + c.Unsubscribe
	}
	return Message{To: to, Subject: subject, TextBody: textBody, HTMLBody: htmlBody}
}

type detailRow struct {
	Label string
	Value string
}

func shell(brand Brand, title, intro, cta, ctaURL string, rows []detailRow, unsubscribe string) string {
	var detail strings.Builder
	if len(rows) > 0 {
		detail.WriteString(`<div style="border:1px solid #e4e5e8;border-radius:12px;overflow:hidden;margin:28px 0;">`)
		for _, row := range rows {
			detail.WriteString(`<div style="display:flex;gap:18px;justify-content:space-between;padding:14px 18px;border-bottom:1px solid #eceef1;">`)
			detail.WriteString(`<span style="color:#6f737b;">` + html.EscapeString(row.Label) + `</span>`)
			detail.WriteString(`<strong style="color:#202124;text-align:right;word-break:break-all;">` + html.EscapeString(row.Value) + `</strong>`)
			detail.WriteString(`</div>`)
		}
		detail.WriteString(`</div>`)
	}
	unsub := ""
	if unsubscribe != "" {
		unsub = ` · <a href="` + html.EscapeString(unsubscribe) + `" style="color:#8a8d94;text-decoration:none;">Unsubscribe</a>`
	}
	support := ""
	if brand.SupportEmail != "" {
		support = `Reply to this email or contact ` + html.EscapeString(brand.SupportEmail) + `.`
	}
	return `<!doctype html><html><body style="margin:0;background:#f4f5f7;font-family:Inter,Arial,sans-serif;color:#202124;">
<div style="max-width:520px;margin:28px auto;padding:0 14px;">
  <div style="background:#fff;border:1px solid #dedfe3;border-radius:16px;overflow:hidden;">
    <div style="padding:42px 32px 34px;">
      <h1 style="font-size:28px;line-height:1.2;margin:0 0 18px;font-weight:800;color:#202124;">` + html.EscapeString(title) + `</h1>
      <p style="font-size:16px;line-height:1.65;color:#686c74;margin:0 0 10px;white-space:pre-line;">` + html.EscapeString(intro) + `</p>
      ` + detail.String() + `
      <div style="text-align:center;margin-top:28px;">
        <a href="` + html.EscapeString(ctaURL) + `" style="display:inline-block;background:#202124;color:#fff;text-decoration:none;border-radius:999px;padding:15px 34px;font-weight:800;">` + html.EscapeString(cta) + `</a>
      </div>
      <p style="font-size:13px;line-height:1.6;color:#8a8d94;margin:28px 0 0;">` + support + `</p>
    </div>
    <div style="background:#f7f7f9;border-top:1px solid #e4e5e8;padding:32px;">
      <img src="` + html.EscapeString(brand.LogoURL) + `" alt="` + html.EscapeString(brand.Name) + `" style="height:34px;max-width:150px;display:block;margin-bottom:24px;">
      <p style="font-size:13px;color:#8a8d94;margin:0 0 14px;">Help · Terms · Privacy` + unsub + `</p>
      <p style="font-size:12px;color:#8a8d94;margin:0;">© ` + fmt.Sprint(brand.Year) + ` ` + html.EscapeString(brand.Name) + ` · ` + html.EscapeString(brand.Address) + `</p>
    </div>
  </div>
</div>
</body></html>`
}

func textReceipt(title, intro string, rows []detailRow) string {
	var b strings.Builder
	b.WriteString(title + "\n\n" + intro + "\n\n")
	for _, row := range rows {
		b.WriteString(row.Label + ": " + row.Value + "\n")
	}
	return b.String()
}

func orderURL(siteURL, orderID string) string {
	siteURL = strings.TrimRight(fallback(siteURL, "https://www.chainfx.store"), "/")
	if orderID == "" {
		return siteURL
	}
	return siteURL + "/?order=" + orderID
}

func moneyBRL(v float64) string {
	if v <= 0 {
		return "-"
	}
	return fmt.Sprintf("R$ %.2f", v)
}

func firstPositive(values ...float64) float64 {
	for _, value := range values {
		if value > 0 {
			return value
		}
	}
	return 0
}

func fallback(value, fallback string) string {
	if strings.TrimSpace(value) != "" {
		return strings.TrimSpace(value)
	}
	return fallback
}
