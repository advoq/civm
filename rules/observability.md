---
name: observability
description: slog estruturado, OTel, Prometheus, request_id propagation.
paths:
  - services/api/**
  - apps/web/instrumentation.ts
---

# Observability rules

## Logs estruturados

### Backend (Go)

`slog.JSONHandler` é o handler default. Nunca `fmt.Println` ou `log.Printf` em produção.

```go
slog.InfoContext(ctx, "workspace created",
    slog.String("workspace_id", ws.ID),
    slog.String("workspace_slug", ws.Slug),
    slog.String("user_id", actor.ID),
    slog.String("request_id", middleware.GetReqID(ctx)),
)
```

Atributos obrigatórios em qualquer log de operação:

- `request_id` (do middleware chi `RequestID`)
- `trace_id`, `span_id` (M5 OTel)
- `user_id` (quando autenticado)
- `workspace_slug` (quando dentro de tenant)

### Frontend (Next.js)

Sem `console.log` em produção (invariante #2). Para debugging local, usar `console.debug` que stripa automaticamente em build de produção (eslint rule).

Para erros user-facing, telemetria via Sentry/etc (M5+ ADR).

## PII scrubbing (invariante #3)

Nunca logar email/cpf/phone/password/token raw. Wrapper:

```go
slog.String("email", auth.MaskPII(email))   // -> "ad***@***.com"
slog.String("cpf", auth.MaskPII(cpf))       // -> "***.***.***-**"
slog.String("token", auth.MaskPII(token))   // -> "tk_a***"
```

## OpenTelemetry (M5)

### Bootstrap Go

```go
// services/api/internal/platform/observability/otel.go
func New(ctx context.Context, cfg Config) (shutdown func(context.Context) error, err error) {
    exporter, err := otlptracegrpc.New(ctx, otlptracegrpc.WithEndpoint(cfg.OTLPEndpoint))
    // ...
    tp := trace.NewTracerProvider(
        trace.WithBatcher(exporter),
        trace.WithResource(resource.NewWithAttributes(
            semconv.SchemaURL,
            semconv.ServiceName("compexhub-api"),
            semconv.ServiceVersion(cfg.Version),
        )),
    )
    otel.SetTracerProvider(tp)
    otel.SetTextMapPropagator(propagation.TraceContext{})
    return tp.Shutdown, nil
}
```

Middleware `otelchi` para spans HTTP automáticos. Spans manuais para operações importantes:

```go
ctx, span := tracer.Start(ctx, "auth.Login")
defer span.End()
span.SetAttributes(attribute.String("workspace_slug", slug))
```

### Frontend Next.js

```ts
// apps/web/instrumentation.ts
import { registerOTel } from '@vercel/otel';

export function register() {
  registerOTel({
    serviceName: 'compexhub-web',
    spanProcessors: ['auto'],
  });
}
```

W3C Trace Context propagação automática para fetches via `@compexhub/api-client`.

## Prometheus metrics

### Endpoint

```
GET /metrics
```

Apenas internal network. **Nunca** proxy via Nginx público.

### Métricas obrigatórias

```go
// services/api/internal/platform/observability/metrics.go
http_requests_total{method, route, status}
http_request_duration_seconds{method, route}
auth_login_total{outcome}              // success|failure|lockout|error
argon2_semaphore_acquisitions_total{operation}
argon2_semaphore_wait_seconds{operation}
db_pool_acquired_conns
db_pool_idle_conns
db_pool_total_conns
db_pool_max_conns
```

### Dashboards

`infra/grafana/provisioning/dashboards/`:

- `auth.json` — login rate, p95 latency, argon2 queue depth
- `api.json` — RPS, p50/p95/p99 latency, error rate
- `tenant.json` — workspaces ativos, provisioning duration, schemas count
- `db.json` — connection pool por schema, query duration

## Tempo (traces) + Loki (logs) + Prometheus (metrics) + Grafana

Stack `infra/`:

```yaml
# infra/docker-compose.yml (M5)
services:
  otel-collector:    # Recebe OTLP, forwards para Tempo/Prometheus/Loki
  tempo:             # Traces
  prometheus:        # Metrics scrape
  loki:              # Logs aggregation
  grafana:           # Visualização
```

Dashboards e dashboards via provisioning (Grafana Configuration as Code).

## Runbooks

Cada métrica/dashboard tem runbook em `docs/runbooks/`:

- `RUNBOOK-AUTH-INCIDENT.md` — login spike, argon2 overload
- `RUNBOOK-DB-DOWN.md` — failover, restore
- `RUNBOOK-TENANT-PROVISIONING.md` — provisioner stuck, schema drift

## Don't

- ❌ `fmt.Println` ou `log.Printf` em produção (use slog).
- ❌ `console.log` em frontend produção (invariante #2).
- ❌ Logar PII raw (invariante #3 — sempre `MaskPII`).
- ❌ Métrica sem dashboard correspondente (orfã).
- ❌ Métrica sem runbook quando virar página (alerta sem ação).
- ❌ Trace sampling agressivo (>10% off) sem ADR — perde-se debugging.
- ❌ `/metrics` exposto publicamente.
