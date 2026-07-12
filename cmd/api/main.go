package main

import (
	"context"
	"log"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"payment-gateway/internal/config"
	"payment-gateway/internal/database"
	"payment-gateway/internal/email"
	"payment-gateway/internal/logger"
	"payment-gateway/internal/mobile"
	"payment-gateway/internal/rpc"
	"payment-gateway/internal/server"
	"payment-gateway/internal/workers"
)

func main() {
	logger.Configure()
	log.Println("Iniciando o ecossistema concorrente em Go...")

	cfg := config.LoadConfig()
	if err := cfg.ValidateProduction(); err != nil {
		log.Fatalf("Configuracao invalida para producao: %v", err)
	}

	db, err := database.ConnectPostgres(cfg)
	if err != nil {
		log.Fatalf("Erro fatal ao conectar no banco de dados: %v", err)
	}
	defer db.Close()

	// 1. Contexto cancelável para desligamento ordenado.
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	mailer := email.NewService(cfg)

	// 2. RPC pool (BSC). Nil-safe — workers self-disable gracefully if absent.
	var pool *rpc.Pool
	if cfg.BscRpcUrls != "" {
		pool, err = rpc.NewPool(cfg.BscRpcUrls)
		if err != nil {
			slog.Warn("RPC pool init failed, Gas Station + Auto-Sweeper will be disabled", "error", err)
		} else {
			pool.StartHealthChecks(ctx, 30*time.Second)
		}
	}

	// 3. Worker manager — includes Gas Station (Paymaster) + Auto-Sweeper.
	workerMgr := workers.NewWorkerManager(db, cfg, mailer, pool)
	workerMgr.StartAll(ctx)

	// 4. HTTP API server.
	api := server.New(cfg, db, workerMgr, mailer)

	// Wire the Paymaster service into the HTTP server for /v1/gas/* routes.
	if workerMgr.PaymasterService != nil {
		api.WithPaymaster(workerMgr.PaymasterService)
	}

	mob := mobile.New(cfg, db, workerMgr)
	httpServer := &http.Server{
		Addr:         ":" + cfg.Port,
		Handler:      mob.Wrap(api.Handler()),
		ReadTimeout:  10 * time.Second,
		WriteTimeout: 30 * time.Second,
		IdleTimeout:  60 * time.Second,
	}

	go func() {
		log.Printf("API HTTP iniciada na porta %s", cfg.Port)
		if err := httpServer.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Fatalf("Erro fatal ao subir API HTTP: %v", err)
		}
	}()

	log.Println("API e motores em background foram disparados e isolados.")

	// 5. Espera sinal de desligamento.
	stop := make(chan os.Signal, 1)
	signal.Notify(stop, os.Interrupt, syscall.SIGTERM)
	<-stop

	log.Println("Sinal de encerramento recebido. Desligando sistemas de forma limpa...")
	cancel()

	shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer shutdownCancel()
	if err := httpServer.Shutdown(shutdownCtx); err != nil {
		log.Printf("Erro ao desligar HTTP server: %v", err)
	}
	log.Println("Aplicação finalizada com 100% de segurança de dados.")
}
