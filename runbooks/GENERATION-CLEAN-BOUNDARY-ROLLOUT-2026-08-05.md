# Validacao live da fronteira limpa — 2026-08-05

## Escopo e limite da evidencia

Esta entrada registra o rollout guest-first do commit `59cb68e` do `civm` e
do controlador C# correspondente. Antes deste documento ser publicado, ainda
nao havia sido executado um PR canario posterior ao rollout; build e teste
local nao substituem essa prova.

## Pre-condicoes medidas

- filas `advoq/advoq` e `advoq/civm`: `0` queued e `0` in progress;
- VM `gha-ubuntu-2404`: `Off`, memoria atribuida `0` e checkpoints
  automaticos desabilitados;
- `V:`: `83,85 GiB` livres;
- `V:\civm-current-context`: vazio;
- `V:\civm-reclaim.lock`: ausente;
- `civm-host-orchestrator`: owner unico; owner PowerShell legado `Disabled`.

Dois runs de maio apareciam como `queued`, mas o endpoint de cancelamento os
classificava como concluidos. O PR, a branch e o SHA ja nao existiam. Os IDs
`26423751663` e `26423751642` foram removidos de forma exata pela API; a fila
voltou a `[]`.

## Rollout do guest

O primeiro `self-upgrade` recompilou o source staged em `/opt/civm`, mas esse
source estava em `c3bf40e`, anterior ao merge. A divergencia foi detectada
antes de promover o host. O worktree de deploy existente, sem mudancas
tracked, foi atualizado para `59cb68e`; nenhum clone novo foi criado.

Depois da recompilacao e do `hook install --no-restart`:

- `civmctl capability generation-clean-boundary` retornou
  `civm-generation-boundary/v1`;
- o wrapper root-owned retornou o mesmo marcador;
- `civmctl doctor --repos=auto --json` retornou `exit=0`;
- `civmctl health --json` retornou `exit=0`;
- reaper timer ficou `enabled+active` e service com resultado `success`;
- runner serial ficou `1/1 active/running` e `idle-check` retornou `0`.

## Rollout do host e compactacao

O binario do host foi trocado com a task owner desabilitada e backup
recuperavel. Um tick shadow decidiu `stop_and_compact` com `queued=0`,
`running=0` e `V:=76 GiB` enquanto a VM estava ligada.

O tick ativo manteve o contexto em `reclaim`, executou o wrapper do guest,
chegou a `Off` por poweroff gracioso e compactou o VHDX:

| Medida | Antes | Depois |
| --- | ---: | ---: |
| VHDX | `35,38 GiB` | `32,20 GiB` |
| `V:` livre | `76,00 GiB` | `86,94 GiB` |
| reclaim lock | presente durante o efeito | ausente |
| contexto publicado | `reclaim` durante o efeito | `0 bytes` |

Um trigger recorrente recebeu `0x800710e0` porque
`MultipleInstances=IgnoreNew` recusou sobreposicao enquanto o processo
original ainda possuia o lock. O processo original nao foi interrompido. O
heartbeat posterior terminou com `LastTaskResult=0`, task `Ready`, VM `Off`,
zero processo `civm-host`/SSH orfao e owner legado ainda `Disabled`.

## Veredito

PASS para rollout guest-first, owner unico, cleanup, poweroff gracioso,
compactacao e piso fisico de 80 GiB. PENDENTE nesta entrada: PR canario e
segundo push no mesmo PR para provar duas geracoes reais consecutivas.

## Canario do PR 230

O SHA `4fa5424` disparou o workflow completo. Na primeira tentativa, antes da
variavel opt-in existir no contexto do run, os tres jobs GitHub-hosted e o
agregador passaram; o smoke self-hosted foi `skipped`. A tentativa 2 do mesmo
run, depois de habilitar `CIVM_SELF_HOSTED_SMOKE=true`, criou o job real com
labels `[self-hosted, civm]`.

Antes de publicar esse contexto, o controlador mediu e executou:

| Instante UTC | Acao | `V:` livre | Contexto publicado |
| --- | --- | ---: | --- |
| `09:39:48` | boot de manutencao | `86 GiB` | vazio |
| `09:43:40` | cleanup + compactacao | `79 -> 87 GiB` | vazio |
| `09:45:42` | boot separado da geracao | `87 GiB` | vazio |
| `09:47:52` | admissao/hold | `80 GiB` | `pr-230@4fa5424...` |

Na admissao, o guest reportou `32 GiB` livres. O runner Linux `2.336.0`
estava `online`, `idle`, com labels `self-hosted,civm`, e o grupo `Default`
autorizava explicitamente `advoq/civm`. Mesmo assim, o endpoint do job da
tentativa rerun permaneceu `queued` e `runner_id=0`; nao houve atribuicao pelo
scheduler do GitHub. Este commit cria um SHA novo no mesmo PR para validar o
dispatch fresco, a cura do SHA anterior e a segunda geracao consecutiva.

WYSIATI: a fronteira e a admissao acima foram observadas; a conclusao do job
self-hosted no SHA novo ainda precisa aparecer verde no proprio PR.

Rollback trigger: restaurar o backup do controlador e manter o gate fechado
se um worker for interrompido, uma geracao publicar abaixo de 80 GiB, o lock
ficar sem owner ou um retry recuperavel deixar de ocorrer.
