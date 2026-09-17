#!/usr/bin/env python3

import json
import sys


mode, path = sys.argv[1:]
with open(path, encoding="utf-8") as handle:
    document = json.load(handle)

install = 'host.cihost.components.artifact_tool.artifact.install["/usr/local/bin/dbf-artifact-tool"]'
alpha = 'host.cihost.components.alpha.services.service["dbf-artifact-alpha"]'
beta = 'host.cihost.components.beta.services.service["dbf-artifact-beta"]'
changes = {change["address"]: change for change in document["changes"]}

if mode == "initial":
    assert changes[alpha].get("depends_on") == [install], changes[alpha]
    assert changes[beta].get("depends_on") == [install], changes[beta]
elif mode == "destroy":
    for address in (install, alpha, beta):
        assert changes[address]["action"] == "destroy", changes[address]
else:
    raise AssertionError(f"unknown assertion mode: {mode}")
