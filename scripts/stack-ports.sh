#!/usr/bin/env bash
# Prints the first of the 20 host ports a checkout's stack publishes, derived from the checkout's path.
# Usage: scripts/stack-ports.sh <checkout path> | --slot <n> | --all; docs/decisions.md says why these bounds.
set -euo pipefail

PORT_FLOOR=10000
SLOT_COUNT=500
SLOT_WIDTH=20
# Ports that other stacks on the host publish (Armature's Jaeger) or that common services hold.
DENIED_PORTS=(10250 11211 15672 16686)

slot_is_free() {
  local base=$(($1 * SLOT_WIDTH + PORT_FLOOR)) port
  for port in "${DENIED_PORTS[@]}"; do
    (( port < base || port >= base + SLOT_WIDTH )) || return 1
  done
}

# A slot holding a denied port gives way to the next free one, so every hash still lands somewhere.
base_for_slot() {
  local slot=$(($1 % SLOT_COUNT)) tries=0
  until slot_is_free "$slot"; do
    slot=$(((slot + 1) % SLOT_COUNT))
    (( ++tries < SLOT_COUNT )) || { echo "stack-ports.sh: every slot holds a denied port" >&2; exit 1; }
  done
  echo $((slot * SLOT_WIDTH + PORT_FLOOR))
}

case "${1:-}" in
  --slot) base_for_slot "$2" ;;
  --all) for ((s = 0; s < SLOT_COUNT; s++)); do base_for_slot "$s"; done ;;
  --width) echo "$SLOT_WIDTH" ;;
  --denied) printf '%s\n' "${DENIED_PORTS[@]}" ;;
  "" | -*) echo "Usage: $0 <checkout path> | --slot <n> | --all | --width | --denied" >&2; exit 2 ;;
  *) base_for_slot "$(printf '%s' "$1" | cksum | cut -d' ' -f1)" ;;
esac
