# HARDWARE da box civm — fonte da verdade

> Revalidado em 30/07/2026 via métricas do host e SSH read-only no guest.
> Valores de capacidade usam **GiB** (`2^30 bytes`), mesmo quando interfaces
> exibem `GB`. Bytes exatos aparecem quando relevantes.
>
> Este documento separa 3 medidas que não podem ser comparadas como se fossem
> uma só: espaço livre no `V:` do host, tamanho físico/lógico do VHDX e espaço
> livre no filesystem `/` do guest.

## Baseline atual

| Camada | Estado medido | Capacidade | Livre/arquivo |
| --- | --- | ---: | ---: |
| Host `V:` | VM `Running`, `2026-07-30T06:17:04Z` | `119,24 GiB` | `73,19 GiB` livres |
| VHDX | VM `Running`, mesma coleta | máximo lógico `40 GiB` | arquivo ≈`39 GiB` |
| Guest `sda` | VM `Running`, mesma coleta | `40 GiB` | n/a |
| Guest `/` | VM `Running`, mesma coleta | `37,70 GiB` | `14,03 GiB` disponíveis (`63%` usado) |
| Docker guest | VM `Running`, mesma coleta | `174` volumes | `10,44 GB`, `100%` recuperável |

Bytes exatos do guest: `/dev/sda1` expõe `40.483.942.400` bytes de
filesystem, dos quais `15.062.634.496` bytes estavam disponíveis. O bloco
`sda` expõe `42.949.672.960` bytes.

### Referência Off/compactado

Em 30/07/2026 às `06:33Z`, após o boundary natural de `main@1ae4e29e`,
a VM ficou `Off` e o `V:` atingiu `80,50 GiB` livres. A medição confirma a
referência de 25/07/2026 (`80,32 GiB`, após remover o checkpoint diferencial
de `28,54 GiB`) para a geometria atual:

- **aproximadamente `80 GiB` livres no `V:`** significa host desligado e VHDX
  compactado;
- não significa `80 GiB` livres dentro do guest — o disco inteiro do guest
  mede `40 GiB`;
- `92 GiB` livres foi uma observação histórica em 15/07/2026, antes das
  mudanças posteriores da cadeia/geometria. Não é o baseline atual.

O gate funcional continua usando os thresholds versionados, não uma comparação
frágil com `80`: `AdmitFloorGb=55`, `GuestFloorGb=40`,
`WarnFloorGb=28` e `PanicFloorGb=18`. A referência de `80 GiB` serve para
detectar drift após uma fronteira limpa, não para substituir esses gates.

## Host (Windows)

| Recurso | Valor |
| --- | --- |
| CPU | AMD Ryzen 5 3600 — `6` cores / `12` threads, até `3,95 GHz` |
| RAM host | `31,9 GB` |
| Disco da VM | SSD SATA 128G; volume `V:` com `119,24 GiB` úteis |

O `V:` ocupa o disco físico inteiro. Aumentar sua capacidade exige mover o VHDX
para outro disco ou trocar o SSD; software de limpeza não amplia o teto físico.

## Composição do `V:`

O `V:` contém principalmente:

- o arquivo VHDX dinâmico;
- o VMRS de aproximadamente `8 GiB` enquanto a VM está ligada;
- metadados do Hyper-V/filesystem.

Desligar a VM libera o VMRS. `fstrim` informa ao host quais blocos do guest
podem ser descartados, e `Optimize-VHD` offline pode reduzir o arquivo físico.
Nenhuma dessas operações remove dados ainda alocados no guest.

## Guest (Ubuntu 24.04)

| Recurso | Valor atual |
| --- | --- |
| RAM | `8 GiB` |
| vCPU | `12` |
| Disco virtual | `40 GiB` |
| Filesystem `/` | `37,70 GiB`; `23,66 GiB` usados; `14,03 GiB` disponíveis |
| Docker | `0` imagens, `0` containers, `0` build cache, `174` volumes inativos |

Dos `174` volumes, ao menos `171` são volumes nomeados de Docker Compose com
projeto no formato `advoq-org-<run-id>`; `3` são anônimos sem labels. O comando
atual `docker volume prune -f` não remove os named nessa versão do Docker, razão
pela qual uma execução reportada como limpeza deixou `10,44 GB` para trás.

## Snapshots históricos, não baselines atuais

Em 23/06/2026, a documentação registrou VHDX virtual de `110 GB`, guest `/` de
`108 GB`, arquivo VHDX de `47–56,8 GB` e `V:` com `54–72 GB` livres conforme o
power-state. Esses números descrevem a topologia daquela data e foram
substituídos pela medição de 30/07/2026.

Preservar o histórico evita re-derivar a conclusão errada de que a VM atual
deveria ter `80–90 GB` livres internamente.

## Protocolo de medição

Após mudança de disco, checkpoint, limpeza ou compactação, registrar as
3 camadas no mesmo instante:

1. host: tamanho/livre de `V:` e estado da VM;
2. VHDX: tamanho do arquivo, máximo lógico, mínimo e checkpoints;
3. guest: `lsblk -b`, `df -B1 /` e `docker system df`.

Uma fronteira só pode ser chamada de limpa quando a limpeza do guest, o
`fstrim`, a compactação e a prontidão do runner tiverem evidências separadas.
