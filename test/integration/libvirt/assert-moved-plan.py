#!/usr/bin/env python3

import json
import sys


def fail(message: str) -> int:
    print(message, file=sys.stderr)
    return 1


def main() -> int:
    if len(sys.argv) != 8:
        print(
            "usage: assert-moved-plan.py PLAN_JSON HOST FROM_PREFIX TO_PREFIX "
            "EXPECTED_MOVES CHANGE_ADDRESS OPERATION_ADDRESS",
            file=sys.stderr,
        )
        return 2

    path, host, from_prefix, to_prefix, move_text, change_address, operation_address = sys.argv[1:]
    try:
        expected_moves = int(move_text)
    except ValueError:
        return fail("EXPECTED_MOVES must be an integer")

    with open(path, encoding="utf-8") as plan_file:
        document = json.load(plan_file)

    if document.get("format_version") != "debianform.plan.alpha1":
        return fail("expected a debianform.plan.alpha1 JSON document")

    summary = document.get("summary")
    if not isinstance(summary, dict):
        return fail("expected plan summary object")
    expected_summary = {
        "move": expected_moves,
        "create": 0,
        "update": 1,
        "delete": 0,
        "operations": 1,
    }
    for name, expected in expected_summary.items():
        if summary.get(name) != expected:
            return fail(f"expected summary.{name}={expected}, got {summary.get(name)!r}")

    moves = document.get("moves")
    if not isinstance(moves, list) or len(moves) != expected_moves:
        return fail(f"expected {expected_moves} realized moves, got {moves!r}")
    for move in moves:
        if move.get("host") != host:
            return fail(f"move has unexpected host: {move!r}")
        if not move.get("from", "").startswith(from_prefix + "."):
            return fail(f"move has unexpected source: {move!r}")
        if not move.get("to", "").startswith(to_prefix + "."):
            return fail(f"move has unexpected destination: {move!r}")
        if move["to"] != to_prefix + move["from"][len(from_prefix) :]:
            return fail(f"move did not retain its address suffix: {move!r}")

    changes = document.get("changes")
    if not isinstance(changes, list) or len(changes) != 1:
        return fail(f"expected one real resource change, got {changes!r}")
    if changes[0].get("host") != host or changes[0].get("action") != "update" or changes[0].get("address") != change_address:
        return fail(f"unexpected resource change: {changes[0]!r}")

    operations = document.get("operations")
    if not isinstance(operations, list) or len(operations) != 1:
        return fail(f"expected one operation, got {operations!r}")
    operation = operations[0]
    if operation.get("host") != host or operation.get("action") != "run" or operation.get("address") != operation_address:
        return fail(f"unexpected operation: {operation!r}")
    if operation.get("triggered_by") != [change_address]:
        return fail(f"operation has unexpected triggers: {operation!r}")

    return 0


if __name__ == "__main__":
    raise SystemExit(main())
