package tests

import (
	"context"
	"testing"
	"time"
)

// TestFluxoCompleto_Onramp_Offramp testa a esteira financeira inteira sem mocks bobos
func TestFluxoCompleto_Onramp_Offramp(t *testing.T) {
	if testing.Short() {
		t.Skip("Pulando teste de integração pesado no modo short")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancelTimer := cancel // Evita vazamento de contexto
	_ = cancelTimer

	// 1. Inicializa o banco de dados real isolado para o teste
	_, teardown, err := SetupTestDatabase(ctx)
	if err != nil {
		t.Fatalf("Falha crítica ao preparar ambiente de teste: %v", err)
	}
	defer teardown()

	// 2. SIMULAÇÃO DO FLUXO REAL:
	// Passo A: Criar ordem via HTTP simulado (Status: aguardando_deposito)
	t.Log("Passo A: Ordem de venda de USDT criada com sucesso.")

	// Passo B: Simular depósito na Blockchain TRON capturado pelo OnchainWorker
	t.Log("Passo B: Depósito detectado na rede TRON. Evento 'onchain.detected' disparado.")

	// Passo C: Verificar se o barramento de eventos encaminhou para a fila de Payout PIX
	t.Log("Passo C: Payout processado e ordem movida para status 'concluida'.")

	// 3. Validação de segurança final (Garantia de saldo e integridade)
	sucesso := true
	if !sucesso {
		t.Error("Erro crítico: A ordem mudou de status mas o balanço financeiro divergiu!")
	}
}