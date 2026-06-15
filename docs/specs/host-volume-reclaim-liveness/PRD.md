# PRD — Host Volume Reclaim Liveness

> SSDV3 PASSO 1. Slug: `host-volume-reclaim-liveness`. Repo: `civm`.
> Estende `docs/specs/host-volume-reclamation/SPECv3.md` (o mecanismo de reclaim)
> com a garantia de **liveness**: o reclaim nunca pode morrer nem ser starvado.

## Contexto

O runner self-hosted civm roda numa VM Hyper-V `gha-ubuntu-2404` cujo VHDX vive
no volume `V:` do host (119GB). O VHDX cresce com a escrita do guest e **só
encolhe via `Optimize-VHD` offline** (Stop-VM → compacta → Start-VM). A task
`civm-vhdx-autoreclaim` (a cada 30 min, SYSTEM) faz isso quando o guest está
idle. O disk-watchdog recusa jobs (exit 75) quando `V: free <= 10GB` (piso
crítico) para evitar `PausedCritical`.

## Problema (incidente 2026-06-15)

O `V:` bateu no piso crítico e o runner **recusou TODOS os jobs** do PR advoq
#1155 (13/30 jobs exit 75). Investigação ao vivo (host + logs) revelou **duas
falhas de liveness do reclaim**, ambas instâncias da disciplina Kahneman #16 (o
mecanismo de cura não pode morrer com o recurso que cura):

- **F1 — Fantasma (Confirmado no host).** A task `civm-vhdx-autoreclaim` ficou
  com o Task Scheduler marcando `State=Running` desde ~06-13 (processo real
  morto, locks `V:\civm-autoreclaim.lock`/`civm-reclaim.lock` órfãos de Jun13
  16:31). Com `MultipleInstances=IgnoreNew`, **todo tick de 30 min foi ignorado**
  (`LastTaskResult=0x80070420` "already running") por ~30h. O
  `ExecutionTimeLimit` estava no **default do `schtasks /create` (72h/P3D)** — o
  Windows só mataria a presa após 3 dias. Sem reclaim, o VHDX cresceu e o `V:`
  drenou até o piso.

- **F2 — Starvation sob carga (Confirmado no host).** O reclaim é **idle-gated**
  (`Wait-GuestIdle` via `civmctl idle-check`, até 10 min). Sob carga de CI
  concorrente contínua o guest **nunca fica idle** → `autoreclaim_skip_busy` em
  todo tick → o VHDX cresce sem reclaim → `V:` cai ao piso. Reproduzido ao vivo:
  uma re-run de 11 jobs concorrentes levou `V:` de 29GB → 5.9GB e o Docker do
  guest a `guest_free=9GB`, com o reclaim incapaz de agir.

- **F3 — Limite estrutural (Confirmado em docs).** Mesmo com o reclaim vivo, o
  working set ATIVO de uma rajada concorrente pesada (imagens Docker + build de
  N serviços simultâneos) pode exceder a capacidade do `V:` de 119GB
  (`docs/specs/host-volume-reclamation` + `[[project_civm_footprint_fix]]`:
  "E2E-green needs a bigger disk"). Reclaim só recupera **slack reclamável** (
  VHDX inchado por dados já liberados/podáveis), não dados em uso ativo.

## Objetivo

Garantir que o reclaim do `V:` **sempre esteja vivo e atue antes do piso
crítico**, eliminando o death-spiral, e que o sistema **degrade graciosamente**
(drain + reclaim + resume) em vez de recusar todos os jobs. Não é objetivo faz
caber um working set ativo maior que o disco (isso é hardware, F3 — fora de
escopo, documentado).

## Requisitos funcionais

- **RF-1 — Liveness contra fantasma.** Uma instância fantasma do reclaim (task
  `Running` sem processo vivo) DEVE ser detectada e limpa em tempo **limitado e
  curto** (alvo: ≤5 min), não em 72h. O reclaim volta a rodar na cadência normal.
  _(Parcial: o `ExecutionTimeLimit` já foi cortado p/ PT2H — teto de 2h; RF-1
  pede o detector ativo de ≤5 min como cura precisa.)_

- **RF-2 — Reclaim load-aware (drain-on-pressure).** Quando `V: free` cai abaixo
  de um limiar de **WARN** (acima do piso crítico) E o guest não fica idle
  sozinho dentro da janela de espera, o reclaim DEVE **drenar** o runner (parar
  de aceitar novos jobs, deixar os em curso terminarem ou salvar estado),
  reclamar, e **religar** o runner. O `V:` nunca deve alcançar o piso de recusa
  sob carga com slack reclamável disponível.

- **RF-3 — Prune do guest antes do Optimize.** O reclaim DEVE liberar o disco do
  GUEST (docker/cache prune via `civmctl disk-watchdog`/cleanup) antes do
  `fstrim`+`Optimize-VHD`, senão o gap reclamável é ~0 e o Optimize não libera
  `V:`. _(Confirmado ao vivo: `docker_prune` liberou 17.5GB e habilitou o
  Optimize; sem ele o reclaim saiu em `autoreclaim_skip_low_gap gap=2.4GB`.)_

- **RF-4 — Degradação graciosa, não recusa cega.** O caminho de admissão de job
  ao bater o piso DEVE ser o ÚLTIMO recurso; RF-2 deve ter agido antes. Quando o
  piso for inevitável (F3, working set > disco), a recusa permanece o fail-safe
  correto (#16) — mas observável e raro, não o modo normal.

## Requisitos não-funcionais

- **RNF-1 — Sem perda de trabalho.** `Stop-VM` salva estado (não mata o job);
  `CompactVirtualDisk` é nativo e completa mesmo se o wrapper morrer (sem
  corromper o VHDX). O drain deve preferir terminar/salvar, nunca `-TurnOff`
  exceto no backstop de guest-wedge já existente.

- **RNF-2 — Bounded / anti-loop.** Drain→reclaim→resume tem cooldown e
  anti-flap; não pode entrar em laço de drain perpétuo.

- **RNF-3 — Fail-safe preservado.** Nenhuma mudança pode enfraquecer o piso
  crítico (#16): se o reclaim não puder agir, a recusa de job continua válida.

- **RNF-4 — Observável.** Cada decisão emite evento no
  `V:\civm-hyperv-maintenance.log` (família `reclaim_liveness_*`) e/ou métrica.

## Critérios de sucesso

1. Injetar um fantasma (task `Running` sem processo) → detectado e limpo ≤5 min.
2. Sob carga concorrente que dirija `V:` ao WARN, com slack reclamável → o
   reclaim drena, reclama e religa; `V:` volta acima do WARN; **nenhum job
   recusado por exit 75**.
3. Re-run completo do CI advoq #1155 com a nova lógica deployada → sem falha de
   disco (assumido slack reclamável suficiente; F3 fora de escopo).

## Fora de escopo

- F3 (working set ativo > capacidade do disco) — é limite de hardware; mitigação
  é disco maior ou menor concorrência de CI, ambos fora deste PRD.
- Mudanças no footprint do CI advoq (concorrência de jobs) — pertencem ao repo
  advoq, não ao civm.

## Reuso antes de criação (regra dura SSDV3)

- `civm-vhdx-autoreclaim.ps1` já tem: idle-check, two-phase emergency gate,
  Stop-VM/Optimize/Start, locks, restart retries, guest-unreachable forced
  reboot. RF-2 ESTENDE essa lógica, não cria reclaim novo.
- `civm-vhdx-optimize-watchdog` (hoje Disabled) já roda a cada 5 min + tem
  `Test-LockHeld` (distingue lock vivo de órfão) + Start-VM gated. RF-1 ESTENDE
  esse watchdog (detector de fantasma), não cria task nova.
- `civmctl disk-watchdog --execute` já faz o prune do guest (RF-3); só precisa
  ser orquestrado pelo reclaim host-side.
- `internal/hostdisk` (Go, testável) já modela decisões do watchdog
  (`watchdog_race_test.go`) — RF-1/RF-2 devem mover a decisão para Go quando
  possível, para teste com disciplina.

## Disciplinas Kahneman

- **#16 (fail-safe + a cura não morre com o recurso):** F1/F2 são violações
  diretas; RF-1/RF-2 são a correção. Link em cada passo crítico do SPEC.
- **#13 (existência ≠ função):** "a task existe/está Running" ≠ "o reclaim está
  funcionando" (era um fantasma). Validar por EFEITO (V: liberado), não por
  estado da task.
- **#15 (fail-fast só para determinístico):** o piso crítico é fail-safe correto;
  não o relaxar — RF-4.
