# Higiene Cognitiva & Comunicação Intencional

> Portado do "ouro" de engenharia do advoq (superprompt de Auditoria de Ruído
> Arquitetural / Kahneman + DDD), **curado para o civm**: mantém só as regras
> gerais de engenharia Go/infra e descarta os specifics de negócio do advoq
> (multi-tenant, FSD/Next.js, RLS, AIGateway). Complementa — não substitui —
> [KAHNEMAN-DISCIPLINES.md](KAHNEMAN-DISCIPLINES.md), [INVARIANTS.md](INVARIANTS.md)
> e [SSDV3-PROMPTS.md](SSDV3-PROMPTS.md).

## Princípio

O código executável e os testes são a **principal fonte de verdade e
documentação** do sistema. O objetivo não é micro-otimizar performance, é
**reduzir Ruído** (variabilidade indesejada, inconsistência) e a carga do
**Sistema 2** (esforço analítico denso) onde o **Sistema 1** (leitura intuitiva
e previsível) deveria bastar. O Sistema 1 não lê código — ele _escaneia_.

Um código não pode só "funcionar" (fazer a coisa certa); ele tem que **"dizer a
coisa certa"**, transmitindo as regras e a linguagem do domínio.

## Regras (aplicáveis ao civm — Go puro, infra, tooling)

### 1. Comunicação Intencional (nomes)
- Nomes de variáveis, funções e métodos revelam a intenção sem ambiguidade e
  contam uma história contínua.
- Elimine booleanos negativos/ambíguos: `isNotReady` → `isPending`,
  `notFound` → `missing`. Positivos afirmados primeiro.

### 2. Limites cognitivos estritos (Go)
- Máx. **7 parâmetros** por função; complexidade cognitiva alvo **≤ 15**.
- Funções grandes têm seus laços internos extraídos para **helpers nomeados e
  testáveis** — o laço vira um verbo do domínio, não um bloco anônimo.

### 3. Early returns / guard clauses
- Combata _deep nesting_ de `if/else`. A leitura flui como linguagem natural:
  trate as falhas no topo (guard clauses), matando fluxos de erro cedo e
  deixando o "caminho feliz" alinhado à esquerda. Substitua `else` por retorno
  antecipado sempre que possível.

### 4. Modelo rico vs. anêmico (DDD em Go)
- Não misture lógica de domínio anexada a structs (`func (e *Model) Acao()`)
  com helpers anêmicos procedurais na camada de serviço (`func FazAcao(e *Model)`).
- Lógica rica e pertinente ao modelo vive no **receiver method** do struct.

### 5. Testes como documentação
- Assertions rigorosas. A organização e a nomenclatura dos testes servem como
  documentação viva do design e do tratamento de exceções (incl. casos de erro).

### 6. Tooling "Puro Go" (anti troca-de-contexto)
- Scripts soltos em `.mjs`/`.sh`/PowerShell complexos são **Ruído**: forçam
  trocar o contexto mental de Go para Node/Bash/PS. Prefira ferramentas em Go
  (`go run`, subcomando do `civmctl`) e Makefiles chamando Go.
- _Nota civm:_ a camada host (Hyper-V) é inevitavelmente PowerShell
  (`deploy/windows/*.ps1`); mantenha-a **fina e guardada por teste Go**
  (ex.: o lint de `[math]::Max` em `internal/hostdisk`), com a lógica de decisão
  no binário Go sempre que possível.

### 7. Higiene de decisão (Kahneman)
- Nenhuma afirmação sem métrica: diga "p99 caiu para 90ms", não "ficou mais
  rápido". Toda decisão não-trivial precisa de um **Counterfactual** (gatilho
  numérico explícito de rollback). Ver [KAHNEMAN-DISCIPLINES.md](KAHNEMAN-DISCIPLINES.md).

## Estratégia de execução (anti-falácia do planejamento)

**Nunca** refatore múltiplos padrões / dezenas de arquivos de uma vez — o
Sistema 1 é otimista demais. Adote **Micro-Slicing**:

- Uma **fatia ortogonal por vez** (um padrão, um pacote).
- Cada fatia vira um **commit atômico**.
- Valide e teste a fatia **antes** de seguir para a próxima.

Quando o ruído for **estrutural** (refatoração pesada / quebra de contrato),
**não escreva código direto**: acione a esteira completa
[SSDV3-PROMPTS.md](SSDV3-PROMPTS.md) (PRD → SPEC → Passo 2.5 Red-Team → SPECv2 →
Passo 2.5 de novo → SPECv3).

## Entregável de uma auditoria

1. **Diagnóstico de Ruído** — liste os "ofensores do Sistema 1" encontrados.
2. **Código de Higiene** — o snippet refatorado (coeso, determinístico, que
   "diz a coisa certa"), com testes.
3. **Esteira SSDV3** — apenas quando o ruído for estrutural.

## Leituras cruzadas
- [KAHNEMAN-DISCIPLINES.md](KAHNEMAN-DISCIPLINES.md) — as 13 disciplinas + counterfactual como gatekeeper.
- [INVARIANTS.md](INVARIANTS.md) — invariantes de CI.
- [SSDV3-PROMPTS.md](SSDV3-PROMPTS.md) — esteira PRD→SPECv3.
- `AGENTS.md` §"Decision hygiene (Kahneman)".
