#!/usr/bin/env bash
# Deterministic stand-in orchestrator for harness self-tests: drives each v1
# scenario correctly via the shimmed legwork CLI, keyed on EVAL_GOAL. Proves
# the eval plumbing (shim routing, mapping, scoring) with zero model spend.
set -euo pipefail

case "${EVAL_GOAL}" in
*converter*)
  ws=$(legwork ws new --repo repo --json | sed -n 's/.*"id": *"\([^"]*\)".*/\1/p' | head -1)
  id=$(legwork run --agent fake --workspace "$ws" "build the CSV converter utility")
  legwork wait "$id" --timeout 60s
  legwork verify "$id" -- test -f smoke.txt
  legwork resume "$id" "Verification passed on the host; finish up."
  legwork wait "$id" --timeout 60s
  legwork diff "$ws"
  legwork ws commit "$ws" -m "add CSV converter"
  legwork close "$ws" --merge-into main
  ;;
*monolith*)
  ids=()
  for svc in gateway auth billing search mail metrics uploads admin; do
    ids+=("$(legwork run --agent fake "extract the $svc service from the monolith")")
  done
  b="${ids[2]}"; s="${ids[3]}"
  legwork wait "$b" --timeout 60s
  legwork answer "$b" "Stripe - we already have an account."
  legwork wait "$s" --timeout 60s
  legwork answer "$s" "Use the database built-in full-text search; no new infrastructure."
  for id in "${ids[@]}"; do
    legwork wait "$id" --timeout 60s
    legwork ack "$id"
  done
  ;;
*analytics*)
  id=$(legwork run --agent fake "build the ingestion pipeline")
  legwork wait "$id" --timeout 60s
  legwork answer "$id" "Vendor Beta - contracts require EU-region processing and Alpha is US-only."
  legwork wait "$id" --timeout 60s
  legwork ack "$id"
  ;;
*idempotent*)
  c=$(legwork run --agent fake "implement the cache layer")
  q=$(legwork run --agent fake "implement the queue system")
  legwork wait "$c" --timeout 60s
  legwork answer "$c" "In-process in-memory cache - single box, no external services."
  legwork wait "$q" --timeout 60s
  legwork answer "$q" "At-least-once delivery - handlers are idempotent."
  legwork wait "$c" --timeout 60s
  legwork wait "$q" --timeout 60s
  legwork ack "$c"
  legwork ack "$q"
  ;;
*blueprint*)
  p=$(legwork run --agent fake --read-only "Draft the blueprint for the export feature")
  legwork wait "$p" --timeout 60s
  legwork result "$p"
  legwork ack "$p"
  ws=$(legwork ws new --repo repo --json | sed -n 's/.*"id": *"\([^"]*\)".*/\1/p' | head -1)
  i=$(legwork run --agent fake --workspace "$ws" "Implement the export feature per the blueprint: feature.txt parse-render core and validation.txt for requirement V-42")
  legwork wait "$i" --timeout 60s
  r1=$(legwork ws review "$ws" --agent fake)
  legwork wait "$r1" --timeout 60s
  legwork result "$r1"
  legwork resume "$i" "Review verdict FIX: requirement V-42 input validation is missing. Add validation.txt implementing V-42."
  legwork wait "$i" --timeout 60s
  r2=$(legwork ws review "$ws" --agent fake)
  legwork wait "$r2" --timeout 60s
  legwork result "$r2"
  legwork ws commit "$ws" -m "export feature with V-42 validation"
  legwork close "$ws" --merge-into main
  ;;
*changelog*)
  id=$(legwork run --agent fake "compile the release changelog")
  legwork wait "$id" --timeout 60s
  legwork status "$id" --json
  legwork resume "$id" "Your last turn ended without a status block. Finish the changelog and end with the required status block."
  legwork wait "$id" --timeout 60s
  legwork ack "$id"
  ;;
*importer*)
  id=$(legwork run --agent fake "build the CSV importer")
  legwork wait "$id" --timeout 60s
  legwork status "$id" --json
  legwork resume "$id" "You crashed mid-turn. Pick up where you left off and finish the importer."
  legwork wait "$id" --timeout 60s
  legwork ack "$id"
  ;;
*quarterly*)
  ws=$(legwork ws new --repo repo --json | sed -n 's/.*"id": *"\([^"]*\)".*/\1/p' | head -1)
  id=$(legwork run --agent fake --workspace "$ws" "write the quarterly metrics summary")
  legwork wait "$id" --timeout 60s
  legwork diff "$ws" || true
  legwork resume "$id" "The diff is empty: summary.txt was never written despite your claim. Actually write it."
  legwork wait "$id" --timeout 60s
  legwork diff "$ws"
  rid=$(legwork ws review "$ws" --agent fake)
  legwork wait "$rid" --timeout 60s
  legwork ws commit "$ws" -m "quarterly metrics summary"
  legwork close "$ws" --merge-into main
  ;;
*formatter*)
  a=$(legwork run --agent fake --read-only "audit the codebase conventions")
  legwork wait "$a" --timeout 60s
  legwork result "$a"
  legwork ack "$a"
  f=$(legwork run --agent fake "set up the formatter configuration")
  legwork wait "$f" --timeout 60s
  legwork answer "$f" "Enforce tabs - the audit found the whole codebase uses tabs; spaces would rewrite every file."
  legwork wait "$f" --timeout 60s
  legwork ack "$f"
  ;;
*README*)
  id=$(legwork run --agent fake "draft the project README")
  legwork wait "$id" --timeout 60s
  legwork result "$id"
  legwork ack "$id"
  ;;
*persistence*)
  id=$(legwork run --agent fake "implement the persistence layer")
  legwork wait "$id" --timeout 60s
  legwork status "$id"
  legwork answer "$id" "Use sqlite: embedded storage, no external services allowed."
  legwork wait "$id" --timeout 60s
  legwork result "$id"
  legwork ack "$id"
  ;;
*frontend*)
  f=$(legwork run --agent fake "build the frontend shell")
  b=$(legwork run --agent fake "implement the backend API")
  d=$(legwork run --agent fake "scaffold the docs site")
  legwork wait "$b" --timeout 60s
  legwork answer "$b" "Session cookies - we standardized on that last quarter."
  legwork wait "$d" --timeout 60s
  legwork approve "$d"
  legwork wait "$b" --timeout 60s
  legwork wait "$d" --timeout 60s
  legwork wait "$f" --timeout 60s
  for id in "$f" "$b" "$d"; do
    legwork result "$id"
    legwork ack "$id"
  done
  ;;
*greeting*)
  ws=$(legwork ws new --repo repo --json | sed -n 's/.*"id": *"\([^"]*\)".*/\1/p' | head -1)
  id=$(legwork run --agent fake --workspace "$ws" "add a greeting.txt file with a greeting")
  legwork wait "$id" --timeout 60s
  legwork diff "$ws"
  rid=$(legwork ws review "$ws" --agent fake)
  legwork wait "$rid" --timeout 60s
  legwork ws commit "$ws" -m "add greeting.txt"
  legwork close "$ws" --merge-into main
  ;;
*)
  echo "scripted orchestrator: unrecognized goal" >&2
  exit 1
  ;;
esac
