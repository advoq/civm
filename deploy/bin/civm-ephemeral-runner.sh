#!/usr/bin/env bash
# civm-ephemeral-runner.sh <slot>
#
# Loop de runner EPHEMERAL (clean-slate por job) — a maior fidelidade ao CI pago
# que a box self-hosted alcanca sem VM-por-job. A cada volta: registra um runner
# JIT novo (stateless, sem .credentials), roda UM job, faz deep-clean (zera o
# estado do job) e repete. O pago entrega VM efemera nova por job; aqui o efeito
# equivalente e "registro novo + workspace limpo por job" na mesma VM.
#
# IMPORTANTE (honestidade — PAID-CI-PARITY.md): isto fecha o #1 (clean-slate por
# job) mas NAO os 🧱 — o daemon Docker, o disco e o kernel seguem compartilhados
# (dwell cross-job so some com VM-por-job). Custo: cada job comeca com cache local
# frio (o workflow re-hidrata via actions/cache remoto, igual ao pago).
#
# Config vem de /etc/civm/ephemeral-<slot>.env (chmod 600):
#   GH_PAT              token com escopo organization_self_hosted_runners:write
#   GH_ORG              org dona do runner (ex.: acme)
#   RUNNER_DIR          dir do runner instalado (ex.: /home/emdev/actions-runner-acme-org)
#   LABELS              labels CUSTOM, CSV (default: civm) — self-hosted/Linux/X64 sao auto
#   RUNNER_GROUP_ID     grupo do runner (default: 1 = Default)
#   RUNNER_NAME_PREFIX  prefixo do nome JIT (default: civm-<slot>-eph)
set -uo pipefail

SLOT="${1:?uso: civm-ephemeral-runner.sh <slot>}"
ENVFILE="/etc/civm/ephemeral-${SLOT}.env"
# shellcheck disable=SC1090
[ -r "$ENVFILE" ] && . "$ENVFILE"
: "${GH_PAT:?GH_PAT ausente em $ENVFILE}"
: "${GH_ORG:?GH_ORG ausente em $ENVFILE}"
: "${RUNNER_DIR:?RUNNER_DIR ausente em $ENVFILE}"
LABELS="${LABELS:-civm}"
GROUP="${RUNNER_GROUP_ID:-1}"
PREFIX="${RUNNER_NAME_PREFIX:-civm-${SLOT}-eph}"
API="https://api.github.com"

cd "$RUNNER_DIR" || { echo "[eph:$SLOT] RUNNER_DIR inacessivel: $RUNNER_DIR" >&2; exit 1; }

# labels CSV -> array JSON ("civm,foo" -> ["civm","foo"])
labels_json() {
  local out="" l IFS=','
  for l in $LABELS; do out="${out:+$out,}\"$l\""; done
  printf '[%s]' "$out"
}
LJ="$(labels_json)"

# deep-clean entre jobs: zera o _work (preserva _tool/_actions, que sao o
# equivalente da "image pre-assada" do pago — toolchains/actions cacheados, nao
# estado do job anterior), apaga caches locais, prune do docker e fstrim (devolve
# os blocos pro VHDX dinamico). E o enforcement do clean-slate por job.
deep_clean() {
  if [ -d _work ]; then
    find _work -maxdepth 1 -mindepth 1 ! -name _tool ! -name _actions -exec rm -rf {} + 2>/dev/null
  fi
  rm -rf "$HOME"/.cache/* 2>/dev/null
  sudo docker system prune -af --volumes >/dev/null 2>&1
  sudo fstrim -av >/dev/null 2>&1
}

echo "[eph:$SLOT] loop iniciado org=$GH_ORG labels=$LABELS group=$GROUP dir=$RUNNER_DIR"
while true; do
  NAME="${PREFIX}-$(tr -dc 'a-f0-9' < /proc/sys/kernel/random/uuid | cut -c1-8)"
  # 1) minta um JIT config: registro stateless de 1 job, auto-deregistra ao fim.
  RESP="$(curl -fsS -X POST \
      -H "Authorization: Bearer ${GH_PAT}" \
      -H "Accept: application/vnd.github+json" \
      -H "X-GitHub-Api-Version: 2022-11-28" \
      "${API}/orgs/${GH_ORG}/actions/runners/generate-jitconfig" \
      -d "{\"name\":\"${NAME}\",\"runner_group_id\":${GROUP},\"labels\":${LJ},\"work_folder\":\"_work\"}" 2>/dev/null)"
  JIT="$(printf '%s' "$RESP" | python3 -c 'import sys,json;print(json.load(sys.stdin).get("encoded_jit_config",""))' 2>/dev/null)"
  if [ -z "$JIT" ]; then
    echo "[eph:$SLOT] falha ao mintar JIT (resp: $(printf '%s' "$RESP" | tr -d '\n' | head -c 200)) — retry 10s" >&2
    sleep 10
    continue
  fi
  echo "[eph:$SLOT] registrado $NAME — aguardando 1 job"
  # 2) roda UM job e sai (--jitconfig implica ephemeral). Bloqueia ate o job acabar.
  ./run.sh --jitconfig "$JIT" || echo "[eph:$SLOT] run.sh exit $?"
  echo "[eph:$SLOT] job concluido — deep-clean (clean-slate)"
  # 3) clean-slate antes do proximo registro
  deep_clean
done
