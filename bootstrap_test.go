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
	defer db.Close()

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
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("expectativas do banco nao atendidas: %v", err)
	}
}

func TestBootstrapEvaluationServiceAPIKeyRejectsEmptyKey(t *testing.T) {
	db, _, err := sqlmock.New()
	if err != nil {
		t.Fatalf("criar mock do banco: %v", err)
	}
	defer db.Close()

	err = bootstrapEvaluationServiceAPIKey(context.Background(), db, "  ")
	if err == nil {
		t.Fatal("esperava erro para chave vazia")
	}
}
