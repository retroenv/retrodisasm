#!/usr/bin/env bash
# Benchmark mapper pass/fail baseline for the commercial ROM corpus.
#
# This script runs retrodisasm verification over the working/notworking
# commercial sets and outputs:
# 1) per-ROM CSV results (rom,set,mapper,status,duration_ms)
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

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
REPO_ROOT="$(cd "${SCRIPT_DIR}/.." && pwd)"
CORPUS_DIR="${REPO_ROOT}/internal/testroms/commercial"
OUTPUT_CSV="${REPO_ROOT}/mapper_baseline_${ASSEMBLER}.csv"

usage() {
    cat <<EOF
Usage: $(basename "$0") [-a assembler] [-g group] [-o output_csv]

Options:
  -a assembler   Assembler for -verify (default: ca65)
  -g group       ROM group: all|working|notworking (default: all)
  -o output_csv  Output CSV path (default: ${REPO_ROOT}/mapper_baseline_<assembler>.csv)
EOF
}

while getopts ":a:g:o:h" opt; do
    case "$opt" in
        a) ASSEMBLER="$OPTARG" ;;
        g) GROUP="$OPTARG" ;;
        o) OUTPUT_CSV="$OPTARG" ;;
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
echo "rom,set,mapper,status,duration_ms" > "$OUTPUT_CSV"

tmp_dir="$(mktemp -d)"
trap 'rm -rf "$tmp_dir"' EXIT
mkdir -p "${tmp_dir}/gocache"

# Keep go build cache inside writable temp space (important for sandboxed runs).
export GOCACHE="${tmp_dir}/gocache"

declare -A PASS FAIL TOTAL

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
    if "${RUNNER[@]}" -verify -q -a "$ASSEMBLER" -s nes -o "${tmp_dir}/out.asm" "$rom" >/dev/null 2>&1; then
        status="pass"
        PASS["$key"]=$(( ${PASS["$key"]:-0} + 1 ))
    else
        status="fail"
        FAIL["$key"]=$(( ${FAIL["$key"]:-0} + 1 ))
    fi
    end_ms="$(date +%s%3N)"
    duration_ms=$(( end_ms - start_ms ))

    TOTAL["$key"]=$(( ${TOTAL["$key"]:-0} + 1 ))

    printf '"%s",%s,%s,%s,%s\n' \
        "$rom_name" "$set_name" "$mapper" "$status" "$duration_ms" >> "$OUTPUT_CSV"
    printf '%-55s mapper=%-3s set=%-10s status=%s\n' "$rom_name" "$mapper" "$set_name" "$status"
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

echo ""
echo "Wrote baseline CSV: $OUTPUT_CSV"
