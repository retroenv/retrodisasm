#!/usr/bin/env bash
# Cluster captured failure artifacts to accelerate mapper-specific triage.
#
# Input artifacts are expected from:
# - scripts/benchmark_mapper_corpus.sh -d <artifact_dir>
# - scripts/benchmark_trace_sweep.sh -d <artifact_dir>
#
# Outputs:
# 1) detail CSV per artifact
# 2) offset-cluster CSV
# 3) label-cluster CSV
# 4) markdown summary

set -u

ARTIFACT_DIR=""
OUTPUT_MD=""
DETAILS_CSV=""
TOP_N=10
MIN_HITS=2

usage() {
    cat <<EOF
Usage: $(basename "$0") -d artifact_dir [-o summary_md] [-c details_csv] [-n top_n] [-k min_hits]

Options:
  -d artifact_dir Root directory containing failure artifact bundles (required)
  -o summary_md   Markdown summary path (default: <artifact_dir>/failure_cluster_summary.md)
  -c details_csv  Per-artifact detail CSV path (default: <artifact_dir>/failure_cluster_details.csv)
  -n top_n        Top N offsets/labels per mapper in summary (default: 10)
  -k min_hits     Minimum artifact hits for stable-cluster outputs (default: 2)
EOF
}

while getopts ":d:o:c:n:k:h" opt; do
    case "$opt" in
        d) ARTIFACT_DIR="$OPTARG" ;;
        o) OUTPUT_MD="$OPTARG" ;;
        c) DETAILS_CSV="$OPTARG" ;;
        n) TOP_N="$OPTARG" ;;
        k) MIN_HITS="$OPTARG" ;;
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

if [[ -z "$ARTIFACT_DIR" ]]; then
    echo "artifact_dir is required (-d)." >&2
    usage
    exit 1
fi

if [[ ! -d "$ARTIFACT_DIR" ]]; then
    echo "artifact_dir does not exist: $ARTIFACT_DIR" >&2
    exit 1
fi

if ! [[ "$TOP_N" =~ ^[0-9]+$ ]] || [[ "$TOP_N" -le 0 ]]; then
    echo "top_n must be a positive integer: $TOP_N" >&2
    exit 1
fi

if ! [[ "$MIN_HITS" =~ ^[0-9]+$ ]] || [[ "$MIN_HITS" -le 0 ]]; then
    echo "min_hits must be a positive integer: $MIN_HITS" >&2
    exit 1
fi

if [[ -z "$OUTPUT_MD" ]]; then
    OUTPUT_MD="${ARTIFACT_DIR%/}/failure_cluster_summary.md"
fi

if [[ -z "$DETAILS_CSV" ]]; then
    DETAILS_CSV="${ARTIFACT_DIR%/}/failure_cluster_details.csv"
fi

OFFSET_CSV="${DETAILS_CSV%.csv}_offset_clusters.csv"
LABEL_CSV="${DETAILS_CSV%.csv}_label_clusters.csv"
OFFSET_STABLE_CSV="${DETAILS_CSV%.csv}_offset_stable.csv"
LABEL_STABLE_CSV="${DETAILS_CSV%.csv}_label_stable.csv"

mkdir -p "$(dirname "$OUTPUT_MD")"
mkdir -p "$(dirname "$DETAILS_CSV")"

tmp_dir="$(mktemp -d)"
trap 'rm -rf "$tmp_dir"' EXIT

mapper_from_rom() {
    local rom="$1"
    if [[ -z "$rom" ]] || [[ ! -f "$rom" ]]; then
        echo "unknown"
        return 0
    fi

    local b6 b7
    b6="$(od -An -j6 -N1 -tu1 "$rom" 2>/dev/null | tr -d ' ')"
    b7="$(od -An -j7 -N1 -tu1 "$rom" 2>/dev/null | tr -d ' ')"

    if [[ -z "$b6" ]] || [[ -z "$b7" ]]; then
        echo "unknown"
        return 0
    fi

    echo $(( ((b7 & 240)) | ((b6 >> 4) & 15) ))
}

read_meta_value() {
    local meta_file="$1"
    local key="$2"
    sed -n "s/^${key}=//p" "$meta_file" | head -n 1
}

declare -A FAIL_BY_MAPPER
declare -A OFFSET_COUNT
declare -A LABEL_COUNT

details_tmp="${tmp_dir}/details_rows.csv"
: > "$details_tmp"

meta_list="${tmp_dir}/meta_files.txt"
find "$ARTIFACT_DIR" -type f -name 'meta.txt' | sort > "$meta_list"

if [[ ! -s "$meta_list" ]]; then
    echo "No meta.txt files found under: $ARTIFACT_DIR" >&2
    exit 1
fi

artifacts_processed=0

while IFS= read -r meta_file; do
    artifact_path="$(dirname "$meta_file")"
    rom="$(read_meta_value "$meta_file" "rom")"
    rom_name="$(read_meta_value "$meta_file" "rom_name")"
    trace_mode="$(read_meta_value "$meta_file" "trace_mode")"
    max_instr="$(read_meta_value "$meta_file" "max_instr")"
    max_visits="$(read_meta_value "$meta_file" "max_visits")"
    max_branch="$(read_meta_value "$meta_file" "max_branch")"
    assembler="$(read_meta_value "$meta_file" "assembler")"
    group="$(read_meta_value "$meta_file" "group")"

    if [[ -z "$rom_name" ]]; then
        rom_name="$(basename "${rom:-unknown}")"
    fi

    mapper="$(mapper_from_rom "$rom")"
    FAIL_BY_MAPPER["$mapper"]=$(( ${FAIL_BY_MAPPER["$mapper"]:-0} + 1 ))

    config="baseline:${assembler:-unknown}"
    if [[ -n "$group" ]]; then
        config="${config},group=${group}"
    fi
    if [[ -n "$trace_mode" ]]; then
        config="trace=${trace_mode},i=${max_instr:-0},v=${max_visits:-0},b=${max_branch:-0}"
    fi

    mismatch_file="${artifact_path}/mismatch_offsets.txt"
    labels_file="${artifact_path}/labels.txt"

    mismatch_count=0
    first_offset="none"
    if [[ -f "$mismatch_file" ]]; then
        mismatch_count="$(rg -c 'Offset mismatch' "$mismatch_file" || true)"
        mismatch_count="${mismatch_count:-0}"
        first_offset="$(rg -o '"offset":"0x[0-9A-Fa-f]+"' "$mismatch_file" | head -n 1 | sed -E 's/.*"offset":"(0x[0-9A-Fa-f]+)".*/\1/' | tr 'A-F' 'a-f')"
        if [[ -z "$first_offset" ]]; then
            first_offset="none"
        fi

        while IFS= read -r offset; do
            [[ -z "$offset" ]] && continue
            offset_key="${mapper}|${offset}"
            OFFSET_COUNT["$offset_key"]=$(( ${OFFSET_COUNT["$offset_key"]:-0} + 1 ))
        done < <(rg -o '"offset":"0x[0-9A-Fa-f]+"' "$mismatch_file" | sed -E 's/.*"offset":"(0x[0-9A-Fa-f]+)".*/\1/' | tr 'A-F' 'a-f' | sort -u)
    fi

    labels_count=0
    if [[ -f "$labels_file" ]]; then
        labels_count="$(wc -l < "$labels_file" | tr -d ' ')"
        while IFS= read -r label; do
            [[ -z "$label" ]] && continue
            label_key="${mapper}|${label}"
            LABEL_COUNT["$label_key"]=$(( ${LABEL_COUNT["$label_key"]:-0} + 1 ))
        done < <(sed -nE 's/^[0-9]+:([A-Za-z_.][A-Za-z0-9_.]*):.*/\1/p' "$labels_file" | sort -u)
    fi

    printf '"%s","%s","%s","%s",%s,%s,"%s"\n' \
        "$rom_name" "$mapper" "$config" "$first_offset" "$mismatch_count" "$labels_count" "$artifact_path" >> "$details_tmp"

    artifacts_processed=$((artifacts_processed + 1))
done < "$meta_list"

{
    echo 'rom_name,mapper,config,first_mismatch_offset,mismatch_count,labels_count,artifact_path'
    cat "$details_tmp"
} > "$DETAILS_CSV"

offset_rows_tmp="${tmp_dir}/offset_rows.csv"
: > "$offset_rows_tmp"
for key in "${!OFFSET_COUNT[@]}"; do
    mapper="${key%%|*}"
    offset="${key#*|}"
    count="${OFFSET_COUNT[$key]}"
    printf '%s,%s,%s\n' "$mapper" "$offset" "$count" >> "$offset_rows_tmp"
done

{
    echo 'mapper,offset,artifact_hits'
    if [[ -s "$offset_rows_tmp" ]]; then
        sort -t, -k1,1 -k3,3nr -k2,2 "$offset_rows_tmp"
    fi
} > "$OFFSET_CSV"

{
    echo 'mapper,offset,artifact_hits'
    awk -F, -v min_hits="$MIN_HITS" 'NR > 1 && $3 >= min_hits { print $1 "," $2 "," $3 }' "$OFFSET_CSV"
} > "$OFFSET_STABLE_CSV"

label_rows_tmp="${tmp_dir}/label_rows.csv"
: > "$label_rows_tmp"
for key in "${!LABEL_COUNT[@]}"; do
    mapper="${key%%|*}"
    label="${key#*|}"
    count="${LABEL_COUNT[$key]}"
    printf '%s,%s,%s\n' "$mapper" "$label" "$count" >> "$label_rows_tmp"
done

{
    echo 'mapper,label,artifact_hits'
    if [[ -s "$label_rows_tmp" ]]; then
        sort -t, -k1,1 -k3,3nr -k2,2 "$label_rows_tmp"
    fi
} > "$LABEL_CSV"

{
    echo 'mapper,label,artifact_hits'
    awk -F, -v min_hits="$MIN_HITS" 'NR > 1 && $3 >= min_hits { print $1 "," $2 "," $3 }' "$LABEL_CSV"
} > "$LABEL_STABLE_CSV"

mapper_keys_tmp="${tmp_dir}/mapper_keys.txt"
for mapper in "${!FAIL_BY_MAPPER[@]}"; do
    echo "$mapper" >> "$mapper_keys_tmp"
done
sort -V "$mapper_keys_tmp" -o "$mapper_keys_tmp"

{
    echo "# Failure Artifact Cluster Summary"
    echo
    echo "Artifact root: \`$ARTIFACT_DIR\`"
    echo "Artifacts analyzed: $artifacts_processed"
    echo "Top-N per mapper: $TOP_N"
    echo "Stable threshold (artifact hits): >= $MIN_HITS"
    echo
    echo "Generated files:"
    echo "- details: \`$DETAILS_CSV\`"
    echo "- offset clusters: \`$OFFSET_CSV\`"
    echo "- label clusters: \`$LABEL_CSV\`"
    echo "- stable offsets: \`$OFFSET_STABLE_CSV\`"
    echo "- stable labels: \`$LABEL_STABLE_CSV\`"
    echo
    echo "## Failures by Mapper"
    echo
    echo "| mapper | failures |"
    echo "|---|---:|"
    while IFS= read -r mapper; do
        count="${FAIL_BY_MAPPER["$mapper"]:-0}"
        echo "| $mapper | $count |"
    done < "$mapper_keys_tmp"
    echo

    while IFS= read -r mapper; do
        echo "## Mapper $mapper"
        echo
        echo "Top mismatch offsets:"
        echo
        echo "| offset | artifact hits |"
        echo "|---|---:|"
        while IFS=, read -r offset count; do
            [[ -z "$offset" ]] && continue
            echo "| $offset | $count |"
        done < <(awk -F, -v mapper="$mapper" -v top_n="$TOP_N" '
            NR > 1 && $1 == mapper {
                print $2 "," $3
                shown++
                if (shown >= top_n) {
                    exit
                }
            }
        ' "$OFFSET_CSV")
        if ! awk -F, -v mapper="$mapper" 'NR > 1 && $1 == mapper { found=1 } END { exit found ? 0 : 1 }' "$OFFSET_CSV"; then
            echo "| (none) | 0 |"
        fi
        echo
        echo "Top emitted labels:"
        echo
        echo "| label | artifact hits |"
        echo "|---|---:|"
        while IFS=, read -r label count; do
            [[ -z "$label" ]] && continue
            echo "| \`$label\` | $count |"
        done < <(awk -F, -v mapper="$mapper" -v top_n="$TOP_N" '
            NR > 1 && $1 == mapper {
                print $2 "," $3
                shown++
                if (shown >= top_n) {
                    exit
                }
            }
        ' "$LABEL_CSV")
        if ! awk -F, -v mapper="$mapper" 'NR > 1 && $1 == mapper { found=1 } END { exit found ? 0 : 1 }' "$LABEL_CSV"; then
            echo "| (none) | 0 |"
        fi
        echo
        echo "Stable mismatch offsets (hits >= $MIN_HITS):"
        echo
        echo "| offset | artifact hits |"
        echo "|---|---:|"
        while IFS=, read -r offset count; do
            [[ -z "$offset" ]] && continue
            echo "| $offset | $count |"
        done < <(awk -F, -v mapper="$mapper" -v top_n="$TOP_N" '
            NR > 1 && $1 == mapper {
                print $2 "," $3
                shown++
                if (shown >= top_n) {
                    exit
                }
            }
        ' "$OFFSET_STABLE_CSV")
        if ! awk -F, -v mapper="$mapper" 'NR > 1 && $1 == mapper { found=1 } END { exit found ? 0 : 1 }' "$OFFSET_STABLE_CSV"; then
            echo "| (none) | 0 |"
        fi
        echo
        echo "Stable emitted labels (hits >= $MIN_HITS):"
        echo
        echo "| label | artifact hits |"
        echo "|---|---:|"
        while IFS=, read -r label count; do
            [[ -z "$label" ]] && continue
            echo "| \`$label\` | $count |"
        done < <(awk -F, -v mapper="$mapper" -v top_n="$TOP_N" '
            NR > 1 && $1 == mapper {
                print $2 "," $3
                shown++
                if (shown >= top_n) {
                    exit
                }
            }
        ' "$LABEL_STABLE_CSV")
        if ! awk -F, -v mapper="$mapper" 'NR > 1 && $1 == mapper { found=1 } END { exit found ? 0 : 1 }' "$LABEL_STABLE_CSV"; then
            echo "| (none) | 0 |"
        fi
        echo
    done < "$mapper_keys_tmp"
} > "$OUTPUT_MD"

echo "Processed artifacts: $artifacts_processed"
echo "Wrote summary:        $OUTPUT_MD"
echo "Wrote details CSV:    $DETAILS_CSV"
echo "Wrote offset CSV:     $OFFSET_CSV"
echo "Wrote label CSV:      $LABEL_CSV"
echo "Wrote stable offsets: $OFFSET_STABLE_CSV"
echo "Wrote stable labels:  $LABEL_STABLE_CSV"
