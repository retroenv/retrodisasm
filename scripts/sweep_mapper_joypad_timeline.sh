#!/usr/bin/env bash
# Sweep JOYPAD timeline presets across commercial NES ROMs and capture
# advisory emu-trace telemetry for mapper/input triage.
#
# Default profile targets mapper-2 notworking ROMs with deterministic hybrid
# trace settings used in prior triage phases.
#
# Usage examples:
#   scripts/sweep_mapper_joypad_timeline.sh
#   scripts/sweep_mapper_joypad_timeline.sh -m 2 -r Alfred -o /tmp/alfred_timeline.csv
#   scripts/sweep_mapper_joypad_timeline.sh -m 1,2 -p neutral,start_hold

set -u

ASSEMBLER="ca65"
GROUP="notworking" # all|working|notworking
MAPPERS_CSV="2"
TRACE_MODE="hybrid"
MAX_INSTR="200000"
MAX_VISITS="8"
MAX_BRANCH="0"
PRESETS_CSV="neutral,start_tap,start_hold,right_then_start,down_then_start,a_then_start,menu_probe"
ROM_NAME_FILTER=""
ARTIFACT_DIR=""

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
REPO_ROOT="$(cd "${SCRIPT_DIR}/.." && pwd)"
CORPUS_DIR="${REPO_ROOT}/internal/testroms/commercial"
OUTPUT_CSV="${REPO_ROOT}/mapper_joypad_timeline_sweep_${ASSEMBLER}.csv"

usage() {
    cat <<EOF
Usage: $(basename "$0") [-a assembler] [-g group] [-m mappers] [-t trace_mode] [-i max_instr] [-v max_visits] [-b max_branch] [-p presets_csv] [-r rom_name_filter] [-d artifact_dir] [-o output_csv]

Options:
  -a assembler        Assembler for -verify (default: ca65)
  -g group            ROM group: all|working|notworking (default: notworking)
  -m mappers          Comma-separated mapper filter (default: 2)
  -t trace_mode       Trace mode: static|emu|hybrid (default: hybrid)
  -i max_instr        Max trace instructions (default: 200000)
  -v max_visits       Max visits per PC state (default: 8)
  -b max_branch       Max branch states (default: 0)
  -p presets_csv      Comma-separated JOYPAD preset names
  -r rom_name_filter  Case-insensitive substring filter on ROM basename
  -d artifact_dir     Optional directory for per-failure artifacts
  -o output_csv       Output CSV path
EOF
}

while getopts ":a:g:m:t:i:v:b:p:r:d:o:h" opt; do
    case "$opt" in
        a) ASSEMBLER="$OPTARG" ;;
        g) GROUP="$OPTARG" ;;
        m) MAPPERS_CSV="$OPTARG" ;;
        t) TRACE_MODE="$OPTARG" ;;
        i) MAX_INSTR="$OPTARG" ;;
        v) MAX_VISITS="$OPTARG" ;;
        b) MAX_BRANCH="$OPTARG" ;;
        p) PRESETS_CSV="$OPTARG" ;;
        r) ROM_NAME_FILTER="$OPTARG" ;;
        d) ARTIFACT_DIR="$OPTARG" ;;
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

case "$TRACE_MODE" in
    static|emu|hybrid) ;;
    *)
        echo "Invalid trace mode: $TRACE_MODE (expected static|emu|hybrid)" >&2
        exit 1
        ;;
esac

is_non_negative_int() {
    [[ "$1" =~ ^[0-9]+$ ]]
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

if ! is_non_negative_int "$MAX_INSTR"; then
    echo "Invalid max instruction budget: ${MAX_INSTR}" >&2
    exit 1
fi
if ! is_non_negative_int "$MAX_VISITS"; then
    echo "Invalid max visits budget: ${MAX_VISITS}" >&2
    exit 1
fi
if ! is_non_negative_int "$MAX_BRANCH"; then
    echo "Invalid max branch budget: ${MAX_BRANCH}" >&2
    exit 1
fi

validate_csv_ints "$MAPPERS_CSV" "mappers"

declare -A PRESET_SEQ
PRESET_SEQ["neutral"]=""
PRESET_SEQ["start_tap"]="8,0,0,0"
PRESET_SEQ["start_hold"]="8,8,8,8,8,8,8,8"
PRESET_SEQ["right_then_start"]="128,128,128,8,8,0,0,0"
PRESET_SEQ["down_then_start"]="32,32,32,8,8,0,0,0"
PRESET_SEQ["a_then_start"]="1,1,0,8,8,0,0,0"
PRESET_SEQ["menu_probe"]="16,16,128,128,8,8,1,0,8,0,0,0"

IFS=',' read -r -a PRESETS <<< "$PRESETS_CSV"
if [[ ${#PRESETS[@]} -eq 0 ]]; then
    echo "No presets requested." >&2
    exit 1
fi
for preset in "${PRESETS[@]}"; do
    if [[ -z "${PRESET_SEQ[${preset}]+x}" ]]; then
        echo "Unknown preset: ${preset}" >&2
        echo "Available presets: $(IFS=','; echo "${!PRESET_SEQ[*]}")" >&2
        exit 1
    fi
done

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

sanitize_name() {
    local name="$1"
    name="$(echo "$name" | tr ' ' '_' | tr -cd '[:alnum:]_.-')"
    if [[ -z "$name" ]]; then
        name="unknown"
    fi
    echo "$name"
}

extract_int_field() {
    local key="$1"
    local file="$2"
    local line
    line="$(rg -o "\"${key}\":[0-9]+" "$file" | head -n1 || true)"
    if [[ -z "$line" ]]; then
        echo ""
        return 0
    fi
    echo "${line##*:}"
}

extract_string_field() {
    local key="$1"
    local file="$2"
    local line value
    line="$(rg -o "\"${key}\":\"[^\"]*\"" "$file" | head -n1 || true)"
    if [[ -z "$line" ]]; then
        echo ""
        return 0
    fi
    value="${line#*\"${key}\":\"}"
    value="${value%\"}"
    echo "$value"
}

code_line_count() {
    local asm_file="$1"
    if command -v rg >/dev/null 2>&1; then
        rg -n '^[[:space:]]+[a-z]{3}\b' "$asm_file" | wc -l | tr -d ' '
    else
        grep -En '^[[:space:]]+[a-z]{3}\b' "$asm_file" | wc -l | tr -d ' '
    fi
}

classify_failure() {
    local log_file="$1"
    if rg -qi "unexpected EOF|failed reading PRG data|could not read cartridge|invalid iNES|failed reading cartridge" "$log_file"; then
        echo "input_corrupt"
        return 0
    fi
    if rg -qi "Range error" "$log_file"; then
        echo "assembler_range"
        return 0
    fi
    if rg -qi "segment PRG mismatch|offset mismatch" "$log_file"; then
        echo "prg_mismatch"
        return 0
    fi
    if rg -qi "verification failed" "$log_file"; then
        echo "verification_failed"
        return 0
    fi
    if rg -qi "Disassembling failed" "$log_file"; then
        echo "disasm_failed"
        return 0
    fi
    echo "unknown"
}

verify_rom() {
    local rom="$1"
    local out_file="$2"
    local log_file="$3"
    local sequence="$4"

    cmd=(go run . -debug -verify -q -a "$ASSEMBLER" -s nes
        -trace-mode "$TRACE_MODE"
        -trace-max-instr "$MAX_INSTR"
        -trace-max-visits-per-state "$MAX_VISITS"
        -trace-max-branch-states "$MAX_BRANCH"
        -o "$out_file" "$rom")

    if [[ -n "$sequence" ]]; then
        cmd+=( -trace-joypad1-seq "$sequence" )
    fi

    "${cmd[@]}" >"$log_file" 2>&1
    local rc=$?
    if [[ $rc -ne 0 ]]; then
        return 1
    fi
    if rg -qi "Disassembling failed|verification failed" "$log_file"; then
        return 1
    fi
    return 0
}

write_failure_artifacts() {
    local rom="$1"
    local rom_name="$2"
    local preset="$3"
    local log_file="$4"
    local out_file="$5"

    if [[ -z "$ARTIFACT_DIR" ]]; then
        echo ""
        return 0
    fi

    local rom_stem artifact_path
    rom_stem="$(sanitize_name "${rom_name%.nes}")"
    artifact_path="${ARTIFACT_DIR}/${rom_stem}/preset-${preset}"
    mkdir -p "$artifact_path"

    cp "$log_file" "${artifact_path}/verify.log"
    if [[ -f "$out_file" ]]; then
        cp "$out_file" "${artifact_path}/disasm.asm"
    fi
    cat > "${artifact_path}/meta.txt" <<EOF
rom=${rom}
rom_name=${rom_name}
preset=${preset}
sequence=${PRESET_SEQ[$preset]}
trace_mode=${TRACE_MODE}
max_instr=${MAX_INSTR}
max_visits=${MAX_VISITS}
max_branch=${MAX_BRANCH}
assembler=${ASSEMBLER}
EOF
    echo "$artifact_path"
}

mkdir -p "$(dirname "$OUTPUT_CSV")"
echo "rom,set,mapper,preset,sequence,status,failure_class,duration_ms,asm_lines,asm_bytes,code_lines,unique_pc,unique_mapping,mapper_writes,mapping_changes,conditional_branches,branch_alternates,branch_states_executed,halt_reason,emu_only_pc,static_only_pc,artifact_path" > "$OUTPUT_CSV"

if [[ -n "$ARTIFACT_DIR" ]]; then
    mkdir -p "$ARTIFACT_DIR"
fi

tmp_dir="$(mktemp -d)"
trap 'rm -rf "$tmp_dir"' EXIT
mkdir -p "${tmp_dir}/gocache"
export GOCACHE="${tmp_dir}/gocache"

declare -A FAIL_CLASS_COUNT
declare -A ROM_PASS_COUNT
declare -A ROM_FAIL_COUNT
declare -A ROM_TELEMETRY_SEEN
declare -A ROMS_SEEN
total_runs=0
pass_runs=0
fail_runs=0

declare -a ROMS
while IFS= read -r -d '' rom; do
    mapper="$(mapper_from_rom "$rom")"
    if ! in_csv_list "$mapper" "$MAPPERS_CSV"; then
        continue
    fi
    if [[ -n "$ROM_NAME_FILTER" ]]; then
        rom_name="$(basename "$rom")"
        if [[ "${rom_name,,}" != *"${ROM_NAME_FILTER,,}"* ]]; then
            continue
        fi
    fi
    ROMS+=("$rom")
done < <(collect_roms | sort -z)

if [[ ${#ROMS[@]} -eq 0 ]]; then
    echo "No ROMs found for group '$GROUP', mappers '$MAPPERS_CSV', filter '$ROM_NAME_FILTER'." >&2
    exit 1
fi

echo "Sweeping mapper JOYPAD timeline presets..."
echo "  group:          ${GROUP}"
echo "  mapper filter:  ${MAPPERS_CSV}"
echo "  assembler:      ${ASSEMBLER}"
echo "  trace mode:     ${TRACE_MODE}"
echo "  max instr:      ${MAX_INSTR}"
echo "  max visits:     ${MAX_VISITS}"
echo "  max branch:     ${MAX_BRANCH}"
echo "  presets:        ${PRESETS_CSV}"
echo "  rom filter:     ${ROM_NAME_FILTER:-<none>}"
echo "  output csv:     ${OUTPUT_CSV}"
echo "  rom count:      ${#ROMS[@]}"
echo ""

cd "$REPO_ROOT" || exit 1

for rom in "${ROMS[@]}"; do
    set_name="$(group_from_path "$rom")"
    mapper="$(mapper_from_rom "$rom")"
    rom_name="$(basename "$rom")"

    for preset in "${PRESETS[@]}"; do
        sequence="${PRESET_SEQ[$preset]}"
        out_file="${tmp_dir}/out.asm"
        log_file="${tmp_dir}/run.log"
        rm -f "$out_file" "$log_file"

        start_ms="$(date +%s%3N)"
        if verify_rom "$rom" "$out_file" "$log_file" "$sequence"; then
            status="pass"
            failure_class=""
        else
            status="fail"
            failure_class="$(classify_failure "$log_file")"
        fi
        end_ms="$(date +%s%3N)"
        duration_ms=$(( end_ms - start_ms ))

        asm_lines=""
        asm_bytes=""
        code_lines=""
        if [[ -f "$out_file" ]]; then
            asm_lines="$(wc -l < "$out_file" | tr -d ' ')"
            asm_bytes="$(wc -c < "$out_file" | tr -d ' ')"
            code_lines="$(code_line_count "$out_file")"
        fi

        unique_pc="$(extract_int_field unique_pc "$log_file")"
        unique_mapping="$(extract_int_field unique_mapping "$log_file")"
        mapper_writes="$(extract_int_field mapper_writes "$log_file")"
        mapping_changes="$(extract_int_field mapping_changes "$log_file")"
        conditional_branches="$(extract_int_field conditional_branches "$log_file")"
        branch_alternates="$(extract_int_field branch_alternates "$log_file")"
        branch_states_executed="$(extract_int_field branch_states_executed "$log_file")"
        emu_only_pc="$(extract_int_field emu_only_pc "$log_file")"
        static_only_pc="$(extract_int_field static_only_pc "$log_file")"
        halt_reason="$(extract_string_field halt_reason "$log_file")"
        artifact_path=""
        if [[ "$status" == "fail" ]]; then
            artifact_path="$(write_failure_artifacts "$rom" "$rom_name" "$preset" "$log_file" "$out_file")"
        fi

        total_runs=$(( total_runs + 1 ))
        ROMS_SEEN["$rom_name"]=1
        if [[ "$status" == "pass" ]]; then
            pass_runs=$(( pass_runs + 1 ))
            ROM_PASS_COUNT["$rom_name"]=$(( ${ROM_PASS_COUNT["$rom_name"]:-0} + 1 ))
        else
            fail_runs=$(( fail_runs + 1 ))
            ROM_FAIL_COUNT["$rom_name"]=$(( ${ROM_FAIL_COUNT["$rom_name"]:-0} + 1 ))
            FAIL_CLASS_COUNT["$failure_class"]=$(( ${FAIL_CLASS_COUNT["$failure_class"]:-0} + 1 ))
        fi

        telemetry_key="${unique_pc:-NA}|${mapper_writes:-NA}|${halt_reason:-NA}|${code_lines:-NA}"
        ROM_TELEMETRY_SEEN["${rom_name}|${telemetry_key}"]=1

        printf '"%s",%s,%s,%s,"%s",%s,%s,%s,%s,%s,%s,%s,%s,%s,%s,%s,%s,%s,"%s",%s,%s,"%s"\n' \
            "$rom_name" "$set_name" "$mapper" "$preset" "$sequence" "$status" "$failure_class" "$duration_ms" "$asm_lines" "$asm_bytes" "$code_lines" \
            "$unique_pc" "$unique_mapping" "$mapper_writes" "$mapping_changes" "$conditional_branches" "$branch_alternates" "$branch_states_executed" \
            "$halt_reason" "$emu_only_pc" "$static_only_pc" "$artifact_path" >> "$OUTPUT_CSV"

        printf '%-45s preset=%-16s status=%-4s class=%-16s unique_pc=%-6s mapper_writes=%-4s halt=%s\n' \
            "$rom_name" "$preset" "$status" "${failure_class:-none}" "${unique_pc:-n/a}" "${mapper_writes:-n/a}" "${halt_reason:-n/a}"
    done
done

echo ""
echo "Summary"
echo "  total runs: ${total_runs}"
echo "  pass runs:  ${pass_runs}"
echo "  fail runs:  ${fail_runs}"
if [[ ${#FAIL_CLASS_COUNT[@]} -gt 0 ]]; then
    echo "  failure classes:"
    for class in "${!FAIL_CLASS_COUNT[@]}"; do
        printf '    %s: %d\n' "$class" "${FAIL_CLASS_COUNT[$class]}"
    done
fi
echo "  per-ROM telemetry variants (unique_pc|mapper_writes|halt_reason|code_lines):"
for rom_name in "${!ROMS_SEEN[@]}"; do
    variant_count=0
    for telemetry_key in "${!ROM_TELEMETRY_SEEN[@]}"; do
        if [[ "$telemetry_key" == "${rom_name}|"* ]]; then
            variant_count=$(( variant_count + 1 ))
        fi
    done
    printf '    %s: variants=%d pass=%d fail=%d\n' \
        "$rom_name" "$variant_count" "${ROM_PASS_COUNT["$rom_name"]:-0}" "${ROM_FAIL_COUNT["$rom_name"]:-0}"
done

echo ""
echo "Wrote mapper JOYPAD timeline sweep CSV: $OUTPUT_CSV"
