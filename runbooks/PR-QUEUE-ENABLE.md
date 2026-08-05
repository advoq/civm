# Runbook — ligar a fila FIFO por-PR da box (canario primeiro)

> **SUPERSEDED em 2026-08-05:** produção usa o owner C# e contexto exato por
> SHA. Este documento permanece como referência do rollback PowerShell. O
> rollout vigente é
> [`GENERATION-CLEAN-BOUNDARY-ROLLOUT.md`](GENERATION-CLEAN-BOUNDARY-ROLLOUT.md).
> Não use gate que libera ao vencer timeout.

Cada geração executa todos os checks; a box limpa todo estado regenerável,
compacta até comprovar `V: >=80 GiB` e só então admite a próxima geração.

## O que ja esta no codigo (commitado, DESLIGADO)
- `civm-pr-queue.ps1` — cerebro puro `Resolve-PrSlot` (grant/hold/grace/boundary_advance).
- `civm-vm-orchestrator.ps1` — observe (VIVO, loga `would_*`) + enforce atras de `-EnforceQueue`
  (default OFF): publica `currentPr` em `V:\civm-current-context` + limpa+compacta no boundary.
- `serialize.go` (civm + civm-e2e-fix) — runners `*-gate` ignorados na deteccao de colisao.
- `civm-gate-runner-provision.ps1` — provisiona o runner Windows do HOST.

## O job-gate (adicionar nos workflows box-heavy — go.yml primeiro, no canario)
```yaml
  wait-for-slot:
    if: ${{ vars.CI_BACKEND != 'paid' }}      # no-op no pago
    runs-on: [self-hosted, civm-gate]         # runner Windows do HOST
    timeout-minutes: 180
    steps:
      - name: Wait for PR slot (FIFO box queue)
        shell: pwsh
        run: |
          $ctx = if ('${{ github.event.pull_request.number }}'.Trim()) { 'pr-${{ github.event.pull_request.number }}@${{ github.event.pull_request.head.sha }}' } else { 'branch-${{ github.ref_name }}@${{ github.sha }}' }
          $path = 'V:\civm-current-context'; $deadline = (Get-Date).AddMinutes(170)
          while ($true) {
            $cur = ''; try { $cur = (Get-Content -LiteralPath $path -Raw -ErrorAction Stop).Trim() } catch {}
            if ($cur -eq $ctx) { Write-Host "slot: $ctx"; break }
            if ((Get-Date) -gt $deadline) { throw "timeout aguardando slot $ctx; admissao continua fechada" }
            Write-Host "fila: atual='$cur' eu='$ctx'"; Start-Sleep -Seconds 10
          }
```
Os jobs reais ganham `needs: [wait-for-slot, ...]`. Remover o `concurrency`
per-workflow (o gate substitui — acaba o cancel 1-pending). O contexto inclui
o SHA imutável; um push novo nunca herda a fronteira do push anterior.

## Passos do canario
1. **Provisionar o gate runner** (host, elevado):
   `$tok = gh api -X POST /orgs/acme/actions/runners/registration-token --jq .token`
   `pwsh C:\civm-deploy\civm-gate-runner-provision.ps1 -RegToken $tok -Index 1`
   Conferir: `gh api orgs/acme/actions/runners --jq '.runners[]|select(.name|endswith("-gate"))|.name'`.
   **Persistencia (sobreviver reboot/crash):** NAO use o service do Windows
   (`config.cmd --runasservice` da `Win32 1068` nesta box mesmo sem dependencias
   declaradas — beco sem saida). Use o WATCHDOG via scheduled task:
   `pwsh C:\civm-deploy\civm-gate-task-setup.ps1 -Index 1` (deleta o service
   quebrado e registra uma task com trigger `AtStartup` + tick de 2min
   `IgnoreNew`, mesmo padrao do orquestrador). Repita o `-Index` por runner do pool.
2. **Ligar o enforce** no orquestrador somente no rollback supervisionado e
   depois de provar a capability v1.
3. **PR throwaway A** com o gate so no `go.yml`. Abrir **PR throwaway B** logo depois.
   Medir no `civm-orchestrator.log`: `pr_boundary_compact done=pr-A next=pr-B`, e que o
   `go.yml` do B fica em `wait-for-slot` ate A terminar + a box compactar
   (`V: >=80 GiB`) -> aí B roda.
4. **Validar (Kahneman #13):** so os checks do `currentPr` rodam por vez; `disk_boundary_compact`
   1x por contexto; B comeca com `V: >=80 GiB`; zero cancelamento; PR sozinho sem espera extra.
5. **Rollar** o gate pros outros 6 workflows. Fechar os PRs throwaway.

## Rollback (se algo quebrar)
- Tirar o `-EnforceQueue` da task (volta pro observe-only; o codigo enforce fica dormente).
- O gate runner pode ficar (idle, label civm-gate, nao pega jobs `[self-hosted,civm]`).
- Reverter o `needs: wait-for-slot` nos workflows -> os jobs voltam a rodar sem o gate.

## Rollback trigger
Se um PR ficar preso em `wait-for-slot` >170min, o job-gate falha e a admissão
continua fechada. Investigar capability, reaper, owner e publicação; nunca
converter timeout em autorização para rodar sem a fronteira.
