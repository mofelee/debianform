#!/usr/bin/env python3

import json
import sys


HOST = "host.cihost"
COMPONENT = f"{HOST}.components.routing.systemd.networkd"
RELOAD = f"{HOST}.systemd.networkd.restart"
POST = f'{HOST}.script["reexport_bird"]'
LOOP_RECONFIGURE = f'{HOST}.systemd.networkd.reconfigure["bird-loop0"]'
CORE_RECONFIGURE = f'{HOST}.systemd.networkd.reconfigure["wg-core"]'
EDGE_RECONFIGURE = f'{HOST}.systemd.networkd.reconfigure["wg-edge"]'

LOOP_NETDEV = f'{COMPONENT}.netdev["10-bird-loop0"]'
CORE_NETDEV = f'{COMPONENT}.netdev["30-wg-core"]'
EDGE_NETDEV = f'{COMPONENT}.netdev["50-wg-edge"]'
LOOP_NETWORK = f'{COMPONENT}.network["20-bird-loop0"]'
CORE_NETWORK = f'{COMPONENT}.network["40-wg-core"]'
EDGE_NETWORK = f'{COMPONENT}.network["60-wg-edge"]'


def require(condition, message):
    if not condition:
        raise SystemExit(message)


def load_plan(path):
    with open(path, encoding="utf-8") as handle:
        plan = json.load(handle)
    require(
        plan.get("format_version") == "debianform.plan.alpha1",
        f"{path}: unexpected plan format",
    )
    operations = plan.get("operations", [])
    by_address = {operation["address"]: operation for operation in operations}
    require(
        len(by_address) == len(operations),
        f"{path}: duplicate operation addresses",
    )
    changes = {
        change["address"]: change["action"]
        for change in plan.get("changes", [])
        if change.get("action") != "no-op"
    }
    return plan, by_address, changes


def require_operations(path, operations, expected):
    require(
        set(operations) == set(expected),
        f"{path}: operations={sorted(operations)}, want={sorted(expected)}",
    )


def require_triggers(path, operation, expected):
    actual = operation.get("triggered_by", [])
    require(
        actual == expected,
        f"{path}: {operation['address']} triggered_by={actual}, want={expected}",
    )


def require_reload_delete_order(path, reload, names):
    preview = reload.get("command_preview", "")
    reload_index = preview.find("networkctl reload")
    require(reload_index >= 0, f"{path}: reload command is missing")
    previous = reload_index
    for name in names:
        fragment = f"ip link delete '{name}'"
        index = preview.find(fragment)
        require(index > previous, f"{path}: {fragment!r} is not ordered after reload")
        previous = index


def verify_initial(path, operations, changes):
    expected_operations = {
        RELOAD,
        LOOP_RECONFIGURE,
        CORE_RECONFIGURE,
        EDGE_RECONFIGURE,
        POST,
    }
    require_operations(path, operations, expected_operations)
    require(
        all(changes.get(address) == "create" for address in (
            LOOP_NETDEV,
            CORE_NETDEV,
            EDGE_NETDEV,
            LOOP_NETWORK,
            CORE_NETWORK,
            EDGE_NETWORK,
        )),
        f"{path}: initial networkd resources were not all creates",
    )
    require_triggers(
        path,
        operations[RELOAD],
        [LOOP_NETDEV, CORE_NETDEV, EDGE_NETDEV, LOOP_NETWORK, CORE_NETWORK, EDGE_NETWORK],
    )
    require_triggers(path, operations[LOOP_RECONFIGURE], [LOOP_NETWORK])
    require_triggers(path, operations[CORE_RECONFIGURE], [CORE_NETWORK])
    require_triggers(path, operations[EDGE_RECONFIGURE], [EDGE_NETWORK])
    require_triggers(path, operations[POST], [LOOP_NETWORK, CORE_NETWORK, EDGE_NETWORK])
    require(
        set(operations[LOOP_RECONFIGURE].get("depends_on", [])) == {LOOP_NETWORK, RELOAD},
        f"{path}: loopback reconfigure dependency is not the shared reload",
    )
    require(
        set(operations[CORE_RECONFIGURE].get("depends_on", []))
        == {CORE_NETWORK, RELOAD, LOOP_RECONFIGURE},
        f"{path}: core reconfigure chain is not deterministic",
    )
    require(
        set(operations[EDGE_RECONFIGURE].get("depends_on", []))
        == {EDGE_NETWORK, RELOAD, LOOP_RECONFIGURE, CORE_RECONFIGURE},
        f"{path}: edge reconfigure chain is not deterministic",
    )
    require(
        EDGE_RECONFIGURE in operations[POST].get("depends_on", []),
        f"{path}: post-reload script does not wait for all reconfigure operations",
    )
    require(
        "ip link delete" not in operations[RELOAD].get("command_preview", ""),
        f"{path}: initial apply unexpectedly deletes a runtime link",
    )


def verify_drift(path, operations, changes):
    require_operations(path, operations, {RELOAD, CORE_RECONFIGURE, POST})
    require(changes == {CORE_NETWORK: "update"}, f"{path}: drift changes={changes}")
    require_triggers(path, operations[RELOAD], [CORE_NETWORK])
    require_triggers(path, operations[CORE_RECONFIGURE], [CORE_NETWORK])
    require_triggers(path, operations[POST], [CORE_NETWORK])
    require(
        set(operations[CORE_RECONFIGURE].get("depends_on", [])) == {CORE_NETWORK, RELOAD},
        f"{path}: drift reconfigure does not depend on the shared reload",
    )
    require(
        CORE_RECONFIGURE in operations[POST].get("depends_on", []),
        f"{path}: drift post-reload operation does not wait for core reconfigure",
    )


def verify_delete_edge(path, operations, changes):
    require_operations(path, operations, {RELOAD, CORE_RECONFIGURE, POST})
    require(
        changes.get(CORE_NETWORK) == "update"
        and changes.get(EDGE_NETDEV) == "delete"
        and changes.get(EDGE_NETWORK) == "delete",
        f"{path}: edge deletion changes={changes}",
    )
    require_triggers(path, operations[RELOAD], [EDGE_NETDEV, CORE_NETWORK, EDGE_NETWORK])
    require_triggers(path, operations[CORE_RECONFIGURE], [CORE_NETWORK])
    require_triggers(path, operations[POST], [CORE_NETWORK, EDGE_NETWORK])
    require_reload_delete_order(path, operations[RELOAD], ["wg-edge"])
    require(
        set(operations[CORE_RECONFIGURE].get("depends_on", [])) == {CORE_NETWORK, RELOAD},
        f"{path}: core reconfigure does not wait for reload and runtime deletion",
    )
    require(
        CORE_RECONFIGURE in operations[POST].get("depends_on", []),
        f"{path}: deletion post-reload operation does not wait for core reconfigure",
    )


def verify_final(path, operations, changes):
    require_operations(path, operations, {RELOAD, POST})
    for address in (LOOP_NETDEV, CORE_NETDEV, LOOP_NETWORK, CORE_NETWORK):
        require(changes.get(address) == "delete", f"{path}: {address} was not deleted")
    require_triggers(
        path,
        operations[RELOAD],
        [LOOP_NETDEV, CORE_NETDEV, LOOP_NETWORK, CORE_NETWORK],
    )
    require_triggers(path, operations[POST], [LOOP_NETWORK, CORE_NETWORK])
    require_reload_delete_order(path, operations[RELOAD], ["bird-loop0", "wg-core"])
    require(
        RELOAD in operations[POST].get("depends_on", []),
        f"{path}: final post-reload operation does not depend on reload and deletion",
    )


def main():
    if len(sys.argv) != 3:
        raise SystemExit("usage: assert-activation-plan.py MODE PLAN.json")
    mode, path = sys.argv[1:]
    _, operations, changes = load_plan(path)
    verifiers = {
        "initial": verify_initial,
        "drift": verify_drift,
        "delete-edge": verify_delete_edge,
        "final": verify_final,
    }
    require(mode in verifiers, f"unknown mode: {mode}")
    verifiers[mode](path, operations, changes)


if __name__ == "__main__":
    main()
