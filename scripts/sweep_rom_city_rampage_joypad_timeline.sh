#!/usr/bin/env bash
# Sweep deterministic JOYPAD timeline presets for Rom City Rampage.
#
# Produces a CSV with verification status and coverage heuristics per preset.
# Coverage heuristic is "code lines": lines matching a 6502 mnemonic pattern
# in the emitted asm (`^[[:space:]]+[a-z]{3}\b`), matching the existing project
# metric used for Rom City Rampage comparisons.
#
# Usage examples:
#   scripts/sweep_rom_city_rampage_joypad_timeline.sh
#   scripts/sweep_rom_city_rampage_joypad_timeline.sh -a all
#   scripts/sweep_rom_city_rampage_joypad_timeline.sh -p neutral,start_hold,right_then_start

set -u

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
REPO_ROOT="$(cd "${SCRIPT_DIR}/.." && pwd)"
ROM_PATH="${REPO_ROOT}/internal/testroms/special/Rom City Rampage.nes"

ASSEMBLER_MODE="asm6" # asm6|ca65|all
TRACE_MODE="hybrid"
MAX_INSTR="2000000"
MAX_VISITS="1024"
MAX_BRANCH="4096"
PRESETS_CSV="neutral,start_tap,start_hold,right_then_start,down_then_start,a_then_start,menu_probe"
OUTPUT_DIR="${REPO_ROOT}/internal/testroms/special"
OUTPUT_CSV="${REPO_ROOT}/rom_city_rampage_joypad_timeline_sweep.csv"

usage() {
    cat <<EOF
Usage: $(basename "$0") [-a assembler_mode] [-t trace_mode] [-i max_instr] [-v max_visits] [-b max_branch] [-p presets_csv] [-d output_dir] [-o output_csv]

Options:
  -a assembler_mode  asm6|ca65|all (default: asm6)
  -t trace_mode      static|emu|hybrid (default: hybrid)
  -i max_instr       max trace instructions (default: 2000000)
  -v max_visits      max visits per PC state (default: 1024)
  -b max_branch      max branch states (default: 4096)
  -p presets_csv     comma-separated preset names (default: ${PRESETS_CSV})
  -d output_dir      directory for asm/log outputs (default: ${OUTPUT_DIR})
  -o output_csv      output CSV path (default: ${OUTPUT_CSV})
EOF
}

while getopts ":a:t:i:v:b:p:d:o:h" opt; do
    case "$opt" in
        a) ASSEMBLER_MODE="$OPTARG" ;;
        t) TRACE_MODE="$OPTARG" ;;
        i) MAX_INSTR="$OPTARG" ;;
        v) MAX_VISITS="$OPTARG" ;;
        b) MAX_BRANCH="$OPTARG" ;;
        p) PRESETS_CSV="$OPTARG" ;;
        d) OUTPUT_DIR="$OPTARG" ;;
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

case "$ASSEMBLER_MODE" in
    asm6|ca65|all) ;;
    *)
        echo "Invalid assembler mode: ${ASSEMBLER_MODE} (expected asm6|ca65|all)" >&2
        exit 1
        ;;
esac

case "$TRACE_MODE" in
    static|emu|hybrid) ;;
    *)
        echo "Invalid trace mode: ${TRACE_MODE} (expected static|emu|hybrid)" >&2
        exit 1
        ;;
esac

if [[ ! -f "${ROM_PATH}" ]]; then
    echo "ROM not found: ${ROM_PATH}" >&2
    exit 1
fi

if ! [[ "${MAX_INSTR}" =~ ^[0-9]+$ && "${MAX_VISITS}" =~ ^[0-9]+$ && "${MAX_BRANCH}" =~ ^[0-9]+$ ]]; then
    echo "Trace budgets must be non-negative integers." >&2
    exit 1
fi

declare -A PRESET_SEQ
PRESET_SEQ["neutral"]=""
PRESET_SEQ["start_tap"]="8,0,0,0"
PRESET_SEQ["start_hold"]="8,8,8,8,8,8,8,8"
PRESET_SEQ["right_then_start"]="128,128,128,8,8,0,0,0"
PRESET_SEQ["down_then_start"]="32,32,32,8,8,0,0,0"
PRESET_SEQ["a_then_start"]="1,1,0,8,8,0,0,0"
PRESET_SEQ["menu_probe"]="16,16,128,128,8,8,1,0,8,0,0,0"

IFS=',' read -r -a REQUESTED_PRESETS <<< "${PRESETS_CSV}"
if [[ ${#REQUESTED_PRESETS[@]} -eq 0 ]]; then
    echo "No presets requested." >&2
    exit 1
fi

for preset in "${REQUESTED_PRESETS[@]}"; do
    if [[ -z "${PRESET_SEQ[${preset}]+x}" ]]; then
        echo "Unknown preset: ${preset}" >&2
        echo "Available presets: $(IFS=','; echo "${!PRESET_SEQ[*]}")" >&2
        exit 1
    fi
done

declare -a ASSEMBLERS
if [[ "${ASSEMBLER_MODE}" == "all" ]]; then
    ASSEMBLERS=("asm6" "ca65")
else
    ASSEMBLERS=("${ASSEMBLER_MODE}")
fi

mkdir -p "${OUTPUT_DIR}" "$(dirname "${OUTPUT_CSV}")"

tmp_dir="$(mktemp -d)"
trap 'rm -rf "$tmp_dir"' EXIT
mkdir -p "${tmp_dir}/gocache"
export GOCACHE="${tmp_dir}/gocache"

code_line_count() {
    local asm_file="$1"
    if command -v rg >/dev/null 2>&1; then
        rg -n '^[[:space:]]+[a-z]{3}\b' "$asm_file" | wc -l | tr -d ' '
    else
        grep -En '^[[:space:]]+[a-z]{3}\b' "$asm_file" | wc -l | tr -d ' '
    fi
}

echo "preset,sequence,assembler,status,duration_ms,asm_lines,asm_bytes,code_lines,code_delta_vs_neutral,output_path,log_path" > "${OUTPUT_CSV}"

cd "${REPO_ROOT}" || exit 1

echo "Sweeping Rom City Rampage JOYPAD timeline presets..."
echo "  rom:            ${ROM_PATH}"
echo "  assemblers:     ${ASSEMBLERS[*]}"
echo "  trace mode:     ${TRACE_MODE}"
echo "  max instr:      ${MAX_INSTR}"
echo "  max visits:     ${MAX_VISITS}"
echo "  max branch:     ${MAX_BRANCH}"
echo "  presets:        ${PRESETS_CSV}"
echo "  output dir:     ${OUTPUT_DIR}"
echo "  output csv:     ${OUTPUT_CSV}"
echo ""

declare -A BASELINE_CODE

for assembler in "${ASSEMBLERS[@]}"; do
    for preset in "${REQUESTED_PRESETS[@]}"; do
        sequence="${PRESET_SEQ[${preset}]}"
        out_file="${OUTPUT_DIR}/Rom City Rampage.${assembler}.${preset}.asm"
        log_file="${OUTPUT_DIR}/Rom City Rampage.${assembler}.${preset}.log"
        start_ms="$(date +%s%3N)"

        cmd=(go run . -verify -q -a "${assembler}" -s nes
            -trace-mode "${TRACE_MODE}"
            -trace-max-instr "${MAX_INSTR}"
            -trace-max-visits-per-state "${MAX_VISITS}"
            -trace-max-branch-states "${MAX_BRANCH}"
            -o "${out_file}"
            "${ROM_PATH}")

        if [[ -n "${sequence}" ]]; then
            cmd+=( -trace-joypad1-seq "${sequence}" )
        fi

        "${cmd[@]}" > "${log_file}" 2>&1
        rc=$?
        end_ms="$(date +%s%3N)"
        duration_ms=$(( end_ms - start_ms ))

        status="pass"
        if [[ ${rc} -ne 0 ]] || grep -Eiq "Disassembling failed|verification failed" "${log_file}"; then
            status="fail"
        fi

        asm_lines=""
        asm_bytes=""
        code_lines=""
        code_delta=""

        if [[ "${status}" == "pass" && -f "${out_file}" ]]; then
            asm_lines="$(wc -l < "${out_file}" | tr -d ' ')"
            asm_bytes="$(wc -c < "${out_file}" | tr -d ' ')"
            code_lines="$(code_line_count "${out_file}")"
        fi

        if [[ "${preset}" == "neutral" && -n "${code_lines}" ]]; then
            BASELINE_CODE["${assembler}"]="${code_lines}"
        fi

        baseline="${BASELINE_CODE[${assembler}]:-}"
        if [[ -n "${code_lines}" && -n "${baseline}" ]]; then
            code_delta=$(( code_lines - baseline ))
        fi

        printf '"%s","%s",%s,%s,%s,%s,%s,%s,%s,"%s","%s"\n' \
            "${preset}" "${sequence}" "${assembler}" "${status}" "${duration_ms}" "${asm_lines}" "${asm_bytes}" "${code_lines}" "${code_delta}" "${out_file}" "${log_file}" >> "${OUTPUT_CSV}"

        printf '%-16s assembler=%-4s status=%-4s code=%-5s delta=%-4s time_ms=%s\n' \
            "${preset}" "${assembler}" "${status}" "${code_lines:-n/a}" "${code_delta:-n/a}" "${duration_ms}"
    done
done

echo ""
echo "Wrote JOYPAD timeline sweep CSV: ${OUTPUT_CSV}"
