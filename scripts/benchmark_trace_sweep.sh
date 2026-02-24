#!/usr/bin/env bash
# Benchmark trace-budget sweeps for the commercial NES corpus.
#
# This script runs retrodisasm verification over selected ROM groups/mappers while
# sweeping trace parameters. It outputs:
# 1) per-run CSV results
# 2) aggregated pass/fail totals by (mapper, trace_mode, max_instr, max_visits, max_branch)
#
# Usage examples:
#   scripts/benchmark_trace_sweep.sh
#   scripts/benchmark_trace_sweep.sh -g notworking -m 1,2 -b 0,128,256
#   scripts/benchmark_trace_sweep.sh -g working -m 7 -i 200000 -v 32 -b 0,256
#
# Environment:
#   RETRODISASM_BIN=/path/to/retrodisasm  # optional (defaults to: go run .)

set -u

ASSEMBLER="ca65"
GROUP="notworking" # all|working|notworking
MAPPERS_CSV="1,2"
TRACE_MODE="hybrid"
MAX_INSTR_CSV="200000"
MAX_VISITS_CSV="8,32"
MAX_BRANCH_CSV="0,64,128,256"
ARTIFACT_DIR=""
MAX_ASM_ERROR_CONTEXTS=40

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
REPO_ROOT="$(cd "${SCRIPT_DIR}/.." && pwd)"
CORPUS_DIR="${REPO_ROOT}/internal/testroms/commercial"
OUTPUT_CSV="${REPO_ROOT}/trace_sweep_${ASSEMBLER}.csv"

usage() {
    cat <<EOF
Usage: $(basename "$0") [-a assembler] [-g group] [-m mappers] [-t trace_mode] [-i max_instr_csv] [-v max_visits_csv] [-b max_branch_csv] [-o output_csv] [-d artifact_dir]

Options:
  -a assembler      Assembler for -verify (default: ca65)
  -g group          ROM group: all|working|notworking (default: notworking)
  -m mappers        Comma-separated mapper filter (default: 1,2)
  -t trace_mode     Trace mode: static|emu|hybrid (default: hybrid)
  -i max_instr_csv  Comma-separated max instruction budgets (default: 200000)
  -v max_visits_csv Comma-separated max visits budgets (default: 8,32)
  -b max_branch_csv Comma-separated max branch-state budgets (default: 0,64,128,256)
  -o output_csv     Output CSV path (default: ${REPO_ROOT}/trace_sweep_<assembler>.csv)
  -d artifact_dir   Optional directory for per-failure artifacts
EOF
}

while getopts ":a:g:m:t:i:v:b:o:d:h" opt; do
    case "$opt" in
        a) ASSEMBLER="$OPTARG" ;;
        g) GROUP="$OPTARG" ;;
        m) MAPPERS_CSV="$OPTARG" ;;
        t) TRACE_MODE="$OPTARG" ;;
        i) MAX_INSTR_CSV="$OPTARG" ;;
        v) MAX_VISITS_CSV="$OPTARG" ;;
        b) MAX_BRANCH_CSV="$OPTARG" ;;
        o) OUTPUT_CSV="$OPTARG" ;;
        d) ARTIFACT_DIR="$OPTARG" ;;
        h)
            usage
            exit 0
            ;;
        :)
            echo "Missing argument for -$OPTARG" >&2
            usage
            exit 1
            ;;
        \?)
            echo "Unknown option: -$OPTARG" >&2
            usage
            exit 1
            ;;
    esac
done

case "$GROUP" in
    all|working|notworking) ;;
    *)
        echo "Invalid group: $GROUP (expected all|working|notworking)" >&2
        exit 1
        ;;
esac

case "$TRACE_MODE" in
    static|emu|hybrid) ;;
    *)
        echo "Invalid trace mode: $TRACE_MODE (expected static|emu|hybrid)" >&2
        exit 1
        ;;
esac

if [[ -n "${RETRODISASM_BIN:-}" ]]; then
    RUNNER=("${RETRODISASM_BIN}")
else
    RUNNER=(go run .)
fi

verify_rom() {
    local rom="$1"
    local log_file="$2"
    local trace_mode="$3"
    local max_instr="$4"
    local max_visits="$5"
    local max_branch="$6"

    "${RUNNER[@]}" -verify -q -a "$ASSEMBLER" -s nes \
        -trace-mode "$trace_mode" \
        -trace-max-instr "$max_instr" \
        -trace-max-visits-per-state "$max_visits" \
        -trace-max-branch-states "$max_branch" \
        -o "${tmp_dir}/out.asm" "$rom" >"$log_file" 2>&1
    local rc=$?
    if [[ $rc -ne 0 ]]; then
        return 1
    fi

    # Some flows may log verification failure while returning exit code 0.
    if grep -Eiq "Disassembling failed|verification failed" "$log_file"; then
        return 1
    fi

    return 0
}

classify_failure() {
    local log_file="$1"

    if grep -Eiq "unexpected EOF|failed reading PRG data|could not read cartridge|invalid iNES|failed reading cartridge" "$log_file"; then
        echo "input_corrupt"
        return 0
    fi
    if grep -Eiq "Range error" "$log_file"; then
        echo "assembler_range"
        return 0
    fi
    if grep -Eiq "segment PRG mismatch|Offset mismatch" "$log_file"; then
        echo "prg_mismatch"
        return 0
    fi
    if grep -Eiq "verification failed" "$log_file"; then
        echo "verification_failed"
        return 0
    fi
    if grep -Eiq "Disassembling failed" "$log_file"; then
        echo "disasm_failed"
        return 0
    fi
    echo "unknown"
}

sanitize_name() {
    local name="$1"
    # Keep names filesystem-safe and deterministic.
    name="$(echo "$name" | tr ' ' '_' | tr -cd '[:alnum:]_.-')"
    if [[ -z "$name" ]]; then
        name="unknown"
    fi
    echo "$name"
}

write_assembler_error_artifacts() {
    local log_file="$1"
    local asm_file="$2"
    local artifact_path="$3"

    local errors_file summary_file context_file
    errors_file="${artifact_path}/assembler_errors.txt"
    summary_file="${artifact_path}/assembler_error_summary.txt"
    context_file="${artifact_path}/assembler_error_context.txt"

    rg -o 'out\.asm\([0-9]+\): Error: [^\\"]+' "$log_file" > "$errors_file" || true
    if [[ ! -s "$errors_file" ]]; then
        rm -f "$errors_file" "$summary_file" "$context_file"
        return 0
    fi

    awk -F': Error: ' '{print $2}' "$errors_file" | sort | uniq -c | sort -nr > "$summary_file"

    if [[ ! -f "$asm_file" ]]; then
        return 0
    fi

    : > "$context_file"
    local count=0
    while IFS= read -r err; do
        local line start end
        line="$(echo "$err" | sed -E 's/.*out\.asm\(([0-9]+)\).*/\1/')"
        if [[ -z "$line" ]]; then
            continue
        fi

        start=$(( line - 3 ))
        if (( start < 1 )); then
            start=1
        fi
        end=$(( line + 3 ))

        {
            echo "=== ${err} ==="
            nl -ba "$asm_file" | sed -n "${start},${end}p"
            echo
        } >> "$context_file"

        count=$((count + 1))
        if (( count >= MAX_ASM_ERROR_CONTEXTS )); then
            echo "... truncated after ${MAX_ASM_ERROR_CONTEXTS} contexts ..." >> "$context_file"
            break
        fi
    done < "$errors_file"
}

write_failure_artifacts() {
    local rom="$1"
    local rom_name="$2"
    local max_instr="$3"
    local max_visits="$4"
    local max_branch="$5"
    local failure_class="$6"
    local log_file="$7"

    if [[ -z "$ARTIFACT_DIR" ]]; then
        echo ""
        return 0
    fi

    local rom_stem cfg_slug artifact_path
    rom_stem="$(sanitize_name "${rom_name%.nes}")"
    cfg_slug="mode-${TRACE_MODE}_i${max_instr}_v${max_visits}_b${max_branch}"
    artifact_path="${ARTIFACT_DIR}/${rom_stem}/${cfg_slug}"
    mkdir -p "$artifact_path"

    cp "$log_file" "${artifact_path}/verify.log"

    if [[ -f "${tmp_dir}/out.asm" ]]; then
        cp "${tmp_dir}/out.asm" "${artifact_path}/disasm.asm"
        rg -n '^[A-Za-z_.][A-Za-z0-9_.]*:' "${artifact_path}/disasm.asm" > "${artifact_path}/labels.txt" || true
        write_assembler_error_artifacts "$log_file" "${artifact_path}/disasm.asm" "$artifact_path"
    else
        write_assembler_error_artifacts "$log_file" "" "$artifact_path"
    fi

    rg -n "Offset mismatch|verification failed|Disassembling failed" "$log_file" > "${artifact_path}/mismatch_offsets.txt" || true
    cat > "${artifact_path}/meta.txt" <<EOF
rom=${rom}
rom_name=${rom_name}
trace_mode=${TRACE_MODE}
max_instr=${max_instr}
max_visits=${max_visits}
max_branch=${max_branch}
assembler=${ASSEMBLER}
failure_class=${failure_class}
EOF

    echo "$artifact_path"
}

collect_roms() {
    case "$GROUP" in
        working)
            find "${CORPUS_DIR}/working" -maxdepth 1 -type f -name '*.nes' -print0
            ;;
        notworking)
            find "${CORPUS_DIR}/notworking" -maxdepth 1 -type f -name '*.nes' -print0
            ;;
        all)
            find "${CORPUS_DIR}/working" -maxdepth 1 -type f -name '*.nes' -print0
            find "${CORPUS_DIR}/notworking" -maxdepth 1 -type f -name '*.nes' -print0
            ;;
    esac
}

mapper_from_rom() {
    local rom="$1"
    local b6 b7
    b6="$(od -An -j6 -N1 -tu1 "$rom" | tr -d ' ')"
    b7="$(od -An -j7 -N1 -tu1 "$rom" | tr -d ' ')"
    echo $(( ((b7 & 240)) | ((b6 >> 4) & 15) ))
}

group_from_path() {
    local rom="$1"
    if [[ "$rom" == *"/working/"* ]]; then
        echo "working"
    else
        echo "notworking"
    fi
}

is_non_negative_int() {
    [[ "$1" =~ ^[0-9]+$ ]]
}

in_csv_list() {
    local needle="$1"
    local csv="$2"
    local item
    IFS=',' read -r -a items <<< "$csv"
    for item in "${items[@]}"; do
        if [[ "$item" == "$needle" ]]; then
            return 0
        fi
    done
    return 1
}

validate_csv_ints() {
    local csv="$1"
    local label="$2"
    local item
    IFS=',' read -r -a items <<< "$csv"
    if [[ ${#items[@]} -eq 0 ]]; then
        echo "Empty list for ${label}" >&2
        exit 1
    fi
    for item in "${items[@]}"; do
        if ! is_non_negative_int "$item"; then
            echo "Invalid integer '${item}' in ${label}" >&2
            exit 1
        fi
    done
}

validate_csv_ints "$MAPPERS_CSV" "mappers"
validate_csv_ints "$MAX_INSTR_CSV" "max instructions"
validate_csv_ints "$MAX_VISITS_CSV" "max visits"
validate_csv_ints "$MAX_BRANCH_CSV" "max branch states"

mkdir -p "$(dirname "$OUTPUT_CSV")"
echo "rom,set,mapper,trace_mode,max_instr,max_visits,max_branch,status,failure_class,duration_ms,artifact_path" > "$OUTPUT_CSV"

if [[ -n "$ARTIFACT_DIR" ]]; then
    mkdir -p "$ARTIFACT_DIR"
fi

tmp_dir="$(mktemp -d)"
trap 'rm -rf "$tmp_dir"' EXIT
mkdir -p "${tmp_dir}/gocache"

# Keep go build cache inside writable temp space (important for sandboxed runs).
export GOCACHE="${tmp_dir}/gocache"

declare -A PASS FAIL TOTAL DURATION FAIL_CLASS_BY_CFG

declare -a ROMS
while IFS= read -r -d '' rom; do
    mapper="$(mapper_from_rom "$rom")"
    if in_csv_list "$mapper" "$MAPPERS_CSV"; then
        ROMS+=("$rom")
    fi
done < <(collect_roms | sort -z)

if [[ ${#ROMS[@]} -eq 0 ]]; then
    echo "No ROMs found for group '$GROUP' and mappers '$MAPPERS_CSV'." >&2
    exit 1
fi

echo "Benchmarking trace sweep..."
echo "  group:          $GROUP"
echo "  mapper filter:  $MAPPERS_CSV"
echo "  assembler:      $ASSEMBLER"
echo "  trace mode:     $TRACE_MODE"
echo "  max instr:      $MAX_INSTR_CSV"
echo "  max visits:     $MAX_VISITS_CSV"
echo "  max branch:     $MAX_BRANCH_CSV"
echo "  output csv:     $OUTPUT_CSV"
echo "  rom count:      ${#ROMS[@]}"
echo ""

cd "$REPO_ROOT" || exit 1

IFS=',' read -r -a MAX_INSTR_LIST <<< "$MAX_INSTR_CSV"
IFS=',' read -r -a MAX_VISITS_LIST <<< "$MAX_VISITS_CSV"
IFS=',' read -r -a MAX_BRANCH_LIST <<< "$MAX_BRANCH_CSV"

for max_instr in "${MAX_INSTR_LIST[@]}"; do
    for max_visits in "${MAX_VISITS_LIST[@]}"; do
        for max_branch in "${MAX_BRANCH_LIST[@]}"; do
            for rom in "${ROMS[@]}"; do
                set_name="$(group_from_path "$rom")"
                mapper="$(mapper_from_rom "$rom")"
                rom_name="$(basename "$rom")"
                key="${mapper},${TRACE_MODE},${max_instr},${max_visits},${max_branch}"

                start_ms="$(date +%s%3N)"
                log_file="${tmp_dir}/run.log"
                if verify_rom "$rom" "$log_file" "$TRACE_MODE" "$max_instr" "$max_visits" "$max_branch"; then
                    status="pass"
                    failure_class=""
                    artifact_path=""
                    PASS["$key"]=$(( ${PASS["$key"]:-0} + 1 ))
                else
                    status="fail"
                    failure_class="$(classify_failure "$log_file")"
                    artifact_path="$(write_failure_artifacts "$rom" "$rom_name" "$max_instr" "$max_visits" "$max_branch" "$failure_class" "$log_file")"
                    FAIL["$key"]=$(( ${FAIL["$key"]:-0} + 1 ))
                    FAIL_CLASS_BY_CFG["${key},${failure_class}"]=$(( ${FAIL_CLASS_BY_CFG["${key},${failure_class}"]:-0} + 1 ))
                fi
                end_ms="$(date +%s%3N)"
                duration_ms=$(( end_ms - start_ms ))

                TOTAL["$key"]=$(( ${TOTAL["$key"]:-0} + 1 ))
                DURATION["$key"]=$(( ${DURATION["$key"]:-0} + duration_ms ))

                printf '"%s",%s,%s,%s,%s,%s,%s,%s,%s,%s,"%s"\n' \
                    "$rom_name" "$set_name" "$mapper" "$TRACE_MODE" "$max_instr" "$max_visits" "$max_branch" "$status" "$failure_class" "$duration_ms" "$artifact_path" >> "$OUTPUT_CSV"
                printf '%-55s mapper=%-3s mode=%-6s instr=%-7s visits=%-4s branch=%-4s status=%s class=%s\n' \
                    "$rom_name" "$mapper" "$TRACE_MODE" "$max_instr" "$max_visits" "$max_branch" "$status" "${failure_class:-none}"
            done
        done
    done
done

echo ""
echo "Summary by mapper + trace config"
printf '%-8s %-8s %-9s %-10s %-10s %-6s %-6s %-6s %-9s\n' \
    "mapper" "mode" "max_instr" "max_visits" "max_branch" "pass" "fail" "total" "avg_ms"

declare -a KEYS
for key in "${!TOTAL[@]}"; do
    KEYS+=("$key")
done

IFS=$'\n' KEYS=($(printf '%s\n' "${KEYS[@]}" | sort))
unset IFS

for key in "${KEYS[@]}"; do
    mapper="${key%%,*}"
    rest="${key#*,}"
    mode="${rest%%,*}"
    rest="${rest#*,}"
    max_instr="${rest%%,*}"
    rest="${rest#*,}"
    max_visits="${rest%%,*}"
    max_branch="${rest##*,}"

    pass="${PASS["$key"]:-0}"
    fail="${FAIL["$key"]:-0}"
    total="${TOTAL["$key"]:-0}"
    duration="${DURATION["$key"]:-0}"
    avg_ms=0
    if [[ "$total" -gt 0 ]]; then
        avg_ms=$(( duration / total ))
    fi

    printf '%-8s %-8s %-9s %-10s %-10s %-6s %-6s %-6s %-9s\n' \
        "$mapper" "$mode" "$max_instr" "$max_visits" "$max_branch" "$pass" "$fail" "$total" "$avg_ms"
done

if [[ ${#FAIL_CLASS_BY_CFG[@]} -gt 0 ]]; then
    echo ""
    echo "Failure Classes by mapper + trace config"
    printf '%-8s %-8s %-9s %-10s %-10s %-20s %-6s\n' \
        "mapper" "mode" "max_instr" "max_visits" "max_branch" "class" "count"

    declare -a FAIL_CLASS_KEYS
    for key in "${!FAIL_CLASS_BY_CFG[@]}"; do
        FAIL_CLASS_KEYS+=("$key")
    done

    IFS=$'\n' FAIL_CLASS_KEYS=($(printf '%s\n' "${FAIL_CLASS_KEYS[@]}" | sort))
    unset IFS

    for key in "${FAIL_CLASS_KEYS[@]}"; do
        mapper="${key%%,*}"
        rest="${key#*,}"
        mode="${rest%%,*}"
        rest="${rest#*,}"
        max_instr="${rest%%,*}"
        rest="${rest#*,}"
        max_visits="${rest%%,*}"
        rest="${rest#*,}"
        max_branch="${rest%%,*}"
        class="${rest#*,}"
        printf '%-8s %-8s %-9s %-10s %-10s %-20s %-6s\n' \
            "$mapper" "$mode" "$max_instr" "$max_visits" "$max_branch" "$class" "${FAIL_CLASS_BY_CFG["$key"]:-0}"
    done
fi

echo ""
echo "Wrote sweep CSV: $OUTPUT_CSV"
