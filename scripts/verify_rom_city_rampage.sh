#!/usr/bin/env bash
set -euo pipefail

REPO_ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
ROM_PATH="${REPO_ROOT}/internal/testroms/special/Rom City Rampage.nes"
OUT_DIR="${1:-${REPO_ROOT}/internal/testroms/special}"

if [[ ! -f "${ROM_PATH}" ]]; then
  echo "ROM not found: ${ROM_PATH}" >&2
  exit 1
fi

mkdir -p "${OUT_DIR}"

echo "Verifying Rom City Rampage with ca65 + asm6"
echo "ROM: ${ROM_PATH}"
echo "Output dir: ${OUT_DIR}"

for assembler in ca65 asm6; do
  out_file="${OUT_DIR}/Rom City Rampage.${assembler}.asm"
  log_file="${OUT_DIR}/Rom City Rampage.${assembler}.log"
  gocache_dir="/tmp/retrodisasm_gocache_rom_city_rampage_${assembler}"

  echo ""
  echo "[${assembler}] generating + verifying..."
  (
    cd "${REPO_ROOT}"
    GOCACHE="${gocache_dir}" go run . \
      -verify -q -a "${assembler}" -s nes \
      -o "${out_file}" \
      "${ROM_PATH}" >"${log_file}" 2>&1
  )
  lines="$(wc -l < "${out_file}")"
  size="$(wc -c < "${out_file}")"
  echo "[${assembler}] ok: ${out_file} (${lines} lines, ${size} bytes)"
done

echo ""
echo "Rom City Rampage verification complete."
