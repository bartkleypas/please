#!/bin/bash
LOG="/tmp/please_acp.log"
echo "=== Launched at $(date) in $PWD with args: $@ ===" >> "$LOG"

exec /Users/bart/Code/please/please acp \
  -c /Users/bart/Code/please/livefire.json \
  -v /Users/bart/Code/please/test_vault/livefire.db \
  "$@" \
  2>> "$LOG"

