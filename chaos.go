package main

import (
	"encoding/json"
	"log"
	"net/http"
	"sync/atomic"

	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/trace"
)

// FaultInjector guarda, apenas em memoria, se o pod deve simular uma falha.
//
// Uso no Tech Challenge (Fase 4): ativar a falha faz o /validate responder
// HTTP 500, o alerta de taxa de erros 5xx dispara e o self-healer executa
// "kubectl rollout restart". O pod novo nasce com a falha desligada, provando
// que a automacao mitigou o incidente sem intervencao humana.
type FaultInjector struct {
	enabled atomic.Bool
}

func (f *FaultInjector) Enabled() bool {
	return f.enabled.Load()
}

func (f *FaultInjector) Set(enabled bool) {
	f.enabled.Store(enabled)
}

// faultInjectionMiddleware devolve 500 enquanto a falha simulada estiver ativa.
func (a *App) faultInjectionMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !a.Faults.Enabled() {
			next.ServeHTTP(w, r)
			return
		}

		span := trace.SpanFromContext(r.Context())
		span.SetAttributes(attribute.Bool("togglemaster.fault_injected", true))
		span.SetStatus(codes.Error, "fault injection enabled")
		log.Printf("ERROR falha simulada ativa: respondendo 500 para %s %s", r.Method, r.URL.Path)
		http.Error(w, "Falha simulada (fault injection)", http.StatusInternalServerError)
	})
}

// chaosHandler: POST liga, DELETE desliga, GET consulta.
func (a *App) chaosHandler(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodPost:
		a.Faults.Set(true)
		log.Printf("WARN fault injection ATIVADA via /admin/chaos")
	case http.MethodDelete:
		a.Faults.Set(false)
		log.Printf("INFO fault injection desativada via /admin/chaos")
	case http.MethodGet:
	default:
		w.Header().Set("Allow", "GET, POST, DELETE")
		http.Error(w, "Método não permitido", http.StatusMethodNotAllowed)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(map[string]bool{"fault_injection": a.Faults.Enabled()}); err != nil {
		log.Printf("Erro ao serializar resposta do chaos: %v", err)
	}
}
