#!/usr/bin/env bash
# generate_scc_report_as_svg.sh
#
# Starts a static HTTP server, renders scc-report.html to SVG via url2svg,
# and writes an error-response SVG on any failure.
#
# Usage:
#   ./generate_scc_report_as_svg.sh [-o OUTPUT] [-p PORT] [-w WIDTH] [HTML_FILE]
#
# Defaults:
#   HTML_FILE = scc-report.html
#   OUTPUT    = scc-report.svg
#   PORT      = 8000
#   WIDTH     = 1400

set -euo pipefail

# ---------------------------------------------------------------------------
# Configuration
# ---------------------------------------------------------------------------

HTML_FILE="${1:-scc-report.html}"
OUTPUT="scc-report.svg"
PORT=8000
WIDTH=1400

while getopts ":o:p:w:h" opt; do
  case "$opt" in
    o) OUTPUT="$OPTARG" ;;
    p) PORT="$OPTARG" ;;
    w) WIDTH="$OPTARG" ;;
    h) sed -n '2,14p' "$0"; exit 0 ;;
    *) echo "unknown option: -$OPTARG" >&2; exit 2 ;;
  esac
done

PID_FILE="/tmp/scc-http-server.$$.pid"
LOG_FILE="/tmp/scc-http-server.$$.log"
SERVER_PID=""

# ---------------------------------------------------------------------------
# Error-response SVG writer
# ---------------------------------------------------------------------------

write_error_svg() {
  local out="$1"
  local title="$2"
  local detail="$3"
  local hint="${4:-}"

  # HTML-escape the three text fields.
  esc() { printf '%s' "$1" | sed -e 's/&/\&amp;/g' -e 's/</\&lt;/g' -e 's/>/\&gt;/g'; }
  local t d h
  t="$(esc "$title")"
  d="$(esc "$detail")"
  h="$(esc "$hint")"

  cat > "$out" <<SVG
<?xml version="1.0" encoding="UTF-8"?>
<svg xmlns="http://www.w3.org/2000/svg"
     width="${WIDTH}" height="360"
     viewBox="0 0 ${WIDTH} 360"
     role="img" aria-label="Error: ${t}">
  <defs>
    <style>
      .bg      { fill: #fef2f2; }
      .card    { fill: #ffffff; stroke: #fecaca; stroke-width: 2; }
      .accent  { fill: #dc2626; }
      .title   { font: bold 28px 'DejaVu Sans', Arial, sans-serif; fill: #7f1d1d; }
      .detail  { font: 18px 'DejaVu Sans Mono', Menlo, Consolas, monospace; fill: #450a0a; }
      .hint    { font: 15px 'DejaVu Sans', Arial, sans-serif; fill: #6b7280; }
      .label   { font: bold 13px 'DejaVu Sans', Arial, sans-serif; fill: #991b1b;
                 letter-spacing: 2px; }
      .meta    { font: 13px 'DejaVu Sans Mono', Menlo, Consolas, monospace; fill: #6b7280; }
    </style>
  </defs>

  <rect class="bg" x="0" y="0" width="${WIDTH}" height="360"/>
  <rect class="card" x="40" y="40" width="$((WIDTH - 80))" height="280" rx="12"/>

  <!-- accent bar -->
  <rect class="accent" x="40" y="40" width="8" height="280" rx="4"/>

  <!-- warning icon -->
  <g transform="translate(80, 96)">
    <circle cx="0" cy="0" r="22" fill="#fee2e2" stroke="#dc2626" stroke-width="2"/>
    <rect x="-2.5" y="-12" width="5" height="14" rx="2.5" fill="#dc2626"/>
    <circle cx="0" cy="8" r="3" fill="#dc2626"/>
  </g>

  <text class="label"  x="120" y="86">SCC REPORT GENERATION FAILED</text>
  <text class="title"  x="120" y="122">${t}</text>
  <text class="detail" x="80"  y="182">${d}</text>
  <text class="hint"   x="80"  y="222">${h}</text>

  <line x1="80" y1="250" x2="$((WIDTH - 80))" y2="250"
        stroke="#fecaca" stroke-width="1"/>

  <text class="meta" x="80" y="278">output: ${out}</text>
  <text class="meta" x="80" y="298">time:   $(date -u +%Y-%m-%dT%H:%M:%SZ)</text>
</svg>
SVG

  echo "wrote error SVG: ${out}" >&2
}

# ---------------------------------------------------------------------------
# Cleanup trap — always kill the server, always remove the pid/log files
# ---------------------------------------------------------------------------

cleanup() {
  if [[ -n "${SERVER_PID}" ]] && kill -0 "${SERVER_PID}" 2>/dev/null; then
    kill "${SERVER_PID}" 2>/dev/null || true
    wait "${SERVER_PID}" 2>/dev/null || true
  fi
  rm -f "${PID_FILE}" "${LOG_FILE}" 2>/dev/null || true
}
trap cleanup EXIT INT TERM

# ---------------------------------------------------------------------------
# Pre-flight checks
# ---------------------------------------------------------------------------

if ! command -v python3 >/dev/null 2>&1; then
  write_error_svg "${OUTPUT}" \
    "python3 not found" \
    "python3 is required to serve the HTML file over HTTP." \
    "Install Python 3, or replace the server step with another static file server."
  exit 1
fi

if ! command -v url2svg >/dev/null 2>&1; then
  write_error_svg "${OUTPUT}" \
    "url2svg not found" \
    "url2svg is not on \$PATH." \
    "Install url2svg, or add its directory to PATH."
  exit 1
fi

if [[ ! -f "${HTML_FILE}" ]]; then
  write_error_svg "${OUTPUT}" \
    "input file missing" \
    "${HTML_FILE} does not exist in $(pwd)." \
    "Generate it first (e.g. 'scc --format html -o ${HTML_FILE}'), then re-run."
  exit 1
fi

if ! [[ "${PORT}" =~ ^[0-9]+$ ]] || (( PORT < 1 || PORT > 65535 )); then
  write_error_svg "${OUTPUT}" \
    "invalid port" \
    "PORT must be an integer in 1..65535 (got: ${PORT})." \
    "Pass -p <port> with a valid value."
  exit 1
fi

# ---------------------------------------------------------------------------
# Start the static server in the background
# ---------------------------------------------------------------------------

# --bind 127.0.0.1 keeps it off the network; override by editing here if needed.
nohup python3 -m http.server "${PORT}" --bind 127.0.0.1 \
  > "${LOG_FILE}" 2>&1 &
SERVER_PID=$!
echo "${SERVER_PID}" > "${PID_FILE}"

# Wait until the server is actually accepting connections (up to ~5s).
URL="http://127.0.0.1:${PORT}/${HTML_FILE}"
ready=0
for _ in $(seq 1 50); do
  if curl -sfI "${URL}" >/dev/null 2>&1; then
    ready=1
    break
  fi
  sleep 0.1
done

if (( ready == 0 )); then
  detail="server did not respond at ${URL} within 5s"
  if [[ -s "${LOG_FILE}" ]]; then
    detail="${detail} — $(tail -n 1 "${LOG_FILE}")"
  fi
  write_error_svg "${OUTPUT}" \
    "HTTP server failed to start" \
    "${detail}" \
    "Check whether port ${PORT} is already in use (lsof -i :${PORT})."
  exit 1
fi

# ---------------------------------------------------------------------------
# Render to SVG
# ---------------------------------------------------------------------------

if ! url2svg \
      -url "${URL}" \
      -full-page \
      -mode full \
      -width "${WIDTH}" \
      -o "${OUTPUT}" \
      2> >(tee -a "${LOG_FILE}" >&2)
then
  write_error_svg "${OUTPUT}" \
    "url2svg failed" \
    "url2svg exited non-zero while rendering ${URL}." \
    "See ${LOG_FILE} for the tool's own output. A common cause is a page that needs a wait before hydration."
  exit 1
fi

if [[ ! -s "${OUTPUT}" ]]; then
  write_error_svg "${OUTPUT}" \
    "empty SVG produced" \
    "url2svg reported success but ${OUTPUT} is missing or empty." \
    "Check that -o is writable and that url2svg supports -full-page on this page."
  exit 1
fi

echo "ok: ${OUTPUT} ($(wc -c < "${OUTPUT}") bytes)"