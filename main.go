package main

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	_ "github.com/jackc/pgx/v5/stdlib"
	"github.com/joho/godotenv"
	"go.opentelemetry.io/contrib/instrumentation/net/http/otelhttp"
)

// App struct (para injeção de dependência)
type App struct {
	DB        *sql.DB
	MasterKey string
	Metrics   *AppMetrics
	Faults    FaultInjector
}

func main() {
	if err := run(); err != nil {
		log.Fatal(err)
	}
}

func run() error {
	// Carrega o .env para desenvolvimento local. Em produção, isso não fará nada.
	_ = godotenv.Load()

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	shutdownTelemetry, err := setupTelemetry(ctx)
	if err != nil {
		return fmt.Errorf("inicializar OpenTelemetry: %w", err)
	}
	defer func() {
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		if err := shutdownTelemetry(shutdownCtx); err != nil {
			log.Printf("Erro ao encerrar OpenTelemetry: %v", err)
		}
	}()

	// --- Configuração ---
	port := os.Getenv("PORT")
	if port == "" {
		port = "8001" // Porta padrão
	}

	databaseURL := os.Getenv("DATABASE_URL")
	if databaseURL == "" {
		return errors.New("DATABASE_URL deve ser definida")
	}

	masterKey := os.Getenv("MASTER_KEY")
	if masterKey == "" {
		return errors.New("MASTER_KEY deve ser definida")
	}

	// --- Conexão com o Banco ---
	db, err := connectDB(ctx, databaseURL)
	if err != nil {
		return fmt.Errorf("não foi possível conectar ao banco de dados: %w", err)
	}
	defer func() {
		if err := db.Close(); err != nil {
			log.Printf("Erro ao fechar conexao com o banco: %v", err)
		}
	}()

	app := &App{
		DB:        db,
		MasterKey: masterKey,
		Metrics:   newAppMetrics(),
	}

	// --- Rotas da API ---
	mux := http.NewServeMux()
	mux.HandleFunc("/health", app.healthHandler)

	// Endpoint público para validar uma chave.
	// faultInjectionMiddleware permite simular a falha da Fase 4 (Tech Challenge).
	mux.Handle("/validate", otelhttp.NewHandler(
		app.faultInjectionMiddleware(http.HandlerFunc(app.validateKeyHandler)),
		"GET /validate",
	))

	// Liga/desliga a falha simulada. Protegido pela MASTER_KEY e desligado
	// por padrao; o estado vive so na memoria do pod, entao um restart cura.
	mux.Handle("/admin/chaos", otelhttp.NewHandler(
		app.masterKeyAuthMiddleware(http.HandlerFunc(app.chaosHandler)),
		"/admin/chaos",
	))

	// Endpoints de "admin" para criar/gerenciar chaves
	// Eles são protegidos pelo middleware de autenticação
	mux.Handle("/admin/keys", otelhttp.NewHandler(
		app.masterKeyAuthMiddleware(http.HandlerFunc(app.createKeyHandler)),
		"POST /admin/keys",
	))

	log.Printf("Serviço de Autenticação (Go) rodando na porta %s", port)
	server := &http.Server{
		Addr:              ":" + port,
		Handler:           mux,
		ReadHeaderTimeout: 5 * time.Second,
	}
	serverErrors := make(chan error, 1)
	go func() {
		serverErrors <- server.ListenAndServe()
	}()

	select {
	case err := <-serverErrors:
		if !errors.Is(err, http.ErrServerClosed) {
			return fmt.Errorf("servidor HTTP: %w", err)
		}
	case <-ctx.Done():
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		if err := server.Shutdown(shutdownCtx); err != nil {
			return fmt.Errorf("encerrar servidor HTTP: %w", err)
		}
	}

	return nil
}

// connectDB inicializa e testa a conexão com o PostgreSQL
func connectDB(ctx context.Context, databaseURL string) (*sql.DB, error) {
	db, err := sql.Open("pgx", databaseURL)
	if err != nil {
		return nil, err
	}

	if err = db.PingContext(ctx); err != nil {
		return nil, err
	}

	log.Println("Conectado ao PostgreSQL com sucesso!")
	return db, nil
}
