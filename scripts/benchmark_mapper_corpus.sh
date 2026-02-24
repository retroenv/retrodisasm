#!/usr/bin/env bash
# Benchmark mapper pass/fail baseline for the commercial ROM corpus.
#
# This script runs retrodisasm verification over the working/notworking
# commercial sets and outputs:
# 1) per-ROM CSV results (rom,set,mapper,status,failure_class,duration_ms)
# 2) aggregated pass/fail totals by (set, mapper)
#
# Usage:
#   scripts/benchmark_mapper_corpus.sh
#   scripts/benchmark_mapper_corpus.sh -a asm6
#   scripts/benchmark_mapper_corpus.sh -g working
#
# Environment:
#   RETRODISASM_BIN=/path/to/retrodisasm  # optional (defaults to: go run .)

set -u

ASSEMBLER="ca65"
GROUP="all" # all|working|notworking
ARTIFACT_DIR=""
MAX_ASM_ERROR_CONTEXTS=40

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
REPO_ROOT="$(cd "${SCRIPT_DIR}/.." && pwd)"
CORPUS_DIR="${REPO_ROOT}/internal/testroms/commercial"
OUTPUT_CSV="${REPO_ROOT}/mapper_baseline_${ASSEMBLER}.csv"

usage() {
    cat <<EOF
Usage: $(basename "$0") [-a assembler] [-g group] [-o output_csv] [-d artifact_dir]

Options:
  -a assembler   Assembler for -verify (default: ca65)
  -g group       ROM group: all|working|notworking (default: all)
  -o output_csv  Output CSV path (default: ${REPO_ROOT}/mapper_baseline_<assembler>.csv)
  -d artifact_dir Optional directory for per-failure artifacts
EOF
}

while getopts ":a:g:o:d:h" opt; do
    case "$opt" in
        a) ASSEMBLER="$OPTARG" ;;
        g) GROUP="$OPTARG" ;;
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

if [[ -n "${RETRODISASM_BIN:-}" ]]; then
    RUNNER=("${RETRODISASM_BIN}")
else
    RUNNER=(go run .)
fi

verify_rom() {
    local rom="$1"
    local log_file="$2"
    shift 2

    "${RUNNER[@]}" -verify -q -a "$ASSEMBLER" -s nes -o "${tmp_dir}/out.asm" "$@" "$rom" >"$log_file" 2>&1
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
    local failure_class="$3"
    local log_file="$4"

    if [[ -z "$ARTIFACT_DIR" ]]; then
        echo ""
        return 0
    fi

    local rom_stem artifact_path
    rom_stem="$(sanitize_name "${rom_name%.nes}")"
    artifact_path="${ARTIFACT_DIR}/${rom_stem}/baseline_${ASSEMBLER}"
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
group=${GROUP}
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

mkdir -p "$(dirname "$OUTPUT_CSV")"
echo "rom,set,mapper,status,failure_class,duration_ms,artifact_path" > "$OUTPUT_CSV"

if [[ -n "$ARTIFACT_DIR" ]]; then
    mkdir -p "$ARTIFACT_DIR"
fi

tmp_dir="$(mktemp -d)"
trap 'rm -rf "$tmp_dir"' EXIT
mkdir -p "${tmp_dir}/gocache"

# Keep go build cache inside writable temp space (important for sandboxed runs).
export GOCACHE="${tmp_dir}/gocache"

declare -A PASS FAIL TOTAL FAIL_CLASS

declare -a ROMS
while IFS= read -r -d '' rom; do
    ROMS+=("$rom")
done < <(collect_roms | sort -z)

if [[ ${#ROMS[@]} -eq 0 ]]; then
    echo "No ROMs found for group '$GROUP'." >&2
    exit 1
fi

echo "Benchmarking mapper corpus..."
echo "  group:      $GROUP"
echo "  assembler:  $ASSEMBLER"
echo "  output csv: $OUTPUT_CSV"
echo "  rom count:  ${#ROMS[@]}"
echo ""

cd "$REPO_ROOT" || exit 1

for rom in "${ROMS[@]}"; do
    set_name="$(group_from_path "$rom")"
    mapper="$(mapper_from_rom "$rom")"
    rom_name="$(basename "$rom")"
    key="${set_name},${mapper}"

    start_ms="$(date +%s%3N)"
    log_file="${tmp_dir}/run.log"
    if verify_rom "$rom" "$log_file"; then
        status="pass"
        failure_class=""
        artifact_path=""
        PASS["$key"]=$(( ${PASS["$key"]:-0} + 1 ))
    else
        status="fail"
        failure_class="$(classify_failure "$log_file")"
        artifact_path="$(write_failure_artifacts "$rom" "$rom_name" "$failure_class" "$log_file")"
        FAIL["$key"]=$(( ${FAIL["$key"]:-0} + 1 ))
        FAIL_CLASS["$failure_class"]=$(( ${FAIL_CLASS["$failure_class"]:-0} + 1 ))
    fi
    end_ms="$(date +%s%3N)"
    duration_ms=$(( end_ms - start_ms ))

    TOTAL["$key"]=$(( ${TOTAL["$key"]:-0} + 1 ))

    printf '"%s",%s,%s,%s,%s,%s,"%s"\n' \
        "$rom_name" "$set_name" "$mapper" "$status" "$failure_class" "$duration_ms" "$artifact_path" >> "$OUTPUT_CSV"
    printf '%-55s mapper=%-3s set=%-10s status=%s class=%s\n' "$rom_name" "$mapper" "$set_name" "$status" "${failure_class:-none}"
done

echo ""
echo "Summary by set + mapper"
printf '%-12s %-8s %-6s %-6s %-6s\n' "set" "mapper" "pass" "fail" "total"

declare -a KEYS
for key in "${!TOTAL[@]}"; do
    KEYS+=("$key")
done

IFS=$'\n' KEYS=($(printf '%s\n' "${KEYS[@]}" | sort))
unset IFS

for key in "${KEYS[@]}"; do
    set_name="${key%%,*}"
    mapper="${key##*,}"
    pass="${PASS["$key"]:-0}"
    fail="${FAIL["$key"]:-0}"
    total="${TOTAL["$key"]:-0}"
    printf '%-12s %-8s %-6s %-6s %-6s\n' "$set_name" "$mapper" "$pass" "$fail" "$total"
done

if [[ ${#FAIL_CLASS[@]} -gt 0 ]]; then
    echo ""
    echo "Failure Classes"
    printf '%-20s %-6s\n' "class" "count"
    declare -a FAIL_KEYS
    for key in "${!FAIL_CLASS[@]}"; do
        FAIL_KEYS+=("$key")
    done
    IFS=$'\n' FAIL_KEYS=($(printf '%s\n' "${FAIL_KEYS[@]}" | sort))
    unset IFS
    for key in "${FAIL_KEYS[@]}"; do
        printf '%-20s %-6s\n' "$key" "${FAIL_CLASS["$key"]:-0}"
    done
fi

echo ""
echo "Wrote baseline CSV: $OUTPUT_CSV"
