# SSDV3 — Spec-Driven Development: Prompts Base

> **Metodologia adotada pelo compexhub.** Stack alvo: modular monolith em `services/api/internal/platform/{auth,tenancy,rbac,...}` + `services/api/internal/apps/<produto>/`. Stack atual: Go 1.26 · chi/v5 · pgx/v5 (M3+) · sqlc (M3+) · goose (M3+) · Next.js 16 · React 19 · StyleX · Zustand · Serwist · PostgreSQL 18 schema-per-tenant (Neon serverless preview + production) · Redis 8 (M5) · OTel/Tempo/Prometheus/Loki (M5). Prompts referenciam genericamente `ms-auth`/`ms-core`/etc. — substitua mentalmente por `internal/platform/<área>/`. A metodologia (PRD → SPEC → IMPL) é o que importa.

Metodologia em 3 passos: **PRD → SPEC → IMPL**

Versão revisada para o stack compexhub (referência original): Go 1.26 · chi/v5 · pgx v5 · go-redis v9 · Next.js 16 · React 19 · StyleX · FSD · PostgreSQL 18 schema-per-tenant · Redis 8.6 · PgBouncer · Nginx · Docker

Objetivo desta versão:

- preservar a fase de descoberta útil antes de decidir
- reduzir ambiguidade entre fatos do repo e propostas
- produzir PRD e SPEC mais executáveis
- melhorar a passagem do PRD para o SPEC e do SPEC para a implementação
- incorporar guardrails cognitivos que forcem Sistema 2 nas etapas críticas

## Como usar

1. Use o **Passo 1** para gerar `docs/{feature-slug}/PRD.md`
2. Use o **Passo 2** para transformar o PRD em `docs/{feature-slug}/SPEC.md`
3. Use o **Passo 2.5** quando houver risco estrutural, operacional ou de segurança
4. Use o **Passo 3** para implementar estritamente a partir do SPEC

Se um passo encontrar ambiguidade que pertence ao passo anterior, volte um passo.

## Organização dos arquivos

**Todos os artefatos SDD (PRD.md e SPEC.md) devem ser criados dentro de pastas semânticas em `docs/`**, nunca soltos na raiz do repositório.

**Convenção de nomes:**

```text
docs/{feature-slug}/
├── PRD.md
└── SPEC.md
```

- `{feature-slug}` deve ser kebab-case, curto e descritivo
- Se a feature já tem uma pasta em `docs/`, reutilize-a
- Se a feature for uma evolução incremental, reutilize a pasta existente ou crie uma subpasta semântica

## Frontmatter obrigatório no PRD.md

Toda PRD.md começa com frontmatter YAML. Esses campos alimentam o índice gerado em `docs/INDEX.md`:

```yaml
---
slug: process-events-async-worker
title: Process Events Async Worker
milestone: M5-observability
issues: [636]
---

# PRD — Process Events Async Worker
```

- `slug`: kebab-case igual ao nome da pasta.
- `title`: humano, vai aparecer no índice.
- `milestone`: identificador da milestone (M3, M4, M5...). Use `—` se ainda não associada.
- `issues`: array de números de issue do GitHub (vazio `[]` se não tem).

O status no índice é derivado da presença de arquivos: `PRD` → só PRD.md; `SPEC` → SPEC.md ou SPECv2.md também presente; `DONE` → IMPL.md presente.

Regenerar o índice: `npm run docs:index`. Validar sincronia em CI: `npm run docs:check`.

## Referência cognitiva

Quando a mudança envolver risco estrutural, operacional, de segurança, rollout, rollback, migração, cache, contrato, secret, backfill ou tenant isolation, o SPEC deve apontar explicitamente para `docs/KAHNEMAN-DISCIPLINES.md`.

O objetivo não é teorizar no documento, mas forçar cada etapa crítica a responder:

- qual viés está sendo combatido
- qual pergunta obrigatória de Sistema 2 precisa ser respondida
- qual evidência mínima autoriza avançar
- qual condição objetiva exige abortar, voltar um passo ou fazer rollback

## Princípios da versão 3

1. **Discovery antes de convergência**
   A investigação pode ser ampla, mas o documento final deve convergir para uma única direção recomendada.
2. **Reuso antes de criação**
   Antes de propor novo endpoint, tabela, evento, env var, cache key ou componente, prove que o padrão existente não atende.
3. **Separar fato de proposta**
   Todo documento deve diferenciar explicitamente:
   - `Confirmado no codebase`
   - `Confirmado na documentação oficial`
   - `Inferência / proposta`
4. **Rastreabilidade obrigatória**
   Cada requisito do PRD deve aparecer no SPEC e cada bloco da implementação deve apontar para itens do SPEC.
5. **Sem criatividade estrutural no Passo 3**
   Se a implementação exigir decisão nova, a decisão volta para o SPEC antes de virar código.
6. **Sistema 2 explícito nas etapas críticas**
   Todo SPEC deve apontar, nos passos com risco estrutural, operacional ou de segurança, qual disciplina de `docs/KAHNEMAN-DISCIPLINES.md` está sendo usada para reduzir viés, quais evidências mínimas são exigidas e qual condição objetiva dispara abortar, voltar um passo ou rollback.
7. **Passos críticos devem levar ao documento de disciplina**
   Nenhum item crítico do SPEC fica autocontido só em execução; ele precisa apontar para `docs/KAHNEMAN-DISCIPLINES.md` e registrar como a disciplina afeta a decisão local.

---

## PASSO 1 — Geração do PRD.md

### Prompt

Preciso gerar o PRD técnico para a seguinte mudança:

**[DESCREVA A FEATURE/MUDANÇA EM 1-2 FRASES]**

Objetivo:

- [qual resultado de negócio/técnico deve existir ao final]

Camada(s) envolvida(s):

- [ ] Backend — serviço(s):
- [ ] Frontend
- [ ] Infra
- [ ] Migração
- [ ] Eventos/Redis
- [ ] OpenAPI / SDK types
- [ ] Documentação / ADR

### Processo obrigatório

Antes de escrever o PRD final, siga estas fases:

#### Fase 1 — Discovery

- Levante o contexto real no codebase
- Identifique o que já existe e pode ser reutilizado
- Liste opções de implementação viáveis
- Levante edge cases, riscos, dependências e impactos cross-service

#### Fase 2 — Convergência

- Escolha uma opção principal
- Explique por que ela é a recomendada no contexto do compexhub
- Liste alternativas descartadas e por quê
- Registre lacunas de contexto que permaneceram abertas

#### Fase 3 — PRD final

- Escreva o `PRD.md` refletindo apenas a opção recomendada
- Não escreva um PRD com múltiplas arquiteturas concorrentes
- Se houver incerteza real, registre-a em riscos, dependências ou fora de escopo

### Pesquisa obrigatória antes de gerar o PRD

#### 1. Codebase compexhub — contexto interno

- Leia `.claude/rules/` (backend.md, frontend.md, security.md, testing.md, infra.md, coding.md)
- Leia `CLAUDE.md` (root) para topologia de serviços, multi-tenancy flow, auth modes, env vars
- Identifique o(s) serviço(s) alvo em `services/ms-{name}/` e leia `cmd/api/main.go`, routes, handlers, services, repository e models existentes
- Mapeie o schema PostgreSQL atual: migrações em `services/ms-{name}/migrations/` e schema tenant vs. `public`
- Identifique tabelas, índices, constraints e relações que tocam nessa feature
- Verifique Redis keys, Pub/Sub channels, locks, cache patterns e invalidação relacionados
- Leia `docs/config-reference.json` para env vars existentes e categorias disponíveis
- Leia `docs/events-catalog.json` para eventos Redis Pub/Sub já catalogados
- Leia `docs/KAHNEMAN-DISCIPLINES.md` quando a mudança envolver risco estrutural, operacional ou de segurança
- Verifique ADRs existentes em `docs/decisions/` que sejam relevantes
- Se frontend: leia `web/CLAUDE.md`, `web/src/fsd/`, `web/src/app/`, `shared/ui/`, `shared/tokens/`
- Se tocar em auth/permissões: verifique o chain `TenantResolver → Auth → Permission` e permissões existentes em `ms-auth`
- Identifique explicitamente:
  - o que já existe e pode ser reutilizado
  - o que precisa ser estendido
  - o que realmente precisa ser criado do zero

#### 2. Documentação oficial e compatibilidade

Pesquise e valide contra a documentação oficial das tecnologias realmente envolvidas na mudança:

**Backend:**

- Go 1.26
- chi/v5
- pgx v5
- go-redis v9
- goose
- testcontainers-go
- Prometheus client_golang

**Frontend:**

- Next.js 16 App Router
- React 19
- StyleX
- TanStack Query v5
- Zustand
- React Hook Form + Zod
- Playwright

**Infra:**

- PostgreSQL 18
- Redis 8.6
- PgBouncer
- Nginx
- Docker multi-stage builds

#### 3. Padrões de mercado e edge cases

- Pesquise implementações de referência em projetos open-source de escala similar
- Identifique edge cases comuns para esse tipo de feature em contexto multi-tenant
- Verifique padrões OWASP relevantes
- Para features com dados pessoais: verifique requisitos LGPD
- Avalie trade-offs de consistência vs. disponibilidade

### Regras de qualidade do PRD

- Não invente arquitetura nova se o codebase já tiver um padrão equivalente
- Diferencie explicitamente:
  - **Confirmado no codebase**
  - **Confirmado na documentação oficial**
  - **Inferência / proposta**
- Se faltar contexto no repo, declare a lacuna em vez de assumir como fato
- Prefira reaproveitar tabelas, env vars, canais Redis, middlewares, componentes e contratos existentes
- Não proponha novos endpoints, tabelas, eventos ou env vars sem justificar por que os existentes não atendem
- Aponte breaking changes, estratégia de rollout, rollback e backfill quando aplicável
- Liste os documentos que precisarão ser atualizados no mesmo commit quando houver impacto estrutural
- Se a mudança tiver risco alto, antecipe no PRD quais etapas provavelmente exigirão disciplina explícita de `docs/KAHNEMAN-DISCIPLINES.md` no SPEC
- Mantenha o PRD específico e operacional; evite texto genérico

### Saída esperada

Gere o arquivo `docs/{feature-slug}/PRD.md` com **EXATAMENTE** esta estrutura:

#### Resumo

- O que é, por que existe, qual problema resolve
- Valor de negócio para escritórios de advocacia no contexto do compexhub

#### Contexto técnico

- Serviço(s) envolvidos e papel de cada um na topologia do compexhub
- Estado atual: tabelas, endpoints, componentes, caches e fluxos já existentes que serão reutilizados ou estendidos
- Tenant scope: tenant-scoped (`{slug}_{service}`) ou global (`public`)
- Dependências entre serviços (HTTP interno, Redis Pub/Sub, cache)
- O que está **confirmado no codebase**
- O que está **confirmado na documentação oficial**
- O que está **sendo proposto**

#### Opção recomendada

- Solução escolhida
- Motivo da escolha
- Alternativas descartadas
- Trade-offs aceitos

#### Requisitos funcionais

Para cada requisito:

- **RF-N**: descrição objetiva sem ambiguidade
- **Critério de aceite**: condição verificável
- **Tenant isolation**: como esse requisito respeita o isolamento, se aplicável

#### Requisitos não-funcionais

- **Performance**: latência esperada (p50, p99), throughput, tamanho de payload
- **Segurança**: autenticação, autorização, rate limiting, validação de input
- **Observabilidade**: métricas Prometheus, structured logs, health checks
- **Escalabilidade**: comportamento com N tenants, N réplicas, limites de pool
- **LGPD**: dados pessoais envolvidos, masking, retenção, anonimização
- **Resiliência**: comportamento com Redis down, upstream lento, falhas parciais

#### Fluxos

**Happy path**

- Passo a passo numerado
- Qual componente/serviço executa cada passo
- Qual protocolo é usado (HTTP, Redis Pub/Sub, PostgreSQL tx)
- Como o tenant slug flui (Host header → X-Tenant-Slug → WithSchema)

**Fluxos alternativos**

- Variações válidas do happy path

**Fluxos de erro**

Para cada erro:

- Condição de trigger
- HTTP status code / mensagem ao cliente
- Log level e campos contextuais
- Impacto na consistência dos dados

#### Modelo de dados

**Tabelas novas**

Para cada tabela, incluir DDL completo:

```sql
CREATE TABLE schema.table_name (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    -- ...
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX idx_table_column ON schema.table_name (column);
-- Justificativa: ...
```

**Alterações em tabelas existentes**

- `ALTER` exato
- Justificativa
- Impacto em dados existentes
- Necessidade de backfill, se houver

**Schema scope**

- `{slug}_{service}` para dados tenant-scoped
- `public.` para dados globais

#### API / Interfaces

**Endpoints novos ou modificados**

| Campo            | Valor                                        |
| ---------------- | -------------------------------------------- |
| Method           | `GET` / `POST` / `PATCH` / `DELETE`          |
| Path             | `/v1/...` ou `/internal/v1/...`              |
| Auth             | JWT + Permission / Token / Internal          |
| Rate limit       | se aplicável                                 |
| Middleware chain | TenantResolver → Auth → Permission → Handler |
| Idempotência     | sim/não e por quê                            |

**Request**

```json
{
  "field": "type — validação"
}
```

**Response (2xx)**

```json
{
  "field": "type"
}
```

**Erros**

| Status | Condição           | Body                     |
| ------ | ------------------ | ------------------------ |
| 400    | Validação falha    | `{"error":"mensagem"}`   |
| 403    | Sem permissão      | `{"error":"forbidden"}`  |
| 404    | Recurso não existe | `{"error":"not found"}`  |
| 409    | Conflito           | `{"error":"conflict"}`   |
| 429    | Rate limit         | headers e body esperados |

**Impacto em OpenAPI / SDK / BFF**

- Schemas novos ou alterados
- Tipos gerados afetados
- Rotas proxy/BFF afetadas
- Hooks / invalidation / queries afetadas no frontend

**Eventos Redis Pub/Sub** (se aplicável)

| Channel (env var) | Default             | Publisher | Consumer | Payload              |
| ----------------- | ------------------- | --------- | -------- | -------------------- |
| `EVENT_CHANNEL`   | `domain.event_name` | ms-X      | ms-Y     | `EventEnvelope{...}` |

#### Dependências e riscos

- Pré-requisitos
- Riscos técnicos com mitigação concreta
- Impacto em serviços existentes
- Breaking changes, se houver
- Estratégia de rollout
- Estratégia de rollback
- Hipóteses que precisarão de disciplina explícita no SPEC, quando aplicável

#### Estratégia de implementação

- Ordem recomendada das fatias de implementação
- Dependências entre fatias
- O que pode ser validado cedo
- O que exige migração, backfill ou rollout coordenado

#### Fora de escopo

- O que explicitamente NÃO faz parte desta implementação
- Motivo de exclusão

---

## PASSO 2 — Geração do SPEC.md (a partir do PRD)

> **Leia o `docs/{feature-slug}/PRD.md` e produza um `docs/{feature-slug}/SPEC.md` cirúrgico para implementação.**
> O SPEC.md não replica o PRD; ele fecha decisões, remove ambiguidade e traduz requisitos em mudanças exatas no repo.

### Objetivo do Passo 2

- transformar requisitos em tarefas de código com ordem e dependências
- resolver ambiguidades do PRD antes do código
- explicitar impactos em contrato, dados, docs, testes e rollout
- ligar etapas críticas às disciplinas de `docs/KAHNEMAN-DISCIPLINES.md`

### Prompt

Leia `docs/{feature-slug}/PRD.md` e gere `docs/{feature-slug}/SPEC.md` com decisões fechadas, rastreabilidade por requisito e instruções implementáveis sem interpretação.

### Regras

1. Só inclua o que será realmente implementado agora
2. Cada arquivo listado deve ter caminho completo a partir da raiz do repo
3. Cada mudança deve explicar **o que muda**, **como muda** e **por que muda**
4. Referências a código existente devem usar nome exato de função, struct, tipo ou interface
5. Ordem dos itens = ordem de implementação
6. Se o PRD estiver ambíguo, resolva aqui com uma decisão explícita e justificativa
7. Toda query SQL deve usar placeholders posicionais (`$1`, `$2`)
8. Todo handler deve validar input antes de tocar no service layer
9. Todo endpoint tenant-scoped deve passar por `TenantResolver → Auth → Permission` quando aplicável
10. Não deixe pseudocódigo estrutural em tipos, handlers, queries ou contratos
11. Todo requisito funcional do PRD deve ser rastreado por ID no SPEC
12. Toda mudança estrutural deve dizer quais documentos serão atualizados no mesmo commit
13. Em etapas com risco estrutural, operacional, de segurança, rollout, rollback, migração, tenant isolation, auth, cache, contrato, secret, retry ou backfill, o SPEC deve apontar explicitamente a disciplina correspondente em `docs/KAHNEMAN-DISCIPLINES.md`
14. Nenhum passo crítico pode ficar só com instrução operacional; ele deve registrar também pergunta obrigatória, evidência mínima e abort trigger
15. Em qualquer mudança com transação, onboarding, saga, backfill ou múltiplos writes, o SPEC deve declarar explicitamente a fronteira de atomicidade: o que fica atômico nesta issue e o que continua fora dessa garantia
16. Toda evidência mínima de etapa crítica deve dizer como será produzida no repo de forma executável, observável e reproduzível (`go test`, `yarn test`, query SQL, diff, log esperado, validação manual obrigatória documentada). Não vale evidência implícita, presumida ou sem caminho de execução descrito
17. Em qualquer mudança com migration, backfill, rollout ou risco de perda de dados, o SPEC deve definir separadamente:
    - rollback de aplicação
    - rollback de migration
    - rollback de dados
      e dizer explicitamente o que é permitido, proibido ou `forward-only` por ambiente

### Guardrail cognitivo obrigatório

Em qualquer ITEM do SPEC que envolva migração, auth, tenant isolation, rollout, rollback, cache, contrato, secret, retry, backfill ou risco de indisponibilidade, incluir um bloco `Disciplina Kahneman` com:

- **Disciplina**: nome exato da disciplina em `docs/KAHNEMAN-DISCIPLINES.md`
- **Link**: caminho do documento e, quando possível, âncora da seção correspondente
- **Pergunta obrigatória**: pergunta de Sistema 2 que precisa ser respondida antes de avançar
- **Evidência mínima**: métrica, teste, log, diff, output ou validação objetiva exigida
- **Abort trigger**: condição objetiva que impede avanço ou exige rollback

Nenhum passo crítico pode ficar apenas com instrução operacional.

Regras adicionais para etapas críticas:

- A evidência mínima deve ser reproduzível no estado real do repo; se depender de estado anterior à migration atual, o SPEC deve descrever o harness, fixture, seed ou teste específico para produzir esse estado
- Se houver rollback, o SPEC deve dizer explicitamente se ele é de aplicação, de migration ou de dados
- Se algum rollback não for seguro em ambiente compartilhado, o SPEC deve registrar isso como política `forward-only` explícita, com abort trigger correspondente

### Saída esperada

#### Escopo fechado desta implementação

- O que entra agora
- O que fica explicitamente fora agora
- Dependências já assumidas como prontas

#### Matriz de rastreabilidade PRD → SPEC

| PRD  | Implementação no SPEC  |
| ---- | ---------------------- |
| RF-1 | ITEM-3, ITEM-4, ITEM-7 |

#### Decisões técnicas

Decisões tomadas que não estavam explícitas no PRD:

| #    | Decisão | Justificativa |
| ---- | ------- | ------------- |
| DT-1 | ...     | ...           |

#### Fronteira de atomicidade e política de rollback

- **Fronteira de atomicidade desta implementação**:
  - o que esta issue garante atomicamente
  - o que continua fora da atomicidade
  - quais estados parciais continuam aceitos nesta fase
- **Política de rollback**:
  - rollback de app
  - rollback de migration
  - rollback de dados
  - o que é proibido em `staging` / `production`
  - se a migration é `forward-only`

#### Mapa Kahneman por etapa crítica

Para cada etapa crítica da implementação, rollout, validação ou rollback, preencher:

| Etapa / ITEM | Disciplina Kahneman | Link                           | Pergunta obrigatória | Evidência mínima | Abort trigger |
| ------------ | ------------------- | ------------------------------ | -------------------- | ---------------- | ------------- |
| ITEM-3       | ...                 | `docs/KAHNEMAN-DISCIPLINES.md` | ...                  | ...              | ...           |

#### Checklist de segurança (pré-implementação)

- [ ] Tenant isolation: toda query roda dentro de tx com `WithSchema(ctx, tx, "{slug}_{service}")`
- [ ] SQL injection: zero concatenação de input em SQL, apenas placeholders
- [ ] Auth: endpoint tem middleware correto
- [ ] Permissões: ações protegidas verificam `permission.RequirePermission("scope.action")`
- [ ] Rate limiting: endpoints públicos têm `middleware.RateLimit()` aplicado
- [ ] Input validation: todos os campos do request body são validados no handler
- [ ] PII: dados pessoais não são logados; masking aplicado onde necessário
- [ ] Secrets: nenhuma credencial hardcoded
- [ ] Error messages: erros internos não vazam detalhes de implementação

#### Migrações SQL

Para cada migração, na ordem de execução:

**Arquivo:** `services/ms-{name}/migrations/NNN_description.sql`

```sql
-- +goose Up
-- DDL exato

-- +goose Down
-- Rollback exato
```

- **Schema scope:** `{slug}_{service}` ou `public`
- **Dependências:** migrações anteriores necessárias
- **Backfill:** descrever se existe e em qual ordem roda
- **Disciplina Kahneman** quando a migração for crítica:
  - **Disciplina**:
  - **Link**:
  - **Pergunta obrigatória**:
  - **Evidência mínima**:
  - **Abort trigger**:

#### Arquivos a CRIAR

Para cada arquivo novo:

**`caminho/completo/desde/raiz/arquivo.ext`**

- **Propósito**: uma frase
- **Requisitos cobertos**: `RF-N`, `DT-N`
- **Structs/Types/Interfaces**: assinatura exata
- **Funções**: assinatura exata + lógica resumida em passos
- **Dependências internas**: imports do projeto
- **Dependências externas**: libs de terceiros
- **Padrão de referência**: arquivo existente no repo
- **Testes requeridos**: arquivo de teste e cenários mínimos
- **Disciplina Kahneman** se o arquivo suportar etapa crítica:
  - **Disciplina**:
  - **Link**:
  - **Pergunta obrigatória**:
  - **Evidência mínima**:
  - **Abort trigger**:

#### Arquivos a MODIFICAR

Para cada arquivo existente:

**`caminho/completo/desde/raiz/arquivo.ext`**

- **O que muda**: descrição cirúrgica
- **Requisitos cobertos**: `RF-N`, `DT-N`
- **Função/bloco afetado**: nome exato
- **Antes**: trecho ou shape atual relevante
- **Depois**: shape novo esperado
- **Por quê**: vínculo ao PRD
- **Impacto**: quebra interface? exige ajuste em callers? afeta docs? afeta SDK?
- **Testes requeridos**: quais cenários precisam ser cobertos
- **Disciplina Kahneman** se a mudança for crítica:
  - **Disciplina**:
  - **Link**:
  - **Pergunta obrigatória**:
  - **Evidência mínima**:
  - **Abort trigger**:

#### Arquivos a DELETAR (se houver)

| Arquivo        | Motivo                       |
| -------------- | ---------------------------- |
| `path/to/file` | substituído por X / removido |

#### Observabilidade

**Métricas Prometheus** (se aplicável)

- nome exato da métrica
- tipo (`CounterVec`, `HistogramVec`, etc.)
- labels
- onde registrar
- quais fluxos incrementam ou observam

**Logs estruturados**

| Evento           | Level | Campos                              |
| ---------------- | ----- | ----------------------------------- |
| Resource created | Info  | `tenant`, `resource_id`, `actor_id` |

#### Contratos e documentação viva

Preencha explicitamente:

| Documento                             | Atualização necessária | Motivo                           |
| ------------------------------------- | ---------------------- | -------------------------------- |
| `docs/openapi/ms-{name}.openapi.yaml` | Criar / Alterar / N/A  | contrato mudou?                  |
| `web/src/fsd/shared/api/sdk.types.ts` | Regenerar / N/A        | schema mudou?                    |
| `docs/config-reference.json`          | Criar / Alterar / N/A  | env var nova?                    |
| `docs/events-catalog.json`            | Criar / Alterar / N/A  | evento novo?                     |
| `.env.example`                        | Criar / Alterar / N/A  | configuração nova?               |
| `CLAUDE.md`                           | Alterar / N/A          | padrão estrutural mudou?         |
| `.claude/rules/*.md`                  | Alterar / N/A          | convenção nova?                  |
| `web/CLAUDE.md`                       | Alterar / N/A          | frontend pattern novo?           |
| `docs/decisions/ADR-NNN-*.md`         | Criar / N/A            | decisão arquitetural relevante?  |
| `docs/KAHNEMAN-DISCIPLINES.md`        | Alterar / N/A          | nova disciplina, link ou anchor? |

#### Ordem de implementação

Lista numerada, verificável e sem gaps:

1. Migrações
2. Models / types / validation
3. Repository / data access
4. Service / business rules
5. Handlers / routes / middleware
6. OpenAPI / SDK / BFF
7. Frontend integration
8. Métricas / logs / eventos
9. Testes unitários
10. Testes de integração
11. Documentação viva

#### Plano de testes

**Backend**

- unitários: casos
- integração: casos
- tenant isolation: casos
- concorrência / atomicidade: casos

**Frontend**

- hooks / state: casos
- componentes: casos
- fluxos de página: casos

**Manuais**

- curl/httpie ou fluxo UI mínimo
- cenários de erro
- evidências objetivas exigidas pelas etapas críticas do mapa Kahneman

#### Checklist de validação

**Backend**

- [ ] `gofmt -w ./...`
- [ ] `golangci-lint run -c ../../.golangci.yml ./...`
- [ ] `go test ./... -race -count=1`
- [ ] `go test ./... -tags=integration -race -count=1` se aplicável

**Frontend**

- [ ] `yarn lint`
- [ ] `yarn format:check`
- [ ] `yarn typecheck`
- [ ] `yarn test`

**Docs**

- [ ] `yarn docs:merge`
- [ ] `yarn docs:types`
- [ ] `yarn docs:validate`

**Gates cognitivos**

- [ ] Cada etapa crítica aponta para `docs/KAHNEMAN-DISCIPLINES.md`
- [ ] Cada etapa crítica registra pergunta obrigatória, evidência mínima e abort trigger
- [ ] Não há linguagem vaga em pontos críticos sem critério observável

---

## PASSO 2.5 — Auditoria do SPEC (opcional por risco)

> **Use este passo quando a implementação tiver risco estrutural, operacional ou de segurança.**
> Ele existe para reduzir ambiguidade antes do código, não para burocratizar issues pequenas.

### Quando usar

Use o Passo 2.5 quando a mudança envolver um ou mais destes pontos:

- auth, sessão, permissões ou secrets
- tenant isolation ou acesso cross-tenant
- migração de dados, backfill ou rollback delicado
- contratos OpenAPI, SDK, BFF ou integração entre serviços
- Redis, cache, filas, locks ou invalidação
- rollout coordenado, restart ordenado ou janela operacional
- risco alto de indisponibilidade, perda de dados ou drift de configuração

### Quando pode pular

Pode pular quando a mudança for pequena, local e sem risco estrutural relevante:

- poucos arquivos
- sem migração
- sem impacto em auth, tenant isolation, contratos ou infra
- sem necessidade de rollout especial

### Prompt

Revise `docs/{feature-slug}/SPEC.md` como auditoria pré-implementação.

Quero uma revisão de lacunas com foco em:

- ambiguidades técnicas ainda não resolvidas
- fronteira de atomicidade implícita, ambígua ou incompatível com o código real
- riscos operacionais de rollout, restart e rollback
- evidência mínima que não tenha caminho executável claro no repo
- rollback descrito de forma genérica sem separar app, migration e dados
- uso de `Down` tecnicamente existente, mas operacionalmente inseguro em ambiente compartilhado
- dependências não mapeadas entre serviços, env vars, docs e automação
- gaps de segurança, autorização, isolamento de tenant e consistência de dados
- inconsistências entre requisitos, decisões técnicas, arquivos listados, testes e validação final
- qualquer item do SPEC que ainda exija interpretação durante a implementação
- ausência de disciplina cognitiva explícita nas etapas críticas
- passos críticos sem pergunta obrigatória, evidência mínima ou abort trigger
- uso de linguagem vaga (`validar`, `garantir`, `confirmar`, `se necessário`) sem critério observável
- gaps entre etapas críticas do SPEC e as disciplinas documentadas em `docs/KAHNEMAN-DISCIPLINES.md`

### Formato da resposta

1. Liste os findings primeiro, ordenados por severidade
2. Para cada finding, cite a seção exata do SPEC afetada
3. Depois liste `Open questions`
4. Depois diga se o SPEC está pronto para implementação
5. Feche com `go` ou `no-go`

### Regra de saída

- Se houver finding que exija decisão nova, volte ao **Passo 2**
- Se houver etapa crítica sem link para disciplina Kahneman aplicável, sem evidência mínima ou sem abort trigger, o resultado deve ser `no-go`
- Se a auditoria resultar em `go`, siga para o **Passo 3**

---

## PASSO 3 — Implementação (a partir do SPEC)

> **Leia o `docs/{feature-slug}/SPEC.md` e execute-o passo a passo.**
> O Passo 3 não fecha lacunas arquiteturais; ele implementa o que já foi decidido.

### Prompt

Implemente a feature descrita em `docs/{feature-slug}/SPEC.md`.

### Regras de execução

1. Siga a ordem de implementação do SPEC
2. Use os trechos, assinaturas e contratos do SPEC como base
3. Não adicione funcionalidade fora do escopo fechado
4. Se encontrar um gap, volte ao Passo 2 antes de continuar
5. Toda alteração em contrato deve ser refletida em docs e types no mesmo ciclo
6. Toda alteração em dado, auth, tenant isolation ou cache deve ser validada com teste
7. Não refatore código adjacente sem necessidade funcional
8. Em qualquer item crítico, execute também o bloco `Disciplina Kahneman` antes de avançar para a próxima fatia

### Ritual de execução por fatia

Para cada fatia do SPEC:

1. implementar apenas o item atual
2. validar compilação
3. validar testes relacionados
4. validar a pergunta obrigatória, a evidência mínima e o abort trigger quando o item tiver disciplina Kahneman
5. comparar com o SPEC
6. só então avançar

### Checklist durante a implementação

A cada camada concluída:

- [ ] Código compila sem erros
- [ ] Lint passa
- [ ] Testes existentes continuam passando
- [ ] Novos testes da fatia foram adicionados
- [ ] Tenant isolation mantida
- [ ] Contratos atualizados quando necessário
- [ ] Docs atualizadas quando o item exige
- [ ] Etapas críticas continuam coerentes com `docs/KAHNEMAN-DISCIPLINES.md`

### Quando voltar ao SPEC

- Descobriu necessidade de índice não previsto
- Handler precisa de campo extra
- Edge case não coberto apareceu
- Ordem de implementação não fecha
- Mudou shape de resposta ou contrato OpenAPI
- Surgiu necessidade de rollout, backfill ou rollback não descritos
- A etapa crítica exige uma decisão que o mapa Kahneman do SPEC ainda não fechou

> **Regra absoluta:** se o código precisar decidir algo que o SPEC não decidiu, a implementação deve parar e o SPEC deve ser atualizado primeiro.

### Validação final

Ao terminar toda a implementação, execute o checklist do SPEC:

**Backend**

```bash
gofmt -w ./...
cd services/ms-{name} && golangci-lint run -c ../../.golangci.yml ./...
go test ./... -race -count=1
go test ./... -tags=integration -race -count=1
```

**Frontend**

```bash
cd web
yarn lint
yarn format:check
yarn typecheck
yarn test
```

**Docs**

```bash
yarn docs:merge
yarn docs:types
yarn docs:validate
```

---

## Critérios de saída entre passos

### PRD → SPEC

Só avance se:

- houver uma opção recomendada clara
- os requisitos funcionais estiverem fechados
- os riscos estruturais estiverem explícitos
- o fora de escopo estiver definido

### SPEC → Implementação

Só avance se:

- cada requisito do PRD estiver rastreado
- a ordem de implementação estiver fechada
- arquivos a criar/modificar estiverem explícitos
- plano de testes e docs estiverem definidos
- etapas críticas estiverem mapeadas para `docs/KAHNEMAN-DISCIPLINES.md`
- cada etapa crítica tiver pergunta obrigatória, evidência mínima e abort trigger
- se o risco for alto, o Passo 2.5 tiver resultado em `go`

### Implementação → Commit

Só avance se:

- código, testes e docs estiverem consistentes com o SPEC
- validações finais tiverem sido executadas
- não houver drift entre contrato, types, handlers e frontend
- não houver drift entre o que foi implementado e os guardrails cognitivos descritos no SPEC

---

## Referência rápida — Stack compexhub

| Camada          | Tecnologia                            | Versão           |
| --------------- | ------------------------------------- | ---------------- |
| Frontend        | Next.js (App Router)                  | 16               |
| UI              | React                                 | 19               |
| Styling         | StyleX                                | latest           |
| State (server)  | TanStack Query                        | v5               |
| State (client)  | Zustand                               | latest           |
| Forms           | React Hook Form + Zod                 | latest           |
| Types           | TypeScript                            | 5.9              |
| Backend         | Go                                    | 1.26             |
| Router          | chi/v5                                | v5               |
| Database driver | pgx                                   | v5               |
| Cache/Pub-Sub   | go-redis                              | v9               |
| Migrations      | goose (SQL only)                      | latest           |
| Metrics         | Prometheus client_golang              | latest           |
| Logging         | slog                                  | stdlib           |
| Database        | PostgreSQL                            | 18               |
| Cache           | Redis                                 | 8.6              |
| Connection pool | PgBouncer                             | transaction mode |
| Proxy           | Nginx                                 | 1.27             |
| Containers      | Docker                                | latest           |
| Testing (Go)    | testing + testify + testcontainers-go | —                |
| Testing (TS)    | Vitest + Testing Library              | —                |
| Testing (E2E)   | Playwright                            | —                |

---

## Regra de ouro

> O **PRD** decide o que e por quê.
> O **SPEC** fecha como, onde, em que ordem e com quais guardrails.
> A **implementação** executa sem reinventar a decisão.

## Quando iterar

- Se o Passo 3 achar um gap real, volte ao Passo 2
- Se o Passo 2 achar ambiguidade insolúvel, volte ao Passo 1
- Nunca resolva um gap estrutural só no código
- Nunca crie `PRD.md` ou `SPEC.md` fora de `docs/{feature-slug}/`
