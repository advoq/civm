# CODEX.md — civm

Operação para CLIs estilo Codex (automação + DEFERRED + pause rules).

## Hierarquia

CODEX.md complementa `AGENTS.md` (que complementa `README.md`). Em conflito:
`rules/<topic>.md` > `CLAUDE.md` (se existir) > `AGENTS.md` > `CODEX.md`.

## Escopo de execução autônoma

civm permite execução autônoma para:

- ✅ Editar `runbooks/*.md`, `templates/*`, `disciplines/*.md`, `rules/*.md`
- ✅ Editar `cmd/civmctl/**` e `internal/**` (código Go)
- ✅ Adicionar testes em `*_test.go`
- ✅ Atualizar `README.md`, `AGENTS.md`, `CODEX.md` (respeitando sync rule)
- ✅ Atualizar `MEMORY.md` (append-only, never reorder/delete)
- ✅ Editar `.github/workflows/*.yml`, `release-please-config.json`,
  `.release-please-manifest.json` (release automation e CI gates)
- ✅ Build e test local (`go build`, `go test`)
- ✅ Commit local (sem push)
- ✅ Modificar peer repos quando houver autorização explícita do humano para
  trabalho cross-repo, preservando WIP e committando só arquivos tocados

civm **NÃO** permite autonomamente:

- ❌ `git push` para `origin/main` (sempre humano)
- ❌ Alterar `.git/config` ou hooks
- ❌ Criar/deletar repos no GitHub via `gh repo create`/`gh repo delete`
- ❌ Modificar repos peer (compexhub, vitae, advoq) sem autorização
  explícita do humano para o escopo cross-repo
- ❌ Executar `civmctl bootstrap` ou `civmctl cleanup --execute` na máquina
  do dev (destinado à VM dedicada; agente sandboxed não tem SSH)
- ❌ Persistir secret em qualquer arquivo (mesmo `.env.example`)

## Governança de PR sem issue

PR sem issue só é válido quando a seção `## Issue` traz marcador
explícito `Sem issue`, `No issue` ou `N/A`. Usar essa exceção apenas para
trabalho operacional, CI ou documentação que não merece rastreio próprio.
Feature, bug, refactor não-trivial e mudança com rollback real continuam
exigindo issue linkada por `Closes`, `Fixes` ou `Resolves`.

Se a metadata do PR divergir do estado real (labels, assignee, issue ou
marcador sem issue), corrigir o PR no GitHub e aguardar checks de
governança/CI antes do merge.

## Cleanup safety

`civmctl cleanup --execute` e `civmctl disk-watchdog --execute` são
fail-closed: se detectarem `Runner.Worker`, processo dentro de `_work`,
Docker build/compose/buildctl ativo ou se não conseguirem provar o host
ocioso, abortam antes de deletar/prunar. Não adicionar flag ou runbook para
contornar esse guard sem nova SPEC e validação em VM.

O cleanup de `_work` preserva caches de runner em `_work/_tool` e
`_work/_actions`. Esses diretórios evitam downloads repetidos de toolchains e
actions; só devem ser removidos manualmente depois de medir pressão real de
disco e confirmar host ocioso.

`civmctl runner restart/remove/upgrade --execute` usa a mesma checagem
compartilhada (`civmctl idle-check`). Mutação de runner deve abortar antes de
`systemctl restart/stop`, `config.sh remove`, `rm -rf` ou upgrade de tarball
quando o host estiver ocupado ou desconhecido. Em `runner remove`, falha real
em `svc.sh stop` ou `svc.sh uninstall` também deve parar o fluxo antes de
desregistrar ou remover diretório.

Downloads executados como root devem ter checksum pinado no código antes de
qualquer extração, instalação ou execução de script. Se o upstream publicar
nova versão sem checksum pinado, o comando deve falhar pedindo atualização do
pin, não prosseguir por confiança em HTTPS.

## Release automation

`release-please` (`.github/workflows/release.yml` +
`release-please-config.json` + `.release-please-manifest.json`) abre e
mantem um PR de release em `main` a cada push, calculando bump por
Conventional Commits. O PR agrupado usa o título
`chore: release civm v<X.Y.Z>`; `civm` e apenas texto cosmetico no
titulo, nao `package-name`. O agente NAO faz tag manual nem
`gh release create` fora desse fluxo — qualquer release passa pelo PR de
release, que e mergeado por humano. Detalhes em
`runbooks/RELEASE-AUTOMATION.md`.

Quando os artefatos `release-please-*.json` ou `release.yml` mudarem,
sincronizar `README.md` §"Versionamento" e `AGENTS.md` §"Commits" no
mesmo commit (invariante #5).

## Pause rules (modo autônomo)

Quando humano pede execução autônoma ("continue", "faça tudo", "auto"):

1. **Pause após 3 commits locais consecutivos** em civm. Reportar estado
   e pedir confirmação antes de seguir.
2. **Pause obrigatoriamente após mudança em `cmd/civmctl/**`** se afetar
   subcomando `bootstrap` ou `cleanup` (lógica de mutação no host).
3. **Pause obrigatoriamente ao adicionar dep externa** (`go get` non-stdlib).
4. **Pause se classifier negar** edição em peer repo — não contornar, pedir
   autorização.
5. **Pause antes de `gh repo create`** — sempre humano confirma.

Sem resposta no ponto de pausa, **não continuar** — aguardar.

## Verificação pós-release

Releases sao criados via merge do PR
`chore: release civm v<X.Y.Z>` gerado por release-please. Após o
merge desse PR, revalidar sem mutação:

```bash
gh release view "$(gh release list --repo advoq/civm --limit 1 --json tagName --jq '.[0].tagName')"
git fetch --tags origin && git tag --list 'v*' --sort=-version:refname | head -3
git status --short --branch
gh run list --workflow=ci.yml --branch=main --limit 5
gh run list --workflow=release.yml --branch=main --limit 3
ssh gha-ubuntu-2404 'civmctl parity'
ssh gha-ubuntu-2404 'civmctl health'
ssh gha-ubuntu-2404 'civmctl doctor --json'
ssh gha-ubuntu-2404 'civmctl idle-check'
```

Warning `LAST cleanup timer nunca rodou` é aceitável até o primeiro
disparo real do timer diário. Se continuar após a próxima janela diária
esperada, pausar qualquer conclusão de release e tratar como ação
operacional na VM.

## DEFERRED (features pensadas, ainda não implementadas)

Lista de funcionalidades que **podem** ser construídas se houver demanda
real. Cada item tem gate numérico de promoção (não promover por entusiasmo).

### `civmctl runner add` automatizado via GitHub App

**Estado:** stub planejado, não implementado.
**Por que adiar:** GitHub App setup é overhead se 1-2 runners servem 3 repos.
**Gate de promoção:** ≥5 peer repos OU ≥10 runners simultâneos OU
incidente de PAT expirado.
**Quando promover:** seguir `runbooks/CI-GITHUB-APP-SETUP.md`.

### `civmctl deploy` para VMs múltiplas

**Estado:** não planejado.
**Por que adiar:** 1 VM serve 3 repos. Multi-VM é prematuro.
**Gate:** ≥3 VMs físicas OU latência geográfica documentada.

### `civmctl ci-mirror` (snapshot/restore de cache)

**Estado:** não planejado.
**Por que adiar:** GitHub Actions cache nativo via `actions/cache`
funciona em self-hosted runner se `RUNNER_TOOL_CACHE` estiver
configurado corretamente.
**Gate:** medição mostrar >30% do tempo de build em cache miss.

### `civmctl metrics` (Prometheus exporter)

**Estado:** não planejado.
**Por que adiar:** observability da VM faz-se via `node_exporter` padrão.
**Gate:** necessidade de métrica custom não-coberta por node_exporter.

### Suporte a `windows-latest` e `macos-latest`

**Estado:** não planejado.
**Por que adiar:** todos os peer repos rodam Linux. Windows/macOS sem demanda.
**Gate:** peer repo concreto que precise + custo de VM Windows/Mac
justificado.

## Promoção de DEFERRED

1. Verificar gate numérico cumprido (não "achei que seria bom").
2. Abrir issue documentando: gate cumprido, evidência, escopo.
3. Adicionar ADR em `decisions/` (criar pasta se necessário).
4. Implementar com testes (≥80% cobertura).
5. Mover entrada deste CODEX.md para a seção apropriada do README.md.
6. Atualizar AGENTS.md com novo subcomando.

## Histórico de decisões

### 2026-05-10 — civmctl criado (revisão de decisão prévia)

Em sessão anterior, decidi-se "não civmctl" pelo argumento de que civm
"não faz audit". Decisão revisada nesta data: civmctl não faz audit; faz
provisioning + maintenance idempotente da VM. Gap detectado: provisionar
manualmente seguindo runbook é repetitivo, frágil e não-replicável (humano
esquece passo). civmctl resolve.

**Rollback trigger:** se em 6 meses (2026-11-10) civmctl não estiver sendo
usado para provisionar nenhuma VM nova OU se cleanup quebrar disco da VM
em produção, reavaliar (talvez voltar para runbook puro + Ansible playbook).

## Referências

- `AGENTS.md` — resumo geral
- `MEMORY.md` — log temporal append-only
- `runbooks/MULTI-PROJECT-RUNNER.md` — fluxo de provisionamento
- `templates/CIVM-USAGE.md` — fonte de `docs/CIVM.md` nos peer repos
- `cmd/civmctl/` — código do CLI

<!-- COMMUNICATION-STYLE:BEGIN -->
## Communication style

Estilo Tech Lead nas respostas:

- **TL;DR** primeiro (1-3 frases): o que é, status, próximo passo se houver.
- **Impact** (opcional): o que muda na prática.
- **Topics**: bullets curtos, no máximo 1 nível de aninhamento.
- **Next Steps**: ação requisitada do humano.

Honestidade técnica:

- Distinguir explícito o que está feito, o que está testado, o que é
  inferência, o que é bloqueio (classifier, permissão, SSH não disponível).
- Quando não puder fazer algo, dizer "não posso fazer X porque Y" — não
  fingir alternativa.
- Números antes de adjetivos. "p99 = 98ms" > "ficou rápido".

Sem floreio. Sem emoji a menos que o usuário use primeiro. Sem agradecimento
performativo. Sem repetir o pedido do usuário antes de responder.
<!-- COMMUNICATION-STYLE:END -->

> Source canônico: `~/codespace/civm/templates/COMMUNICATION-STYLE.md`
