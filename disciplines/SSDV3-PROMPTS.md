# SSDV3 — Spec-Driven Development: Prompts Base

> **Template de metodologia portátil mantido pelo `civm`.** A metodologia (PRD → SPEC → IMPL) é o que importa e é independente de stack. O `civm` em si é um repo **Go de infra** (Go 1.26 stdlib-first, `cmd/civmctl/` + `internal/**`, sem frontend e sem multi-tenant) — para mudanças no próprio `civm`, leia `.github/workflows/ci.yml`, `rules/*.md`, `AGENTS.md` e os packages relevantes em `internal/**`.
>
> Os prompts abaixo trazem, para ilustrar, um **stack de exemplo de produto web modular** (modular monolith Go + Next.js + PostgreSQL schema-per-tenant + Redis). Esse stack é **genérico/ilustrativo** — substitua mentalmente pelos serviços, contratos e camadas do seu repo. Referências a `ms-auth`/`ms-core`/`services/...`/frontend são placeholders do exemplo, não componentes do `civm`.

Metodologia em 3 passos: **PRD → SPEC → IMPL**

Stack de exemplo usado nos prompts (ilustrativo): Go · chi/v5 · pgx v5 · go-redis v9 · Next.js · React · PostgreSQL schema-per-tenant · Redis · Docker. Troque pelo stack real do seu repo.

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
├── SPEC.md
└── SPECv2.md  # opcional, criado somente quando o Passo 2.5 der no-go
```

- `{feature-slug}` deve ser kebab-case, curto e descritivo
- `SPEC.md` é a primeira versão gerada pelo Passo 2 e deve ser preservada como baseline histórico
- `SPECv2.md` é a versão melhorada criada pelo Passo 2 quando a auditoria do Passo 2.5 resultar em `no-go`
- Não sobrescreva `SPEC.md` para incorporar correções de auditoria; gere `SPECv2.md`, salvo pedido explícito do usuário para edição in-place
- Se `SPECv2.md` já existir e uma nova auditoria ainda der `no-go`, atualize `SPECv2.md` in-place, salvo pedido explícito do usuário para criar `SPECv3.md`
- Se a feature já tem uma pasta em `docs/`, reutilize-a
- Se a feature for uma evolução incremental, reutilize a pasta existente ou crie uma subpasta semântica

## Frontmatter obrigatório no PRD.md

Toda PRD.md começa com frontmatter YAML. Esses campos alimentam o índice gerado em `docs/INDEX.md`:

```yaml
---
slug: 613-tenant-scoped-outbox
title: Tenant-scoped outbox com UNIQUE(event_id)
milestone: M22
issues: [613]
---
# PRD — Issue #613 — Outbox tenant-scoped
```

- `slug`: igual ao nome da pasta (`<issue>-<descricao>` para issues, ou `<descricao>` para mudanças sem issue).
- `title`: humano, vai aparecer no índice.
- `milestone`: identificador (M14, M21, M22...). Use `—` se ainda não associada.
- `issues`: array de números de issue do GitHub (vazio `[]` se não tem).

O status no índice é derivado da presença de arquivos: `PRD` → só PRD.md; `SPEC` → SPEC.md ou SPECv2.md também presente; `DONE` → IMPL.md presente.

Regenerar o índice: `yarn docs:index` (ou `npm run docs:index`). Validar sincronia em CI: `yarn docs:check`.

## Referência cognitiva

Quando a mudança envolver risco estrutural, operacional, de segurança, rollout, rollback, migração, cache, contrato, secret, backfill ou tenant isolation, o SPEC deve apontar explicitamente para `disciplines/KAHNEMAN-DISCIPLINES.md`.

O objetivo não é teorizar no documento, mas forçar cada etapa crítica a responder:

- qual viés está sendo combatido
- qual pergunta obrigatória de Sistema 2 precisa ser respondida
- qual evidência mínima autoriza avançar
- qual condição objetiva exige abortar, voltar um passo ou fazer rollback

## Política Day-0 (produto sem produção viva — opcional)

> Aplica-se a um produto-exemplo que ainda **não** tem produção viva com dados legados obrigatórios. Não se aplica ao `civm`, que já roda em produção (runner self-hosted) — para o `civm`, qualquer mudança em comportamento operacional segue rollback trigger numérico e teste, não a política Day-0.

Quando o produto ainda não possui produção viva com dados legados obrigatórios, toda mudança deve ser especificada e implementada como solução principal e única, no formato correto final para Day-0.

Por padrão, é proibido criar workaround, shim, dual-reader, dual-write, camada de compatibilidade com formato antigo, backfill para produção inexistente, migration incremental corretiva desnecessária ou código morto.

Exceções só são permitidas quando houver requisito explícito e documentado para manter duas versões, integração externa real, dado persistido que não possa ser resetado, ou rollout coordenado aprovado. A exceção deve registrar motivo, prazo de remoção, rollback e evidência.

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
   Todo SPEC deve apontar, nos passos com risco estrutural, operacional ou de segurança, qual disciplina de `disciplines/KAHNEMAN-DISCIPLINES.md` está sendo usada para reduzir viés, quais evidências mínimas são exigidas e qual condição objetiva dispara abortar, voltar um passo ou rollback.
7. **Passos críticos devem levar ao documento de disciplina**
   Nenhum item crítico do SPEC fica autocontido só em execução; ele precisa apontar para `disciplines/KAHNEMAN-DISCIPLINES.md` e registrar como a disciplina afeta a decisão local.

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
- Explique por que ela é a recomendada no contexto do seu repo
- Liste alternativas descartadas e por quê
- Registre lacunas de contexto que permaneceram abertas

#### Fase 3 — PRD final

- Escreva o `PRD.md` refletindo apenas a opção recomendada
- Não escreva um PRD com múltiplas arquiteturas concorrentes
- Se houver incerteza real, registre-a em riscos, dependências ou fora de escopo

### Pesquisa obrigatória antes de gerar o PRD

#### 1. Codebase do seu repo — contexto interno

> No `civm`: leia `rules/*.md`, `AGENTS.md`/`README.md`/`CODEX.md`, `.github/workflows/ci.yml` e os packages em `internal/**` (+ `cmd/civmctl/`) tocados pela mudança. Não há serviços HTTP, schema PostgreSQL nem frontend — pule os itens de exemplo abaixo que não se aplicam.

- Leia as regras do repo (no exemplo: `rules/` backend.md, frontend.md, security.md, testing.md, infra.md, coding.md)
- Leia o doc de instruções na raiz para topologia de serviços, multi-tenancy flow, auth modes, env vars (quando existirem)
- Identifique o(s) serviço(s)/package(s) alvo e leia o entry point, routes, handlers, services, repository e models existentes
- Mapeie o schema atual: migrações e schema tenant vs. global (se o repo tiver banco)
- Identifique tabelas, índices, constraints e relações que tocam nessa feature
- Verifique Redis keys, Pub/Sub channels, locks, cache patterns e invalidação relacionados
- Leia `docs/config-reference.json` para env vars existentes e categorias disponíveis
- Leia `docs/events-catalog.json` para eventos Redis Pub/Sub já catalogados
- Leia `disciplines/KAHNEMAN-DISCIPLINES.md` quando a mudança envolver risco estrutural, operacional ou de segurança
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
- Quando o produto não tiver produção viva, aplique a política Day-0: proponha a solução correta principal, sem compatibilidade legada, shims, workarounds ou backfills para produção inexistente
- Se sugerir backfill, migration incremental, dual path ou compatibilidade, declare a exceção Day-0 com motivo objetivo; caso contrário, consolide a modelagem/migration/contrato no desenho final
- Em **Alternativas descartadas**, descarte explicitamente soluções de compatibilidade quando elas só existirem para preservar versão antiga sem produção viva
- Liste os documentos que precisarão ser atualizados no mesmo commit quando houver impacto estrutural
- Se a mudança tiver risco alto, antecipe no PRD quais etapas provavelmente exigirão disciplina explícita de `disciplines/KAHNEMAN-DISCIPLINES.md` no SPEC
- Mantenha o PRD específico e operacional; evite texto genérico

### Saída esperada

Gere o arquivo `docs/{feature-slug}/PRD.md` com **EXATAMENTE** esta estrutura:

#### Resumo

- O que é, por que existe, qual problema resolve
- Valor de negócio no contexto do produto

#### Contexto técnico

- Serviço(s)/package(s) envolvidos e papel de cada um na topologia do repo
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

## PASSO 2 — Geração do SPEC.md / SPECv2.md (a partir do PRD ou auditoria)

> **Leia o `docs/{feature-slug}/PRD.md` e produza um `docs/{feature-slug}/SPEC.md` cirúrgico para implementação.**
> Se este Passo 2 estiver sendo reexecutado depois de um `no-go` do Passo 2.5, preserve o `SPEC.md` original e crie/atualize `docs/{feature-slug}/SPECv2.md`.
> O SPEC não replica o PRD; ele fecha decisões, remove ambiguidade e traduz requisitos em mudanças exatas no repo.

### Objetivo do Passo 2

- transformar requisitos em tarefas de código com ordem e dependências
- resolver ambiguidades do PRD antes do código
- explicitar impactos em contrato, dados, docs, testes e rollout
- ligar etapas críticas às disciplinas de `disciplines/KAHNEMAN-DISCIPLINES.md`

### Prompt

Leia `docs/{feature-slug}/PRD.md` e gere `docs/{feature-slug}/SPEC.md` com decisões fechadas, rastreabilidade por requisito e instruções implementáveis sem interpretação.

Se você estiver voltando do Passo 2.5 com decisão `no-go`, leia também o relatório da auditoria e gere `docs/{feature-slug}/SPECv2.md` como versão melhorada, sem alterar o `SPEC.md` original.

### Regras

1. Só inclua o que será realmente implementado agora
2. Cada arquivo listado deve ter caminho completo a partir da raiz do repo
3. Cada mudança deve explicar **o que muda**, **como muda** e **por que muda**
4. Referências a código existente devem usar nome exato de função, struct, tipo ou interface
5. Ordem dos itens = ordem de implementação
6. Se o PRD estiver ambíguo, resolva aqui com uma decisão explícita e justificativa
   - Na primeira execução do Passo 2, grave o resultado em `docs/{feature-slug}/SPEC.md`
   - Na execução após `no-go` do Passo 2.5, grave o resultado em `docs/{feature-slug}/SPECv2.md`, preservando `SPEC.md`
   - Se `SPECv2.md` já existir, atualize `SPECv2.md` in-place, salvo pedido explícito para criar nova versão
7. Toda query SQL deve usar placeholders posicionais (`$1`, `$2`)
8. Todo handler deve validar input antes de tocar no service layer
9. Todo endpoint tenant-scoped deve passar por `TenantResolver → Auth → Permission` quando aplicável
10. Não deixe pseudocódigo estrutural em tipos, handlers, queries ou contratos
11. Todo requisito funcional do PRD deve ser rastreado por ID no SPEC
12. Toda mudança estrutural deve dizer quais documentos serão atualizados no mesmo commit
13. Em etapas com risco estrutural, operacional, de segurança, rollout, rollback, migração, tenant isolation, auth, cache, contrato, secret, retry ou backfill, o SPEC deve apontar explicitamente a disciplina correspondente em `disciplines/KAHNEMAN-DISCIPLINES.md`
14. Nenhum passo crítico pode ficar só com instrução operacional; ele deve registrar também pergunta obrigatória, evidência mínima e abort trigger
15. Em qualquer mudança com transação, onboarding, saga, backfill ou múltiplos writes, o SPEC deve declarar explicitamente a fronteira de atomicidade: o que fica atômico nesta issue e o que continua fora dessa garantia
16. Toda evidência mínima de etapa crítica deve dizer como será produzida no repo de forma executável, observável e reproduzível (`go test`, `yarn test`, query SQL, diff, log esperado, validação manual obrigatória documentada). Não vale evidência implícita, presumida ou sem caminho de execução descrito
17. Em qualquer mudança com migration, backfill, rollout ou risco de perda de dados, o SPEC deve definir separadamente:
    - rollback de aplicação
    - rollback de migration
    - rollback de dados
      e dizer explicitamente o que é permitido, proibido ou `forward-only` por ambiente
18. Aplique a política Day-0: o SPEC deve escolher a solução principal e única. Não liste shims, compatibilidade legada, dual-reader, dual-write, backfill ou código morto, salvo exceção explícita e justificada
19. Para migrations de estruturas ainda não vivas em produção, prefira consolidar a migration inicial correta em vez de criar migration incremental corretiva
20. Se qualquer arquivo existir apenas para compatibilidade temporária, o SPEC deve marcar o arquivo para não criar ou para deletar, salvo exceção Day-0 documentada

### Guardrail cognitivo obrigatório

Em qualquer ITEM do SPEC que envolva migração, auth, tenant isolation, rollout, rollback, cache, contrato, secret, retry, backfill ou risco de indisponibilidade, incluir um bloco `Disciplina Kahneman` com:

- **Disciplina**: nome exato da disciplina em `disciplines/KAHNEMAN-DISCIPLINES.md`
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

Quando gerar `SPECv2.md`, manter a mesma estrutura abaixo e adicionar logo após o H1:

```markdown
> Versão melhorada após auditoria do Passo 2.5.
> Baseline preservado: `SPEC.md`.
> Motivo: {resumo objetivo dos blockers corrigidos}.
```

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

| Etapa / ITEM | Disciplina Kahneman | Link                                       | Pergunta obrigatória | Evidência mínima | Abort trigger |
| ------------ | ------------------- | ------------------------------------------ | -------------------- | ---------------- | ------------- |
| ITEM-3       | ...                 | `disciplines/KAHNEMAN-DISCIPLINES.md` | ...                  | ...              | ...           |

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
- **Política Day-0:** dizer explicitamente se a mudança consolida migration inicial ou cria migration incremental; migration incremental exige justificativa se não houver produção viva
- **Backfill:** deve ser `N/A — Day-0, sem produção viva` por padrão. Só descrever backfill quando houver dado real que precise ser preservado
- **Compatibilidade legada:** deve ser `N/A` por padrão. Se existir, justificar a exceção Day-0
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

| Documento                                  | Atualização necessária | Motivo                           |
| ------------------------------------------ | ---------------------- | -------------------------------- |
| `docs/openapi/ms-{name}.openapi.yaml`      | Criar / Alterar / N/A  | contrato mudou?                  |
| `web/src/fsd/shared/api/sdk.types.ts`      | Regenerar / N/A        | schema mudou?                    |
| `docs/config-reference.json`               | Criar / Alterar / N/A  | env var nova?                    |
| `docs/events-catalog.json`                 | Criar / Alterar / N/A  | evento novo?                     |
| `.env.example`                             | Criar / Alterar / N/A  | configuração nova?               |
| `CLAUDE.md`                                | Alterar / N/A          | padrão estrutural mudou?         |
| `.claude/rules/*.md`                       | Alterar / N/A          | convenção nova?                  |
| `web/CLAUDE.md`                            | Alterar / N/A          | frontend pattern novo?           |
| `docs/decisions/ADR-NNN-*.md`              | Criar / N/A            | decisão arquitetural relevante?  |
| `disciplines/KAHNEMAN-DISCIPLINES.md` | Alterar / N/A          | nova disciplina, link ou anchor? |

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

- [ ] Cada etapa crítica aponta para `disciplines/KAHNEMAN-DISCIPLINES.md`
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

Se `docs/{feature-slug}/SPECv2.md` existir e tiver sido criado como resposta a um `no-go` anterior, revise o `SPECv2.md` como candidato ativo.

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
- gaps entre etapas críticas do SPEC e as disciplinas documentadas em `disciplines/KAHNEMAN-DISCIPLINES.md`
- presença de workaround, shim, compatibilidade legada, dual-reader, dual-write, backfill ou migration incremental corretiva sem exceção Day-0 explícita
- código novo mantendo versão antiga sem produção viva

### Formato da resposta

1. Liste os findings primeiro, ordenados por severidade
2. Para cada finding, cite a seção exata do SPEC afetada
3. Depois liste `Open questions`
4. Depois diga se o SPEC está pronto para implementação
5. Feche com `go` ou `no-go`

### Regra de saída

- Se houver finding que exija decisão nova, volte ao **Passo 2**
- Se o arquivo auditado foi `SPEC.md` e o resultado foi `no-go`, **não pare apenas na auditoria**: volte ao **Passo 2 no mesmo turno**, crie `SPECv2.md` corrigindo os findings bloqueantes e preserve o `SPEC.md` original
- Se o arquivo auditado foi `SPECv2.md` e o resultado foi `no-go`, **não pare apenas na auditoria**: volte ao **Passo 2 no mesmo turno** e atualize `SPECv2.md` in-place, salvo pedido explícito para criar `SPECv3.md`
- Ao criar ou atualizar o SPEC corrigido depois de um `no-go`, registre no arquivo:
  - qual SPEC foi auditado
  - quais findings bloqueantes foram endereçados
  - que a versão corrigida é o candidato ativo para nova auditoria
- Se houver etapa crítica sem link para disciplina Kahneman aplicável, sem evidência mínima ou sem abort trigger, o resultado deve ser `no-go`
- Se houver violação da política Day-0 sem exceção documentada, o resultado deve ser `no-go`
- Se a auditoria resultar em `go`, siga para o **Passo 3**

---

## PASSO 3 — Implementação (a partir do SPEC ativo)

> **Leia o SPEC ativo e execute-o passo a passo.**
> O Passo 3 não fecha lacunas arquiteturais; ele implementa o que já foi decidido.
> O SPEC ativo é a última versão auditada com `go`: `SPECv2.md` quando existir e tiver sido aprovado pelo Passo 2.5; caso contrário, `SPEC.md`.

### Prompt

Implemente a feature descrita no SPEC ativo de `docs/{feature-slug}/`.

Use `docs/{feature-slug}/SPECv2.md` quando ele existir como versão melhorada pós-auditoria e tiver recebido `go` no Passo 2.5. Use `docs/{feature-slug}/SPEC.md` apenas quando não houver `SPECv2.md` ativo.

### Regras de execução

1. Siga a ordem de implementação do SPEC
2. Use os trechos, assinaturas e contratos do SPEC como base
3. Não adicione funcionalidade fora do escopo fechado
4. Se encontrar um gap, volte ao Passo 2 antes de continuar
5. Toda alteração em contrato deve ser refletida em docs e types no mesmo ciclo
6. Toda alteração em dado, auth, tenant isolation ou cache deve ser validada com teste
7. Não refatore código adjacente sem necessidade funcional
8. Em qualquer item crítico, execute também o bloco `Disciplina Kahneman` antes de avançar para a próxima fatia
9. Implemente a solução Day-0 limpa definida no SPEC. Não adicione shims, fallbacks, compatibilidade com versões antigas, backfills ou dead code
10. Se durante a implementação parecer necessário manter duas versões, pare e volte ao Passo 2 para registrar a exceção Day-0
11. Quando o SPEC consolidar uma estrutura Day-0, reescreva/remova o código antigo necessário em vez de preservar caminhos mortos

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
- [ ] Etapas críticas continuam coerentes com `disciplines/KAHNEMAN-DISCIPLINES.md`

### Quando voltar ao SPEC

- Descobriu necessidade de índice não previsto
- Handler precisa de campo extra
- Edge case não coberto apareceu
- Ordem de implementação não fecha
- Mudou shape de resposta ou contrato OpenAPI
- Surgiu necessidade de rollout, backfill ou rollback não descritos
- A etapa crítica exige uma decisão que o mapa Kahneman do SPEC ainda não fechou
- Surgiu necessidade de manter versão antiga, shim, dual path, backfill ou compatibilidade não documentada como exceção Day-0

> **Regra absoluta:** se o código precisar decidir algo que o SPEC não decidiu, a implementação deve parar e o SPEC deve ser atualizado primeiro.
> Se o SPEC ativo for `SPECv2.md`, atualize `SPECv2.md`; não altere o `SPEC.md` baseline.

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
- etapas críticas estiverem mapeadas para `disciplines/KAHNEMAN-DISCIPLINES.md`
- cada etapa crítica tiver pergunta obrigatória, evidência mínima e abort trigger
- se o risco for alto, o Passo 2.5 tiver resultado em `go`

### Implementação → Commit

Só avance se:

- código, testes e docs estiverem consistentes com o SPEC
- validações finais tiverem sido executadas
- não houver drift entre contrato, types, handlers e frontend
- não houver drift entre o que foi implementado e os guardrails cognitivos descritos no SPEC

---

## Referência rápida — Stack de exemplo (ilustrativo)

> Stack do produto-exemplo usado nos prompts. **Não é o stack do `civm`** (Go stdlib-first, sem frontend/DB). Troque pela tabela do seu repo.

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
