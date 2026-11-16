package main

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
)

const evaluationServiceKeyName = "evaluation-service-internal"

// bootstrapEvaluationServiceAPIKey registra a chave compartilhada recebida por
// Secret. Apenas o hash SHA-256 e persistido. A operacao e idempotente e, em
// caso de rotacao, desativa as chaves anteriores com o mesmo nome.
func bootstrapEvaluationServiceAPIKey(ctx context.Context, db *sql.DB, key string) error {
	key = strings.TrimSpace(key)
	if key == "" {
		return errors.New("chave interna vazia")
	}

	keyHash := hashAPIKey(key)
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("iniciar transacao: %w", err)
	}
	defer func() {
		_ = tx.Rollback()
	}()

	if _, err := tx.ExecContext(ctx, `
		UPDATE api_keys
		SET is_active = false
		WHERE name = $1 AND key_hash <> $2 AND is_active = true
	`, evaluationServiceKeyName, keyHash); err != nil {
		return fmt.Errorf("desativar chave interna anterior: %w", err)
	}

	if _, err := tx.ExecContext(ctx, `
		INSERT INTO api_keys (name, key_hash, is_active)
		VALUES ($1, $2, true)
		ON CONFLICT (key_hash) DO UPDATE
		SET name = EXCLUDED.name, is_active = true
	`, evaluationServiceKeyName, keyHash); err != nil {
		return fmt.Errorf("registrar chave interna: %w", err)
	}

	if err := tx.Commit(); err != nil {
		return fmt.Errorf("confirmar cadastro da chave interna: %w", err)
	}

	return nil
}
