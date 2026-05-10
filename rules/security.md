---
name: security
description: Secrets, gitleaks, RLS, PII, no-console-log, CSP nonce, CSRF.
paths:
  - "**/*"
---

# Security rules

## Invariantes (CI gates)

1. **Sem secrets hardcoded** (gitleaks pre-commit + pre-push + CI).
2. **Sem `console.log`** em `apps/web/src/**` (eslint rule + grep CI).
3. **PII scrubbing** em logs Go via `slog.MaskPII()` wrapper.

Detalhes em `docs/INVARIANTS.md`.

## Secrets management

- **Nunca** commitar `.env` (em `.gitignore`).
- `.env.example` documenta cada variável (sem valores reais).
- Secrets de runtime: variáveis de ambiente lidas em `services/api/internal/platform/config/`.
- **AI provider keys**: nunca client-side. API Go expõe rotas `/api/v1/apps/<slug>/ai/*` que internamente lêem keys do registry encriptado.
- **AES-256-GCM** para secrets at-rest no DB (`services/shared/crypto`).
- Rotação obrigatória: Ed25519 keystore (30 dias), ENCRYPTION_KEY (anual), ASAAS_WEBHOOK_TOKEN (90 dias).
- Runbook: `docs/runbooks/SECRETS.md` (M5).

## SQL safety

- **Sempre** parametrize queries via `pgx`. Nunca `fmt.Sprintf("SELECT ... %s", input)`.
- sqlc gera código tipado por padrão. Usar.
- Schema-per-tenant + `SET search_path` é defense-in-depth, não única barreira. Plus RLS policy onde possível em tabelas sensíveis.

## XSS prevention

- React escapa por padrão. Nunca usar `dangerouslySetInnerHTML` com input user — sanitizar primeiro.
- Sanitização HTML server-side via `bluemonday` quando precisar.
- StyleX e CSS variables não comem JS — seguro.

## CSRF

- BFF endpoints `/api/session/*` validam Origin header contra `process.env.NEXT_PUBLIC_APP_ORIGIN`.
- SameSite=Lax em cookies de auth.
- Forms POST sempre via `<form action="/api/session" method="POST">` ou fetch com `credentials: 'include'` + Origin check no BFF.

## CSP (M5+)

CSP nonce em todo response Next.js. Bloquear:

- `script-src 'self' 'nonce-<random>'`
- `style-src 'self' 'unsafe-inline'` (StyleX precisa inline)
- `connect-src 'self' <api-origin>`
- `frame-ancestors 'none'`

## Rate limiting

- Login: 5 / 15min por email + IP. Lockout 30min após 10 falhas.
- Signup: 3 / 60min por IP.
- AI assist: por workspace + plan limit (M3+).
- `chi/httprate` para HTTP global rate limit (60 req/min default).
- Detalhes em `services/shared/ratelimit`.

## Audit

- Toda mutação em auth/tenancy/RBAC emite evento em `{slug}_core.audit_logs` (futuro: `audit_logs_global` para mudanças no `public`).
- Campos obrigatórios: `actor_user_id`, `workspace_id`, `action`, `target`, `outcome`, `ip_address`, `ua_hash`, `request_id`, `payload_redacted`.
- Auditoria é **append-only** com triggers BLOCK UPDATE/DELETE/TRUNCATE.

## LGPD

- Soft-delete por padrão (`status = 'archived'`).
- Hard-delete (drop schema) só após 90d em `pending_deletion` + ausência de legal hold.
- Endpoint `GET /api/v1/me/export` retorna ZIP com NDJSON dos dados do usuário (M5+).
- Endpoint `POST /api/v1/me/delete` agenda deletion request com 30d janela de cancelamento.

## Reportar vulnerabilidade

Ver `SECURITY.md`. Email `security@compexhub.app`. Timeline: 48h ack / 5d assess / 30d fix.

## Don't

- ❌ Hardcode secrets em qualquer arquivo do repo.
- ❌ Logs com email/cpf/phone/password/token raw.
- ❌ `console.log` em produção (use telemetria).
- ❌ String concat em SQL.
- ❌ `dangerouslySetInnerHTML` sem sanitização.
- ❌ Cookies sem HttpOnly+Secure+SameSite.
- ❌ Pular gitleaks via `--no-verify`.
- ❌ Hard-delete sem janela LGPD.
