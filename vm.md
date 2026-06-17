# VM — gha-ubuntu-2404 (civm CI runner)

> Inventário da máquina virtual que hospeda os self-hosted runners do civm.
> Mantenha atualizado a cada mudança de toolchain/SO ou limpeza grande.
>
> **Última atualização completa: 2026-06-17** — `apt update && upgrade` (primeira
> vez, alinhando ao github-hosted Ubuntu 24.04) + full clean (todos os caches +
> imagens) + compactação do VHDX.

## Host & hardware

- **Hypervisor:** Hyper-V no host Windows (EMEDEV). Acesso elevado via Windows
  `sudo` (UAC off) — Get-VM/Optimize-VHD/Start/Stop funcionam.
- **VHDX:** dinâmico em `V:\Hyper-V\gha-ubuntu-2404\Virtual Hard Disks\gha-ubuntu-2404.vhdx`.
  O volume **V: (119 GB) é o teto real de disco** — o VHDX cresce nele.
- **vCPU:** 12 · **RAM:** 7 GiB (Hyper-V segura essa RAM enquanto a VM está ligada;
  scale-to-zero a devolve ao Windows quando ociosa) · **Disco do guest:** 108 GB.

## Sistema

| | |
| --- | --- |
| OS | Ubuntu 24.04.4 LTS |
| Kernel | 6.8.0-124-generic |
| apt lists atualizado | 2026-06-17 04:46 |
| dpkg última mudança | 2026-06-17 04:49 |

## Toolchains globais

| Ferramenta | Versão |
| --- | --- |
| Go | go1.26.3 |
| Node (default via nvm) | v24.15.0 |
| npm | 11.13.0 (global: 11.12.1) |
| yarn | 1.22.22 (Classic / v1) |
| Python | 3.12.3 |
| Docker | 29.1.3 |
| git | presente |

## nvm + Node (multi-versão)

- **nvm:** instalado (`~/.nvm`).
- **Versões de Node instaladas:** v4.9.1 · v6.17.1 · v8.17.0 · v10.24.1 ·
  v12.22.12 · v14.21.3 · v16.20.2 · v18.20.8 · v20.20.2 · v22.22.2 · v24.14.1 ·
  v24.15.0 · v24.16.0.
- **Default:** v24.15.0 (mais recente instalada: **v24.16.0**).
- Jobs com `actions/setup-node` baixam/usam a versão pedida sob
  `~/actions-runner-*/_work/_tool/node`.

## npm globais

- `corepack@0.34.6`
- `npm@11.12.1`

## Runners (multi-repo — NÃO é dedicada ao advoq)

A VM hospeda runners self-hosted para **7 repos**, todos compartilhando a mesma
box (CPU/RAM/disco/daemon Docker):

`advoq` · `advoq-org` · `advoqwhatsappapi` · `chatwoot-realtime` ·
`n8n-engine` · `typebot-runtime` · `vitae`

Cada um com seu `~/actions-runner-{repo}/` e cache yarn escopado
(`~/.cache/yarn-{repo}-*`), então uma limpeza pode ser escopada por repo.

## Disco — modelo de limpeza

- **Estado limpo alvo (por PR):** **~51 GB livres** no guest (full clean: zera
  `~/.cache/*` + `docker system prune -af --volumes`).
- **VHDX:** dinâmico; só encolhe com `Optimize-VHD -Mode Full` **com a VM off +
  `Mount-VHD -ReadOnly`** (sem o mount, o Optimize vira no-op — confirmado
  2026-06-17). Devolve espaço ao V:.
- **Hygiene contínua (deployada):**
  - `civmctl-buildcache-prune.timer` (3 min) → build cache (`builder prune -af`)
    + imagens de service de runs finalizadas (`advoq-org-{runid}-*`),
    BuildKit/container-safe, sem deferir ao heavy-lock.
  - `civmctl-disk-watchdog` + `civmctl-cleanup` → hygiene geral.
  - `civm-vhdx-autoreclaim` (host) → compacta o VHDX quando V: baixo + guest idle.
- **Scale-to-zero (orchestrator):** `deploy/windows/civm-vm-orchestrator.ps1` —
  liga a VM sob demanda (job na fila) e, na fronteira de cada PR (idle ≥ N min),
  faz full clean + Stop-VM + Optimize-VHD, devolvendo **RAM e disco ao Windows**
  entre rajadas.
