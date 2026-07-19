# ChainFX EVM Contracts

## Status atual no backend

O Gas Station/Paymaster integrado hoje roda majoritariamente off-chain:

- `internal/paymaster`: quote, relay, idempotencia, retry, batching e DLQ.
- `internal/rpc`: pool RPC e health checks.
- `signer/`: assinatura isolada e custody guard.
- `gas_relay_requests` e `auto_sweeper_runs`: auditoria/persistencia.

Os contratos deste diretorio continuam sendo a camada opcional de vault/delegates para governanca on-chain, limites e hardening. Nao sao requisito para publicar o MCP ou usar `/v1/gas/*` no corte atual.

## Laboratorio sem contratos

Enquanto o fluxo produtivo roda com wallet direta + signer + custody guard, esta pasta pode ser usada como bancada de teste:

- gerar wallets de sistema/teste;
- criar wallets EIP probe;
- manter contratos compilando para evolucao futura;
- rodar testes adversariais contra o signer sem fazer deploy;
- validar HMAC, nonce/replay, payload tampering, allowlist e readiness.

Smoke adversarial do signer:

```powershell
cd C:\Users\Paulo\Desktop\payment-gateway\contracts
$env:SIGNER_URL="http://127.0.0.1:4010"
$env:SIGNER_HMAC_SECRET="mesmo segredo do signer"
npm run smoke:signer-adversarial
```

O script usa `amount: "0"` nos casos assinados para passar pela autenticação e ser barrado por policy antes de qualquer envio on-chain. Nao use wallets mainnet com saldo nesses testes.

O final do relatorio imprime latencia agregada:

```text
count=16 min=...ms avg=...ms p50=...ms p55=...ms p75=...ms p90=...ms p95=...ms p99=...ms max=...ms
```

Casos cobertos:

- HMAC ausente/invalido;
- timestamp expirado;
- payload alterado depois da assinatura;
- replay de nonce;
- replay paralelo do mesmo nonce em primeiro uso;
- replay paralelo de nonce ja consumido, que deve rejeitar 10/10 tentativas;
- token fora da allowlist;
- valor acima do limite;
- rede invalida;
- recipient invalido;
- idempotency key repetida;
- `/readyz` sem treasury contract obrigatorio.

Teste live/testnet com envio real fica separado e exige flag explicita:

```powershell
$env:SIGNER_URL="http://127.0.0.1:4010"
$env:SIGNER_HMAC_SECRET="mesmo segredo do signer"
$env:SIGNER_LIVE_TEST_TO="0xWalletDeTeste"
$env:SIGNER_LIVE_TEST_TOKEN="0xTokenDeTeste"
$env:SIGNER_LIVE_TEST_AMOUNT="0.01"
npm run smoke:signer-live-testnet -- --i-understand-this-sends-funds
```

Esse comando envia transacao de verdade. Use apenas testnet ou wallet com saldo pequeno.


Contratos editaveis para operar custodia/payout em redes EVM com foco em seguranca operacional. BSC continua sendo o caminho principal do core atual; Polygon foi adicionada como rede opcional para deploy do mesmo vault em liquidação/settlement de baixo custo.

## Contratos

### `ChainFXTreasuryVault`

Vault de treasury/payout para ERC20, como USDT/USDC em BSC ou Polygon.

Controles principais:

- owner em duas etapas;
- guardians com poder de pause/blocklist;
- operators para payout;
- allowlist de tokens;
- allowlist/blocklist de recipients;
- limite maximo por transferencia;
- limite diario por token;
- idempotencia por `operationId`;
- eventos para auditoria.

Uso recomendado:

```text
Owner multisig
        |
configura token, operadores, guardians e limites
        |
Core/signer solicita payout ao operator
        |
Vault envia USDT ao cliente respeitando limites
```

### `ChainFXDelegateRegistry`

Registry de delegates EIP-7702 confiaveis.

O signer Go ainda valida delegate e bytecode hash off-chain. O registry e uma fonte on-chain auditavel para governanca, incidentes e revogacao.

### `ChainFX7702PayoutDelegate`

Delegate EIP-7702 minimo para payout controlado.

Importante:

- nao possui `execute()` generico;
- nao permite chamada arbitraria;
- exige token permitido;
- exige recipient permitido;
- usa `operationId` para evitar replay;
- pode ser pausado.

Antes de colocar esse contrato em `CUSTODY_TRUSTED_DELEGATES`, faca deploy em testnet, registre o bytecode hash, teste com baixo saldo e valide o comportamento do signer Go em `CUSTODY_MODE=shadow` e depois `paper`.

## Setup

```powershell
cd C:\Users\Paulo\Desktop\payment-gateway\contracts
npm ci --legacy-peer-deps
npm run compile
npm test
```

O Hardhat carrega automaticamente:

- `C:\Users\Paulo\Desktop\payment-gateway\.env`
- `C:\Users\Paulo\Desktop\payment-gateway\contracts\.env`

Chaves aceitas para deploy, em ordem:

```text
DEPLOYER_PRIVATE_KEY
PRIVATE_KEY
EVM_PRIVATE_KEY
```

O script nunca imprime a chave privada. Ele imprime apenas endereço público do deployer/owner/operator.

## Deploy BSC Testnet

```powershell
$env:PRIVATE_KEY="0x..."
$env:CONTRACT_OWNER="0xMultisigOuOwner"
$env:CONTRACT_OPERATOR="0xSignerOuOperador"
$env:BSC_TESTNET_RPC_URL="https://data-seed-prebsc-1-s1.binance.org:8545/"
npm run preflight:testnet
npm run deploy:testnet
```

## Deploy BSC Mainnet

```powershell
$env:PRIVATE_KEY="0x..."
$env:CONTRACT_OWNER="0xMultisigOuOwner"
$env:CONTRACT_OPERATOR="0xSignerOuOperador"
$env:BSC_RPC_URL="https://..."
$env:BSC_USDT_CONTRACT="0x55d398326f99059fF775485246999027B3197955"
$env:TREASURY_MAX_TRANSFER_USDT="100"
$env:TREASURY_DAILY_LIMIT_USDT="1000"
npm run preflight:bsc
npm run deploy:bsc
```

## Deploy Polygon Amoy

Polygon Amoy é a testnet atual para Polygon PoS. Chain ID `80002`; mainnet Polygon PoS usa chain ID `137`. A Polygon mantém instruções oficiais para adicionar Polygon/Amoy via ChainList/MetaMask, e a página de Amoy informa RPC `https://rpc-amoy.polygon.technology/` e chain ID `80002`.

```powershell
$env:PRIVATE_KEY="0x..."
$env:CONTRACT_OWNER="0xMultisigOuOwner"
$env:CONTRACT_OPERATOR="0xSignerOuOperador"
$env:POLYGON_AMOY_RPC_URL="https://rpc-amoy.polygon.technology/"
$env:TREASURY_TOKEN_CONTRACT="0xTokenUSDCouUSDTNaAmoy"
$env:TREASURY_TOKEN_SYMBOL="USDC"
$env:TREASURY_TOKEN_DECIMALS="6"
$env:TREASURY_MAX_TRANSFER="100"
$env:TREASURY_DAILY_LIMIT="1000"
npm run preflight:polygon-amoy
npm run deploy:polygon-amoy
```

## Deploy Polygon Mainnet

Use Polygon para capability settlement/payout quando fizer sentido reduzir custo de gas ou atender providers que já liquidam em Polygon. Nao mude o fluxo principal BSC sem antes adaptar signer/core para aceitar `POLYGON` ponta a ponta.

```powershell
$env:PRIVATE_KEY="0x..."
$env:CONTRACT_OWNER="0xMultisigOuOwner"
$env:CONTRACT_OPERATOR="0xSignerOuOperador"
$env:POLYGON_RPC_URL="https://polygon-rpc.com/"
$env:POLYGON_USDC_CONTRACT="0x2791Bca1f2de4661ED88A30C99A7a9449Aa84174"
$env:TREASURY_TOKEN_SYMBOL="USDC"
$env:TREASURY_TOKEN_DECIMALS="6"
$env:TREASURY_MAX_TRANSFER="100"
$env:TREASURY_DAILY_LIMIT="1000"
npm run preflight:polygon
npm run deploy:polygon
```

Depois do deploy, o script imprime:

```env
TREASURY_CONTRACT=0x...
DELEGATE_REGISTRY=0x...
CUSTODY_TRUSTED_DELEGATES=0x...
CUSTODY_TRUSTED_DELEGATE_CODE_HASH=0x...
```

Tambem grava `contracts/deployments/<network>.json` com rede, chainId, owner, operator, vault, registry, delegate e codehash. Esse arquivo fica ignorado por git porque e artefato de deploy.

Preencha `CUSTODY_TRUSTED_DELEGATES` somente com delegate auditado e validado. Nunca use placeholder como `0xContratoDelegateSeguro`.

### O que o deploy configura automaticamente

Quando `CONTRACT_OWNER` e a carteira deployer sao o mesmo endereco, o script:

- faz deploy do vault, registry e delegate;
- inicializa o delegate com `owner` e `operator`;
- configura `CONTRACT_OPERATOR` como operator do vault;
- configura guardians de `CONTRACT_GUARDIANS`;
- configura recipients de `TREASURY_ALLOWED_RECIPIENTS`;
- aplica policy do token BSC/Polygon se o token estiver configurado;
- registra o delegate no registry com `codeHash`.

Quando `CONTRACT_OWNER` for diferente do deployer, o script faz deploy e inicializa o delegate, mas para antes das chamadas owner-only. Nesse caso o owner precisa configurar token/operator/guardian/recipient/registry manualmente.

## Politica Recomendada

- `owner`: multisig ou carteira operacional separada, nunca a mesma hot wallet do payout.
- `guardian`: carteira capaz de pausar rapidamente em incidente.
- `operator`: signer/operador com limite baixo.
- `TREASURY_MAX_TRANSFER` ou `TREASURY_MAX_TRANSFER_USDT`: comece pequeno.
- `TREASURY_DAILY_LIMIT` ou `TREASURY_DAILY_LIMIT_USDT`: limite menor que o saldo total da hot wallet.
- `TREASURY_TOKEN_DECIMALS`: BSC USDT costuma usar 18; Polygon USDC/USDT usa 6. Confira o contrato antes de configurar limites.
- `CUSTODY_MODE=shadow` primeiro, depois `paper`.

## O Que Nao Fazer

- Nao coloque todos os fundos no vault antes de testar em testnet.
- Nao use delegate EIP-7702 com `execute()` generico.
- Nao permita token contract aberto.
- Nao use owner EOA sem backup/multisig para saldo alto.
- Nao configure `CUSTODY_TRUSTED_DELEGATES` com contrato sem bytecode auditado.
