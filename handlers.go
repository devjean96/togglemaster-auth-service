package main

import (
	"database/sql"
	"encoding/json"
	"errors"
	"log"
	"net/http"
	"strings"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/trace"
)

var authTracer = otel.Tracer("togglemaster/auth-service")

// Estrutura para o corpo da requisição de criação de chave
type CreateKeyRequest struct {
	Name string `json:"name"`
}

// Estrutura para a resposta da criação de chave
type CreateKeyResponse struct {
	Name    string `json:"name"`
	Key     string `json:"key"` // A chave em texto plano é retornada APENAS uma vez
	Message string `json:"message"`
}

// healthHandler é um simples endpoint de verificação de saúde
func (a *App) healthHandler(w http.ResponseWriter, r *http.Request) {
	w.WriteHeader(http.StatusOK)
	if err := json.NewEncoder(w).Encode(map[string]string{"status": "ok"}); err != nil {
		log.Printf("Erro ao serializar resposta do health check: %v", err)
	}
}

// validateKeyHandler verifica se uma chave de API (enviada via Header) é válida
func (a *App) validateKeyHandler(w http.ResponseWriter, r *http.Request) {
	ctx, span := authTracer.Start(r.Context(), "auth.validate_api_key")
	defer span.End()

	// Extrai a chave do header "Authorization: Bearer <key>"
	authHeader := r.Header.Get("Authorization")
	keyString := strings.TrimPrefix(authHeader, "Bearer ")

	if keyString == "" {
		span.SetAttributes(attribute.String("auth.result", "missing_credentials"))
		a.recordValidation(ctx, false)
		http.Error(w, "Authorization header não encontrado", http.StatusUnauthorized)
		return
	}

	// Calcula o hash da chave recebida
	keyHash := hashAPIKey(keyString)

	// Verifica se o hash existe no banco de dados
	var id int
	dbCtx, dbSpan := authTracer.Start(
		ctx,
		"postgresql.validate_api_key",
		trace.WithSpanKind(trace.SpanKindClient),
		trace.WithAttributes(
			attribute.String("db.system.name", "postgresql"),
			attribute.String("db.operation.name", "SELECT"),
		),
	)
	err := a.DB.QueryRowContext(dbCtx, "SELECT id FROM api_keys WHERE key_hash = $1 AND is_active = true", keyHash).Scan(&id)
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		dbSpan.RecordError(err)
		dbSpan.SetStatus(codes.Error, "database query failed")
	}
	dbSpan.End()
	if err != nil {
		span.SetAttributes(attribute.String("auth.result", "invalid"))
		a.recordValidation(ctx, false)
		if !errors.Is(err, sql.ErrNoRows) {
			span.RecordError(err)
			span.SetStatus(codes.Error, "database query failed")
			a.recordDatabaseError(ctx, "validate")
			log.Printf("Falha ao consultar chave no banco: %v", err)
		}
		http.Error(w, "Chave de API inválida ou inativa", http.StatusUnauthorized)
		return
	}

	// Chave válida
	span.SetAttributes(attribute.String("auth.result", "valid"))
	a.recordValidation(ctx, true)
	w.WriteHeader(http.StatusOK)
	if err := json.NewEncoder(w).Encode(map[string]string{"message": "Chave válida"}); err != nil {
		log.Printf("Erro ao serializar resposta de validacao: %v", err)
	}
}

// createKeyHandler cria uma nova chave de API
func (a *App) createKeyHandler(w http.ResponseWriter, r *http.Request) {
	ctx, span := authTracer.Start(r.Context(), "auth.create_api_key")
	defer span.End()

	if r.Method != http.MethodPost {
		http.Error(w, "Método não permitido", http.StatusMethodNotAllowed)
		return
	}

	var req CreateKeyRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "Corpo da requisição inválido", http.StatusBadRequest)
		return
	}

	if req.Name == "" {
		http.Error(w, "O campo 'name' é obrigatório", http.StatusBadRequest)
		return
	}

	// Gera uma nova chave e seu hash
	newKey, err := generateAPIKey()
	if err != nil {
		span.RecordError(err)
		span.SetStatus(codes.Error, "key generation failed")
		http.Error(w, "Erro ao gerar a chave", http.StatusInternalServerError)
		return
	}
	newKeyHash := hashAPIKey(newKey)

	// Salva o hash no banco de dados
	var newID int
	dbCtx, dbSpan := authTracer.Start(
		ctx,
		"postgresql.create_api_key",
		trace.WithSpanKind(trace.SpanKindClient),
		trace.WithAttributes(
			attribute.String("db.system.name", "postgresql"),
			attribute.String("db.operation.name", "INSERT"),
		),
	)
	err = a.DB.QueryRowContext(dbCtx,
		"INSERT INTO api_keys (name, key_hash) VALUES ($1, $2) RETURNING id",
		req.Name, newKeyHash,
	).Scan(&newID)
	if err != nil {
		dbSpan.RecordError(err)
		dbSpan.SetStatus(codes.Error, "database insert failed")
	}
	dbSpan.End()

	if err != nil {
		span.RecordError(err)
		span.SetStatus(codes.Error, "database insert failed")
		a.recordDatabaseError(ctx, "create")
		log.Printf("Erro ao salvar a chave no banco: %v", err)
		http.Error(w, "Erro ao salvar a chave", http.StatusInternalServerError)
		return
	}

	log.Printf("Nova chave criada com sucesso (ID: %d, Name: %s)", newID, req.Name)
	span.SetAttributes(attribute.String("auth.result", "created"))
	a.recordKeyCreated(ctx)
	w.WriteHeader(http.StatusCreated)
	if err := json.NewEncoder(w).Encode(CreateKeyResponse{
		Name:    req.Name,
		Key:     newKey, // Retorna a chave em texto plano pela última vez
		Message: "Guarde esta chave com segurança! Você não poderá vê-la novamente.",
	}); err != nil {
		log.Printf("Erro ao serializar resposta de criacao da chave: %v", err)
	}
}

// --- Middleware ---

// masterKeyAuthMiddleware protege endpoints que só podem ser acessados com a MASTER_KEY
func (a *App) masterKeyAuthMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		authHeader := r.Header.Get("Authorization")
		keyString := strings.TrimPrefix(authHeader, "Bearer ")

		if keyString != a.MasterKey {
			http.Error(w, "Acesso não autorizado", http.StatusForbidden)
			return
		}
		// Se a chave for válida, continua para o handler principal
		next.ServeHTTP(w, r)
	})
}
