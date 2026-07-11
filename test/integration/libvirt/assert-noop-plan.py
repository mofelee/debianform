#!/usr/bin/env python3

import json
import sys


def main() -> int:
    if len(sys.argv) != 2:
        print("usage: assert-noop-plan.py PLAN_JSON", file=sys.stderr)
        return 2

    with open(sys.argv[1], encoding="utf-8") as plan_file:
        document = json.load(plan_file)

    if not isinstance(document, dict) or document.get("format_version") != "debianform.plan.alpha1":
        print("expected a debianform.plan.alpha1 JSON document", file=sys.stderr)
        return 1

    summary = document.get("summary")
    if not isinstance(summary, dict):
        print("expected plan summary object", file=sys.stderr)
        return 1

    counts = {}
    for name in ("create", "update", "delete", "no_op", "operations"):
        value = summary.get(name)
        if isinstance(value, bool) or not isinstance(value, int) or value < 0:
            print(f"expected summary.{name} to be a non-negative integer", file=sys.stderr)
            return 1
        counts[name] = value

    action_counts = {name: counts[name] for name in ("create", "update", "delete", "operations")}
    if any(action_counts.values()):
        print(
            "expected no-op plan after apply, got "
            + ", ".join(f"{name}={count}" for name, count in action_counts.items()),
            file=sys.stderr,
        )
        return 1
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
