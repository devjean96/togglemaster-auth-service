package main

import (
	"context"
	"testing"

	"github.com/DATA-DOG/go-sqlmock"
)

func TestBootstrapEvaluationServiceAPIKey(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("criar mock do banco: %v", err)
	}
	defer closeMockDB(t, db, mock)

	key := "tm_key_evaluation"
	keyHash := hashAPIKey(key)

	mock.ExpectBegin()
	mock.ExpectExec("UPDATE api_keys").
		WithArgs(evaluationServiceKeyName, keyHash).
		WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectExec("INSERT INTO api_keys").
		WithArgs(evaluationServiceKeyName, keyHash).
		WillReturnResult(sqlmock.NewResult(1, 1))
	mock.ExpectCommit()

	if err := bootstrapEvaluationServiceAPIKey(context.Background(), db, key); err != nil {
		t.Fatalf("bootstrap retornou erro: %v", err)
	}
}

func TestBootstrapEvaluationServiceAPIKeyRejectsEmptyKey(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("criar mock do banco: %v", err)
	}
	defer closeMockDB(t, db, mock)

	err = bootstrapEvaluationServiceAPIKey(context.Background(), db, "  ")
	if err == nil {
		t.Fatal("esperava erro para chave vazia")
	}
}

func closeMockDB(t *testing.T, db interface{ Close() error }, mock sqlmock.Sqlmock) {
	t.Helper()
	mock.ExpectClose()
	if err := db.Close(); err != nil {
		t.Errorf("fechar mock do banco: %v", err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Errorf("expectativas do banco nao atendidas: %v", err)
	}
}
