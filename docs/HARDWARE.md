# HARDWARE da box civm — fonte da verdade (sem dúvida)

> Medido em 2026-06-23 via PowerShell (host) + WSL + SSH (guest), valores **exatos**.
> Este doc é a referência canônica de hardware. Toda afirmação sobre disco/RAM/CPU da box
> deve bater com os números aqui — número antes de adjetivo (Kahneman #3).
>
> **Por que existe:** houve muita confusão sobre os GB de disco da box (o piso "limpo" do V:,
> o "overhead de 8 GB", se dava pra liberar mais). Este doc trava os fatos para nenhuma
> sessão (IA ou humana) re-derivar errado.

## Host (Windows)

| Recurso | Valor |
| --- | --- |
| **CPU** | AMD **Ryzen 5 3600** — 6 cores / **12 threads**, até **3.95 GHz** |
| **RAM host** | **31.9 GB** (8GB Corsair + 8GB Corsair + 16GB Kllisre @ 3000 MHz) |

### Discos físicos (5 SATA)

| Disco | Tamanho | Tipo | Volume | Livre (snapshot) |
| --- | --- | --- | --- | --- |
| **disk3** | **119.2 GB** | **SSD SATA 128G** | **V: (VM)** — MBR, ocupa o **disco inteiro** | 54.3 GB (VM Running) |
| disk4 | 465.8 GB | SSD Samsung 850 EVO | C: (CANADA) | 148.1 GB |
| disk2 | 223.6 GB | SSD Kingston A400 | I: (ISRAEL) | 69 GB |
| disk1 | 465.8 GB | HDD Hitachi | R: (RUSSIA) | 95.3 GB |
| disk0 | 1397 GB | HDD Samsung HD154UI | E: (ESPANHA) | 66 GB |

> O **V: é um SSD dedicado de 128 GB (119.2 GB úteis), 100% alocado** — a partição já é o disco
> inteiro, então **não dá pra expandir o V: in-place**. Aumentar o headroom do V: exigiria mover o
> VHDX pra um SSD maior (ex.: C: tem 148 GB livres) ou um SSD novo — **decisão tomada: manter o
> atual** ("suficiente dado ao sistema"; o box já roda job pesado, panic floor nunca batido).

## V: (drive da VM) — composição EXATA por estado

O V: hospeda **o VHDX dinâmico da VM + o VMRS (estado de RAM)**. O livre varia com o power-state:

| Estado | VHDX (GB) | VMRS (GB) | Usado (GB) | **V: livre (GB)** |
| --- | --- | --- | --- | --- |
| **VM Off** (pós-`Optimize-VHD`, entre PRs) | 47 | 0 (liberado) | ~47 | **72** |
| **Job start** (VM booted, VHDX ainda não cresceu) | 47 | 8 | ~55 | **~64** |
| **Mid-job** (VHDX cresceu com o job) | 56.8 | 8.0 | 64.9 | **54.3** |

Conta exata mid-job (bate com o explorer): `119.2 − 56.8 (VHDX) − 8.0 (VMRS) − 0.1 = 54.3` livre.

### O "overhead de 8 GB" = VMRS (estado de RAM), NÃO é cruft

- O **VMRS** (`Virtual Machines/<GUID>.VMRS`) é o **estado de runtime/RAM da VM** (a VM tem **8 GB de RAM**).
- O Hyper-V o materializa em **~8 GB enquanto a VM roda** e o **libera quando ela desliga**.
- Por isso o V: livre **oscila ~8 GB** entre Off (72) e Running (54-64). **NÃO é deletável** — eliminá-lo
  exigiria reduzir a RAM da VM (ruim pra job pesado).
- O orquestrador para a VM com **`Stop-VM -Force`** (shutdown limpo, **não save**) — ver
  `deploy/windows/civm-vm-orchestrator.ps1:297` e o comentário em `:274-276` que já documenta o VMRS.

### VHDX (disco da VM)

- Tipo **dinâmico**; arquivo **47 GB (Off, compactado)** ↔ **56.8 GB (Running, expandido pelo job)**.
- Tamanho **virtual máximo = 110 GB** (o guest vê um disco de 110 GB, usa ~38-50 GB pós-pente-fino).
- O `Optimize-VHD` (offline, VM Off) compacta o arquivo de volta ao piso real do uso do guest.

## Guest (VM Ubuntu 24.04)

| Recurso | Valor |
| --- | --- |
| RAM | **8 GB** (= o VMRS) |
| vCPU | 12 (compartilhadas com o host) |
| Disco `/` | 108 GB (dentro do VHDX), **~38 GB usados** pós-pente-fino (era 50; 12 GB de clones stale removidos) |
| Swap | `/swap.img` 4 GB (tipicamente 0 usado) |

## Implicações operacionais (pra não re-derivar errado)

1. **Cada PR começa com ~64 GB livres** no V: (VM booted, VMRS 8 + VHDX 47); cai pra ~54 mid-job; volta
   pra 72 no `boundary_compact` entre PRs. **Isso é saudável** — panic floor (18 GB) nunca batido desde
   o pente-fino de 2026-06-23.
2. **">70 livre durante o job" não é alcançável** no V: atual (teto físico do SSD de 128 GB com VHDX+VMRS).
   Seria preciso mover o VHDX pra um SSD maior — decisão: **não fazer**, o atual é suficiente.
3. O orquestrador é **serial** (1 VM): respeita a fila do GitHub (1 PR por vez) e compacta entre PRs.
   Ver `PAID-CI-PARITY.md` para o modelo de "simulação serializada do CI pago".
