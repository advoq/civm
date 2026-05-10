# Kahneman disciplines — higiene de decisão no compexhub

Disciplinas operacionais derivadas de **Thinking, Fast and Slow** (Kahneman, 2011) e **Noise** (Kahneman, Sibony, Sunstein, 2021), aplicadas ao compexhub. Doc autocontido — não depende de nenhum outro repositório.

LLMs falam Sistema 1 com fluência altíssima — articulam bem até quando estão errados. O humano (ou o agente AI quando assume papel humano) precisa ser o Sistema 2: verificar, medir, questionar o que parece óbvio. Este doc codifica os atritos que evitam que decisão fluente vire decisão errada.

**Quem deve ler:** dev antes de iniciar PRD em SSDV3, agente AI antes de propor mudança não-trivial em `internal/platform/`, revisor de PR, autor de ADR.

---

## Arquitetura — 4 camadas

```
Disciplinas (12 do livro Kahneman/Sibony/Sunstein)
  ↓ instanciam
Rubricas (SSDV3, PR review, .claude/rules)
  ↓ aplicam-se a
Arquitetura (ADRs com rollback trigger numérico)
  ↓ executam-se em
Operacional (invariantes 1–11 em CI, commit-msg hook, Husky)
```

---

## As 12 disciplinas

| # | Disciplina | Regra (1 linha) | Exemplo compexhub | Status CI |
|---|---|---|---|---|
| 1 | WYSIATI | Declarar o não-visto antes de opinar | "Sem ter testado refresh em concorrência (>50 req/s), estimo X com confiança Y%" | Manual (PR review) |
| 2 | Counterfactual obrigatório | Rollback trigger numérico em commit não-trivial | "se p99 > 500ms em 3 rodadas, reverter pra chave estática" no body | **Invariante #6** |
| 3 | Número não adjetivo | Métrica antes de adjetivo | "p99 220ms ±15% em 3 rodadas" não "rápido". Anti-padrões: "obviamente", "definitivamente", "claramente" | Manual (PR review) |
| 4 | Anchoring em estimativas | Reference class antes de bottom-up | M3 (auth) estimado pela reference class M0+M1+M2 já fechados (custo real × multiplicador) | Manual (ROADMAP/PRD) |
| 5 | Availability heuristic | Worst-case, não happy path | Provisioner desenhado para 1k workspaces simultâneos, não 10. `services/shared/shardpool` LRU N=10 capa pool por slug | Manual |
| 6 | Confiança calibrada | Intervalo, não ponto | "~300 rps ±10%" não "300 rps"; "TTL 10min com jitter ±60s" | Manual |
| 7 | Hindsight bias | Postmortem separa processo de outcome | `docs/postmortems/TEMPLATE.md` exige "decisão tomada com info disponível" ≠ "o que aconteceu" | Manual |
| 8 | Planning fallacy | Multiplicador de reference class | Estimativa inside view × ~1.5x baseado em M já fechados; "tempo é alarme, não meta" (CHARTER) | Manual (META-AUDIT trimestral) |
| 9 | Substituição de pergunta | Qualitativa vira métrica | "schema-per-tenant é boa?" → cobertura ≥98%, LOC ≤400, dep graph acíclico, p99 query ≤80ms | Parcial (#10 cobre cobertura) |
| 10 | Hyperbolic discounting | TODO sem owner+date bloqueado | `// TODO(@user, YYYY-MM-DD): ...`; pasta `apps/_deferred/<slug>/` com gate numérico de promoção | **Invariante #8** |
| 11 | Halo effect em libs | Entry em LIBRARIES.md mandatória | Toda dep nova em `package.json`/`go.mod` exige entry em `docs/LIBRARIES.md` com 7 campos (alternativas, custo, rollback) | Manual (CI auto em M5+) |
| 12 | Priming em prompts | Framing adversarial em PR review e SSDV3 | "qual problema esse código tem?" / "liste 3 cenários de falha" — não "está OK?". Pause após 3 PRs em main evita priming acumulativo | Manual (cultural) |

---

## Top-5 operacionais (espelha CLAUDE.md raiz §"Decision hygiene")

1. **WYSIATI (#1)** — declarar o NÃO-visto antes de opinar.
2. **Counterfactual (#2)** — `Rollback trigger:` no body de commits não-triviais (invariante #6).
3. **Número não adjetivo (#3)** — claim de perf precisa de medição com unidade, intervalo e número de rodadas.
4. **Débito é dívida com juros (#10)** — TODO sem owner+date bloqueado (invariante #8).
5. **Lib nova exige `docs/LIBRARIES.md` (#11)** com critério mensurável.

Quando a pergunta é qualitativa, responder com métrica antes do adjetivo (#9): cobertura ≥98%, LOC por arquivo ≤400, dep graph acíclico, ADR descrevendo trade-offs.

---

## Rubricas ativas

| Procedimento | Onde vive | Disciplinas que opera |
|---|---|---|
| SSDV3 (PRD → SPEC → IMPL) | `docs/`, `docs/specs/<slug>/`, `.claude/rules/ssdv3.md` | #1, #2, #3, #5, #9 |
| ADRs append-only | `docs/decisions/ADR-NNN-*.md` (formato em ADR-000) | #2, #4, #7, #11 |
| Postmortem | `docs/postmortems/TEMPLATE.md` | #5, #7, #8 |
| Invariantes 1–11 em CI | `tools/compexhubctl/cmd/checkinvariants/` | #2 (#6), #3 (#2), #6 (#8), #10 (#8), #11 (#11) |
| Sync rule (CLAUDE ≡ AGENTS ≡ CODEX ≡ rules) | `compexhubctl check-sync` (invariante #5) | #12 (cultural) |
| Reference class para estimativas | Milestones já fechados em `ROADMAP.md` | #4, #8 |
| `docs/META-AUDIT.md` | append-only, trimestral | #7, #11 |
| `docs/LIBRARIES.md` (template 7 campos) | append-only | #11 |

Rules granulares por domínio em `.claude/rules/`:

- **Frontend:** `frontend.md`, `testing.md`, `i18n.md`
- **Backend:** `backend.md`, `auth.md`, `observability.md`
- **Segurança/Infra:** `security.md`, `observability.md`
- **Metodologia:** `ssdv3.md`

---

## Mapeamento disciplina → invariante CI

| Disciplina | Invariante CI | Forma de check | Bloqueio? |
|---|---|---|---|
| #2 Counterfactual | #6 Rollback trigger | commit-msg hook regex `Rollback trigger:` em body para `feat\|fix\|refactor\|perf` | Sim (pre-push + CI) |
| #3 Número (proxy console.log) | #2 | grep `console.log` em `apps/web/src/**` | Sim |
| #10 Débito | #8 TODO owner+date | grep `TODO\|FIXME` sem `(@user, YYYY-MM-DD)` | Sim |
| #9 Substituição (cobertura como métrica) | #10 Coverage | go test + vitest coverage ≥98% | Sim (M5+) |
| Self-containment | #11 | regex termos bloqueados após sanitizar exceções legítimas | Sim |
| #1, #4, #5, #6, #7, #8, #11, #12 | — | manual em PR review + ADR review + SSDV3 | Manual |

**5 disciplinas com gate CI hard, 7 manuais.** Não é teatro: a maioria das disciplinas exige humano consciente.

---

## Contra-exemplos por disciplina

Toda disciplina vira **comportamento de Sistema 1 disfarçado** quando aplicada sem juízo. Os 12 modos de falha agrupam em 4 padrões:

| Padrão | Disciplinas | Sintoma comum |
|---|---|---|
| Forma sem conteúdo | #2, #3, #6 | compliance formal, valor zero |
| Over-engineering | #5, #8 | custo presente para risco hipotético |
| Atrito injustificado | #10, #11, #12 | fricção sem ganho de qualidade |
| Eliminação de nuance | #1, #4, #7, #9 | regra substitui pensamento em vez de guiar |

| # | Vira... | Sintoma concreto | Mitigação |
|---|---|---|---|
| 1 | Paralisia | listar TODA ignorância em todo PRD trava SSDV3 PASSO 1 | PRD agrupa "Inferências" em seção separada, capada em 30% do total |
| 2 | Teatro | "Rollback trigger: se der errado, reverter" passa lint mas é vazio | invariante #6 + PR reviewer recusa frase sem unidade/janela |
| 3 | Métrica sem contexto | "p99 -30%" inútil se bench rodou sem `-race` em máquina diferente | SSDV3 SPEC §"Validação" exige descrever ambiente do bench |
| 4 | Anchor errado | reference class M0+M1+M2 (docs) subestima M3 (runtime) | para M3, citar também complexidade de `docs/specs/auth-port/PRD.md` |
| 5 | Paranoia | provisioner desenhado pra 1M workspaces quando precisa de 1k | CHARTER §"Capacidade declarada" Tier-1 explicita gate por carga real |
| 6 | Escape | "p99 220ms ±50%" cobre tanto "passa" quanto "falha" | stddev >25% trigga investigação, não aceitação |
| 7 | Desculpa | "Foi sorte genuína" vira racionalização do bug | postmortem template §"Causa raiz" exige dimensão técnica precisa |
| 8 | Inflação | multiplier 5x vira justificativa pra qualquer estimativa inflada | ROADMAP "tempo é alarme, não meta" — postmortem em vez de inflar |
| 9 | Eliminação de pergunta válida | forçar tudo em métrica destrói nuance UX | SSDV3 RNFs aceitam "qualidade percebida" + teste com 3 humanos não-autores |
| 10 | Scope creep | PR fix-only removendo TODOs antigos vira PR de 50 arquivos | invariante #8 só pega TODO **novo** no diff (regex no diff) |
| 11 | Not-Invented-Here | recusar lib boa "porque não está em LIBRARIES" sem testar | template é 5min; ADR-NNN documenta razão |
| 12 | Degradação de colaboração | "Qual problema tem?" em pair programming dá ar de desconfiança | framing rota — em PR review usar; em pair programming não |

**Meta-contra-exemplo: cargo cult.** Kahneman aplicado sem skin-in-the-game vira ritual: autor commita rollback trigger numérico mas nunca executa quando condição dispara — invariante existe só no hook, não na mente. Mitigação: `docs/META-AUDIT.md` auditoria trimestral walks rollback triggers e documenta acionamentos.

---

## Meta-regra: counterfactual como gatekeeper

Se você (humano ou IA) não consegue responder "**O que me faria mudar de opinião?**" com algo específico (numérico, observável), **pare**. Está em Sistema 1 — opinião fluente sem condição de revisão.

Resposta válida: "se p99 do hash semaphore passar de 400ms em 3 rodadas, reverter pra chave estática" ou "se shadow-deploy do JWKS divergir > 0.5% em 24h, adiar promoção do M3".

Resposta inválida (Sistema 1 disfarçado): "se der errado", "se piorar", "se não funcionar". Sem unidade, sem janela, sem trigger — é wishful.

compexhub aplica isso a si mesmo:

- ADRs em `docs/decisions/` têm `## Rollback trigger` obrigatória.
- Milestones em `ROADMAP.md` têm rollback trigger numérico.
- Libs em `docs/LIBRARIES.md` têm rollback trigger.
- Invariante #6 bloqueia commits `feat/fix/refactor/perf` sem trigger no body.
- **Este doc tem rollback trigger:** se em 6 meses (2026-10-29) <30% dos PRs não-triviais citarem alguma disciplina, simplificar para Top-5 + 1-pager via ADR superseding.

---

## Como a IA/dev usa este doc

1. Antes de opinar em escolha não-trivial: declarar o NÃO-visto (#1).
2. Com a opinião: número, não adjetivo (#3). Se métrica não disponível, declarar "sem medir, estimo X com confiança Y%".
3. Depois: counterfactual específico no commit body (#2 — invariante #6).
4. Adicionar dep: entry em `docs/LIBRARIES.md` primeiro (#11).
5. Achar TODO órfão: remover ou abrir PR dedicado (#10 — invariante #8).
6. Mudar auth/rbac/tenancy/migration irreversível: SSDV3 obrigatório (#1, #2, #5, #9).
7. Postmortem: separar processo de resultado (#7).
8. Estimativa de milestone: reference class + multiplicador honesto (#4, #8).
9. Prompt para outro agente: framing adversarial (#12).

---

## Princípio de Noise

Mesmo código, reviewer diferente, feedback diferente — isso é ruído. As invariantes 1–11 do compexhub são **rubricas fixas**: não variam com humor do dia. São anti-noise estrutural. Adicionalmente, ADR-000 estabelece formato canônico de decision record que reduz variância na narrativa de decisão.

---

## Leituras cruzadas

- `CHARTER.md`, `ROADMAP.md`, `MANIFESTO.md`, `docs/INVARIANTS.md`
- `docs/LIBRARIES.md`, `docs/META-AUDIT.md`, `docs/decisions/`, `docs/postmortems/TEMPLATE.md`
- `docs/SSDV3-PROMPTS.md`
- `.claude/rules/ssdv3.md`, `.claude/rules/auth.md`, `.claude/rules/backend.md`
- Kahneman, _Thinking, Fast and Slow_ (2011)
- Kahneman, Sibony, Sunstein, _Noise_ (2021)
