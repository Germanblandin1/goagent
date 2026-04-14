#!/usr/bin/env bash
# ============================================================
#   goagent - CI local check (Git Bash / Linux / macOS)
#   Mirrors .github/workflows/ci.yml (push to main)
#
#   Usage:
#     ./check.sh          -- lint + security + test + coverage
#     ./check.sh --full   -- idem + integration tests (necesita Docker)
# ============================================================
set -euo pipefail

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
FAIL=0
RUN_INTEGRATION=0

# En Windows/Git Bash, MinGW GCC necesita paths estilo C:/... (mixed).
# cygpath -m convierte cualquier path a ese formato.
# En Linux/macOS no existe cygpath, devuelve el path tal cual.
mixed_path() {
  if command -v cygpath &>/dev/null; then
    cygpath -m "$1"
  else
    echo "$1"
  fi
}

if [[ "${1:-}" == "--full" ]]; then
  RUN_INTEGRATION=1
fi

PURE_MODULES=(
  mcp
  memory/vector/pgvector
  memory/vector/qdrant
  memory/vector/tiktoken
  otel
  providers/anthropic
  providers/ollama
  providers/voyage
  rag
  ratelimit
)

ALL_MODULES=(
  .
  mcp
  memory/vector/pgvector
  memory/vector/qdrant
  memory/vector/sqlitevec
  memory/vector/tiktoken
  otel
  providers/anthropic
  providers/ollama
  providers/voyage
  rag
  ratelimit
)

# ── helpers ────────────────────────────────────────────────
ok()   { echo "[ OK ] $*"; }
fail() { echo "[FAIL] $*"; FAIL=$((FAIL + 1)); }
skip() { echo "[SKIP] $*"; }
sep()  { echo "============================================================"; }

run() {
  # run <label> <cmd...>: ejecuta el comando y registra OK/FAIL
  local label="$1"; shift
  if "$@"; then
    ok "$label"
  else
    fail "$label"
  fi
}

check_coverage() {
  local file="$1"
  local threshold="$2"
  local label="$3"

  if [[ ! -f "$file" ]]; then
    skip "$label (sin archivo de coverage)"
    return
  fi

  local pct
  pct=$(go tool cover -func="$file" | tail -1 | awk '{print $NF}' | tr -d '%')

  local ok_flag
  ok_flag=$(awk -v p="$pct" -v t="$threshold" 'BEGIN { print (p+0 >= t+0) ? "yes" : "no" }')

  if [[ "$ok_flag" == "yes" ]]; then
    printf "[ OK ] %-40s %s%% (>=%s%%)\n" "$label" "$pct" "$threshold"
  else
    printf "[FAIL] %-40s %s%% (required >=%s%%)\n" "$label" "$pct" "$threshold"
    FAIL=$((FAIL + 1))
  fi
}

# ── tools ──────────────────────────────────────────────────
sep
echo "  goagent - CI local check"
sep
echo "  Root: $ROOT"
if [[ $RUN_INTEGRATION -eq 1 ]]; then
  echo "  Modo: completo (incluye integration tests)"
else
  echo "  Modo: estandar (sin integration tests — usar --full)"
fi
sep
echo

echo "[tools] Verificando herramientas..."

if ! command -v staticcheck &>/dev/null; then
  echo "[tools] Instalando staticcheck..."
  go install honnef.co/go/tools/cmd/staticcheck@latest
fi

if ! command -v govulncheck &>/dev/null; then
  echo "[tools] Instalando govulncheck..."
  go install golang.org/x/vuln/cmd/govulncheck@latest
fi

ok "tools"
echo

# ============================================================
# STEP 1 — LINT (go vet + staticcheck)
# ============================================================
sep
echo "  STEP 1/4 - LINT"
sep

# Disable set -e inside loops so one failure doesn't abort everything
set +e

echo "[lint] root module"
cd "$ROOT"
run "go vet root"       go vet ./...
run "staticcheck root"  staticcheck ./...

for mod in "${PURE_MODULES[@]}"; do
  echo "[lint] $mod"
  pushd "$ROOT/$mod" > /dev/null
  run "go vet $mod"      go vet ./...
  run "staticcheck $mod" staticcheck ./...
  popd > /dev/null
done

echo "[lint] examples (vet only)"
pushd "$ROOT/examples" > /dev/null
run "go vet examples" go vet ./...
popd > /dev/null

echo "[lint] sqlitevec (CGO)"
pushd "$ROOT/memory/vector/sqlitevec" > /dev/null
MOD_CACHE=$(mixed_path "$(go env GOMODCACHE)")
ROOT_M=$(mixed_path "$ROOT")
SQLITE3_VER=$(go list -m -f '{{.Version}}' github.com/mattn/go-sqlite3)
export CGO_ENABLED=1
export CGO_CFLAGS="-I${ROOT_M}/memory/vector/sqlitevec/csrc -I${MOD_CACHE}/github.com/mattn/go-sqlite3@${SQLITE3_VER}"
run "go vet sqlitevec"      go vet ./...
run "staticcheck sqlitevec" staticcheck ./...
unset CGO_ENABLED CGO_CFLAGS
popd > /dev/null

echo

# ============================================================
# STEP 2 — SECURITY (govulncheck)
# ============================================================
sep
echo "  STEP 2/4 - SECURITY"
sep

for mod in "${ALL_MODULES[@]}"; do
  echo "[vuln] $mod"
  if [[ "$mod" == "." ]]; then
    pushd "$ROOT" > /dev/null
  else
    pushd "$ROOT/$mod" > /dev/null
  fi
  run "govulncheck $mod" govulncheck ./...
  popd > /dev/null
done

echo

# ============================================================
# STEP 3 — TEST (race detector + coverage)
# ============================================================
sep
echo "  STEP 3/4 - TEST"
sep

cd "$ROOT"
rm -f coverage-*.out

echo "[test] root module"
cd "$ROOT"
run "build root" go build ./...

# Filtrar paquetes sin test files (evita el error de covdata en Windows)
ROOT_PKGS=$(go list -f '{{if or .TestGoFiles .XTestGoFiles}}{{.ImportPath}}{{end}}' ./... | tr '\n' ' ')
# shellcheck disable=SC2086
run "test root" go test -race -timeout 5m -coverprofile=coverage-root.out -covermode=atomic $ROOT_PKGS

for mod in "${PURE_MODULES[@]}"; do
  echo "[test] $mod"
  pushd "$ROOT/$mod" > /dev/null
  covfile="$ROOT/coverage-$(echo "$mod" | tr '/' '-').out"
  run "build $mod" go build ./...
  run "test $mod"  go test -race -timeout 5m -coverprofile="$covfile" -covermode=atomic ./...
  popd > /dev/null
done

echo "[test] sqlitevec (CGO)"
pushd "$ROOT/memory/vector/sqlitevec" > /dev/null
MOD_CACHE=$(mixed_path "$(go env GOMODCACHE)")
ROOT_M=$(mixed_path "$ROOT")
SQLITE3_VER=$(go list -m -f '{{.Version}}' github.com/mattn/go-sqlite3)
export CGO_ENABLED=1
export CGO_CFLAGS="-I${ROOT_M}/memory/vector/sqlitevec/csrc -I${MOD_CACHE}/github.com/mattn/go-sqlite3@${SQLITE3_VER}"
run "build sqlitevec" go build ./...
run "test sqlitevec"  go test -race -timeout 5m \
  -coverprofile="$ROOT/coverage-memory-vector-sqlitevec.out" \
  -covermode=atomic ./...
unset CGO_ENABLED CGO_CFLAGS
popd > /dev/null

echo

# ============================================================
# STEP 4 — COVERAGE (threshold enforcement)
# ============================================================
sep
echo "  STEP 4/4 - COVERAGE"
sep

cd "$ROOT"

# Core: >=80%
check_coverage coverage-root.out  80 "core"
check_coverage coverage-rag.out   80 "rag"

# Sub-packages: >=70%
check_coverage coverage-mcp.out                          70 "mcp"
check_coverage coverage-otel.out                         70 "otel"
check_coverage coverage-ratelimit.out                    70 "ratelimit"
check_coverage coverage-providers-anthropic.out          70 "providers/anthropic"
check_coverage coverage-providers-ollama.out             70 "providers/ollama"
check_coverage coverage-providers-voyage.out             70 "providers/voyage"
# pgvector y qdrant son paquetes de integración — casi todo su código
# requiere una base de datos real. El job integration es su gate de calidad.
skip "memory/vector/pgvector (integration-only package)"
skip "memory/vector/qdrant   (integration-only package)"
check_coverage coverage-memory-vector-sqlitevec.out      70 "memory/vector/sqlitevec"
check_coverage coverage-memory-vector-tiktoken.out       70 "memory/vector/tiktoken"

echo

# ============================================================
# STEP 5 — INTEGRATION (opcional, requiere Docker)
# ============================================================
if [[ $RUN_INTEGRATION -eq 0 ]]; then
  echo "[integration] Saltado. Usar ./check.sh --full para correrlos."
  echo
else
  sep
  echo "  STEP 5 - INTEGRATION (testcontainers)"
  sep

  echo "[integration] pgvector"
  pushd "$ROOT/memory/vector/pgvector" > /dev/null
  run "integration pgvector" go test -race -timeout 10m -tags integration ./...
  popd > /dev/null

  echo "[integration] qdrant"
  pushd "$ROOT/memory/vector/qdrant" > /dev/null
  run "integration qdrant" go test -race -timeout 10m -tags integration ./...
  popd > /dev/null

  echo
fi

# ============================================================
# RESUMEN
# ============================================================
sep
if [[ $FAIL -eq 0 ]]; then
  echo "  LISTO PARA PUSH - todos los checks OK"
  echo
  echo "  Proximo paso:"
  echo "    git tag -a vX.X.X -m \"release: vX.X.X\""
  echo "    git push origin vX.X.X"
else
  echo "  NO LISTO - $FAIL check(s) fallaron"
fi
sep
echo

exit $FAIL
