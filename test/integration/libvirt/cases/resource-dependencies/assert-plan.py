#!/usr/bin/env python3

import json
import sys


mode, path = sys.argv[1:]
with open(path, encoding="utf-8") as handle:
    document = json.load(handle)

package = 'host.cihost.packages.install["apache2"]'
config = 'host.cihost.files.file["/etc/apache2/ports.conf"]'
service = 'host.cihost.services.service["apache2"]'
changes = {change["address"]: change for change in document["changes"]}

if mode == "initial":
    assert changes[config].get("depends_on") == [package], changes[config]
    assert changes[service].get("depends_on") == [config], changes[service]
elif mode == "no-op":
    assert document["summary"]["no_op"] == 3, document["summary"]
    assert changes == {}, changes
elif mode == "policy-update":
    assert document["summary"]["update"] == 1, document["summary"]
    assert document["summary"]["no_op"] == 2, document["summary"]
    assert set(changes) == {package}, changes
    assert changes[package]["action"] == "update", changes[package]
elif mode == "destroy":
    for address in (package, config, service):
        assert changes[address]["action"] == "destroy", changes[address]
else:
    raise AssertionError(f"unknown assertion mode: {mode}")
