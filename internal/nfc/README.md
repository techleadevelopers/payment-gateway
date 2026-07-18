# ChainFX NFC Closed-Loop Rail

Este pacote implementa a parte backend/protocolo do pagamento NFC fechado da ChainFX. O app Android HCE fica no projeto `C:\Users\Paulo\Desktop\nfcemv-emulator` e foi adaptado para transmitir token ChainFX, nao PAN/Track2.

## O que foi implementado

- Token dinamico `nfc1.<payload>.<signature>` assinado por HMAC-SHA256.
- Claims minimas: `token_id`, `wallet`, `device_id`, `network`, `iat`, `exp`, `nonce`.
- Hash do token persistido, nunca dependencia de PAN/Track2.
- Autorizacao transacional com idempotencia.
- Ledger NFC simples com `available_usdt_micro` e `locked_usdt_micro`.
- Hold de USDT ao aprovar uma transacao.
- Protocolo APDU/TLV proprietario para leitor ChainFX.
- Client tipado para terminal chamar `/api/nfc/authorize`.
- Endpoints mobile para cartao digital HCE:
  - `GET /api/mobile/nfc/card`.
  - `POST /api/mobile/nfc/provision`.
- Respostas no estilo autorizador:
  - `00`: aprovado.
  - `51`: saldo insuficiente.
  - `05`: recusado.

## O que nao foi implementado ainda

- Integracao com POS Visa/Mastercard/adquirente.
- Certificacao EMVCo, PCI DSS, BIN sponsor ou issuer processor.
- HCE iOS em producao. No iOS, HCE de pagamento depende de recursos/entitlements e regras da Apple.

Este trilho e closed-loop: funciona com app ChainFX + leitor/terminal ChainFX. Um POS comum de adquirente nao vai rotear automaticamente para `/api/nfc/authorize`.

## Fluxo tecnico

1. O app autenticado chama `POST /api/mobile/nfc/provision` usando JWT mobile.
2. O backend emite token HMAC com TTL curto, por padrao 120 segundos.
3. O app Android HCE responde ao APDU do terminal com esse token opaco.
4. O terminal ChainFX envia o token para `POST /api/nfc/authorize`.
5. O backend verifica assinatura, expiracao, token persistido e idempotencia.
6. O backend calcula o USDT necessario usando a cotacao `USDT/BRL`.
7. O banco trava a linha de saldo com `SELECT ... FOR UPDATE`.
8. Se `available_usdt_micro >= required_usdt_micro`, move saldo para `locked_usdt_micro` e aprova.
9. Se nao houver saldo, grava a autorizacao como `requires_funding`.

## Endpoints

### Cartao digital mobile

```http
GET /api/mobile/nfc/card
Authorization: Bearer <mobile-access-token>
```

Resposta:

```json
{
  "card": {
    "type": "chainfx_closed_loop_nfc",
    "display_name": "ChainFX NFC",
    "wallet_address": "0x742d35cc6634c0532925a3b844bc454e4438f44e",
    "network": "BSC",
    "asset": "USDT",
    "aid": "F222222222",
    "hce": true,
    "scheme": "closed_loop"
  }
}
```

### Provisionamento mobile

```http
POST /api/mobile/nfc/provision
Authorization: Bearer <mobile-access-token>
Content-Type: application/json
```

```json
{
  "device_id": "android-device-id",
  "network": "BSC",
  "ttl_seconds": 120
}
```

Resposta:

```json
{
  "token": "nfc1...",
  "token_id": "...",
  "expires_at": "2026-07-18T13:53:00Z",
  "aid": "F222222222",
  "network": "BSC",
  "apdu": {
    "response_template": "70",
    "token_tag": "DF01",
    "version_tag": "DF02"
  }
}
```

### Provisionamento terminal/admin

```http
POST /api/nfc/provision
Authorization: Bearer sk_test_...
Content-Type: application/json
```

```json
{
  "wallet_address": "0x742d35cc6634c0532925a3b844bc454e4438f44e",
  "device_id": "android-device-id",
  "network": "BSC",
  "ttl_seconds": 120
}
```

Resposta:

```json
{
  "token": "nfc1...",
  "token_id": "...",
  "expires_at": "2026-07-18T13:53:00Z",
  "network": "BSC"
}
```

### Autorizacao

```http
POST /api/nfc/authorize
Authorization: Bearer sk_test_...
Idempotency-Key: terminal-tx-001
Content-Type: application/json
```

```json
{
  "token": "nfc1...",
  "amount_brl": "25.90",
  "currency": "BRL",
  "merchant_id": "merchant_demo",
  "terminal_id": "terminal_01",
  "external_ref": "cupom-123",
  "idempotency_key": "terminal-tx-001"
}
```

Resposta aprovada:

```json
{
  "authorization_id": "nfc_auth_...",
  "status": "approved",
  "response_code": "00",
  "required_usdt": "4.712345",
  "hold_expires_at": "2026-07-18T14:08:00Z"
}
```

Resposta sem saldo:

```json
{
  "status": "requires_funding",
  "response_code": "51",
  "reason": "insufficient_usdt"
}
```

### Funding sandbox

Disponivel apenas com `ALLOW_SIMULATIONS=true`.

```http
POST /api/nfc/sandbox/fund
```

```json
{
  "wallet_address": "0x742d35cc6634c0532925a3b844bc454e4438f44e",
  "network": "BSC",
  "amount_usdt": "100.000000"
}
```

Em producao, esse saldo deve vir de deposito/escrow on-chain reconciliado pelo backend.

## Schema

Migration principal:

```text
migrations/020_nfc_closed_loop.sql
```

Tabelas:

- `nfc_tokens`: token opaco, hash, wallet, device, rede, expiracao e status.
- `nfc_wallet_balances`: saldo disponivel/travado por wallet, rede e asset.
- `nfc_authorizations`: autorizacoes idempotentes, merchant, terminal, valor, taxa, status e hold.

## Variaveis

```env
NFC_ENABLED=true
NFC_TOKEN_SECRET=use-um-segredo-forte
NFC_TOKEN_TTL_SEC=120
NFC_HOLD_TTL_SEC=900
NFC_MAX_AMOUNT_BRL=500
```

Em producao, `NFC_TOKEN_SECRET` e obrigatorio quando `NFC_ENABLED=true`.

## Integracao Android HCE

O projeto `nfcemv-emulator` ja tem os pontos certos:

- `CardApduService.java`: recebe APDU.
- `apduservice.xml`: registra AIDs.
- `ApduUtils.java`: helpers de HEX/APDU.

Implementacao aplicada no laboratorio Android:

1. Antes da aproximacao, app chama `/api/mobile/nfc/provision`.
2. App guarda o token em `HceTokenStore`, com expiracao.
3. `CardApduService.processCommandApdu` responde ao leitor com esse token em TLV proprietario.
4. Leitor ChainFX extrai o token e chama `/api/nfc/authorize`.

Nao usar PAN real, CVV, Track2 real ou dados de bandeira nesse fluxo.

### Contrato APDU

O AID fechado da ChainFX e:

```text
F222222222
```

O app Android registra esse AID com `android:category="other"` e `android:requireDeviceUnlock="true"`.

SELECT esperado:

```text
00 A4 04 00 05 F2 22 22 22 22
```

Resposta SELECT:

```text
6F ... 84 05 F222222222 A5 ... 50 0B "ChainFX NFC" 87 01 01 9000
```

Resposta de token para `GPO`, `READ RECORD` ou `GET DATA`:

```text
70 <len>
  DF02 01 01
  DF01 <len> <token nfc1... em UTF-8>
9000
```

Sem token provisionado ou token expirado:

```text
6985
```

Funcoes Go do protocolo:

- `BuildTokenResponse(token string) ([]byte, error)`: gera resposta APDU `70 + DF01 + 9000`.
- `ParseTokenResponse(apdu []byte) (string, error)`: extrai token `nfc1...` no leitor/terminal.
- `TerminalClient.Authorize(ctx, req)`: chama `/api/nfc/authorize` com timeout padrao de 1500 ms.

Funcoes Go do token:

- `IssueToken(secret, wallet, deviceID, network, ttl, now)`: emite token opaco assinado.
- `VerifyToken(secret, token, now)`: valida assinatura, estrutura e expiracao.
- `TokenHash(token)`: hash SHA-256 persistido no banco.

## Latencia medida

Ambiente:

- Windows, PowerShell.
- Pacote: `payment-gateway/internal/nfc`.
- Teste: `TestTokenLatencyPercentiles`.
- Operacao medida: `IssueToken + VerifyToken`.
- Amostras: 1000 lotes.
- Tamanho do lote: 100 operacoes.
- Total: 100000 operacoes.

Comando:

```powershell
go test ./internal/nfc -run TestTokenLatencyPercentiles -count=1 -v
```

Resultado desta maquina:

```text
p50 = 9.973us
p55 = 9.987us
p95 = 100.645us
p99 = 101.557us
max = 116.765us
```

Leitura:

- O custo criptografico local do token nao e gargalo.
- A latencia real de autorizacao NFC sera dominada por HTTP, Postgres, cotacao `USDT/BRL` e rede do terminal.
- Para producao, medir tambem `/api/nfc/authorize` end-to-end com Postgres real e carga concorrente.

## Testes

```powershell
go test ./internal/nfc
go test ./internal/nfc ./internal/database ./internal/server
go build -o api-nfc.exe ./cmd/api
```

Ultima validacao local:

```text
go test ./internal/nfc ./internal/database ./internal/server
ok

go build -o api-nfc.exe ./cmd/api
ok

GET http://127.0.0.1:18080/healthz
200 {"ok":true}
```
