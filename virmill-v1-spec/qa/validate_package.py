#!/usr/bin/env python3
"""Validate this specification's schemas/examples and internal links.

Requires PyYAML and jsonschema (with referencing), not the VM application.
This is a document/contract check, not a hardware or product acceptance suite.
"""
from __future__ import annotations
import copy
import ipaddress
import json
import pathlib
import re
from zoneinfo import ZoneInfo
import yaml
from jsonschema import Draft202012Validator, FormatChecker
from referencing import Registry, Resource

ROOT = pathlib.Path(__file__).resolve().parents[1]
BASE = "https://virmill.example/schemas/v1/"


def semantic_validate(data: dict) -> None:
    kind, spec = data.get("kind"), data.get("spec", {})
    def check_vm(vm: dict, network_ids: set[str] | None = None) -> None:
        nics = vm.get("nics", [])
        ids = [n["id"] for n in nics]
        if len(ids) != len(set(ids)):
            raise ValueError("Duplicate NIC ID")
        if sum(bool(n["defaultRoute"]) for n in nics) > 1:
            raise ValueError("Multiple normal default routes")
        for n in nics:
            if network_ids is not None and n["networkRef"] not in network_ids:
                raise ValueError("Unknown lab network")
            ip = n["ipv4"]
            if ip["mode"] == "static":
                ipaddress.IPv4Interface(ip["address"])
    def check_net(net: dict) -> None:
        ip = net.get("ipv4", {})
        if ip.get("cidr") not in (None, "auto"):
            ipaddress.IPv4Network(ip["cidr"], strict=True)
        if net["type"] in ("lab", "guest-only") and ip.get("dhcp", {}).get("advertiseDefaultRoute", False):
            raise ValueError("Lab must not advertise a normal default route")
    if kind == "VM":
        check_vm(spec)
    elif kind == "Network":
        check_net(spec)
    elif kind == "BackupPolicy":
        ZoneInfo(spec["schedule"]["timezone"])
        if len(spec["schedule"]["cron"].split()) != 5:
            raise ValueError("Expected five cron fields")
    elif kind == "Lab":
        nets = spec["networks"]
        names = [n["id"] for n in nets]
        if len(names) != len(set(names)):
            raise ValueError("Duplicate network ID")
        for n in nets:
            if "spec" in n:
                check_net(n["spec"])
        machines = spec["machines"]
        ids = [m["id"] for m in machines]
        if len(ids) != len(set(ids)):
            raise ValueError("Duplicate machine ID")
        graph = {m["id"]: [d["machine"] for d in m.get("dependsOn", [])] for m in machines}
        visiting, visited = set(), set()
        def visit(node: str) -> None:
            if node not in graph:
                raise ValueError("Unknown dependency")
            if node in visiting:
                raise ValueError("Dependency cycle")
            if node in visited:
                return
            visiting.add(node)
            for other in graph[node]:
                visit(other)
            visiting.remove(node)
            visited.add(node)
        for m in machines:
            check_vm(m["spec"], set(names))
            visit(m["id"])


def main() -> None:
    schemas = {}
    for path in sorted((ROOT / "schemas").glob("*.schema.json")):
        schema = json.loads(path.read_text())
        Draft202012Validator.check_schema(schema)
        schemas[schema["$id"]] = schema
    registry = Registry().with_resources((uri, Resource.from_contents(value)) for uri, value in schemas.items())
    def validate(value: dict, name: str) -> None:
        Draft202012Validator(schemas[BASE + name + ".schema.json"], registry=registry,
                             format_checker=FormatChecker()).validate(value)
        semantic_validate(value)
    kindmap = {"VM": "vm", "Network": "network", "Lab": "lab", "BackupPolicy": "backup-policy"}
    examples = []
    for path in sorted((ROOT / "examples").rglob("*.yaml")):
        value = yaml.safe_load(path.read_text())
        validate(value, kindmap[value["kind"]])
        examples.append(str(path.relative_to(ROOT)))
    manifest = json.loads((ROOT / "examples/plugins/vm-summary/manifest.json").read_text())
    validate(manifest, "plugin-manifest")
    examples.append("examples/plugins/vm-summary/manifest.json")
    lab = yaml.safe_load((ROOT / "examples/labs/multi-network-lab.yaml").read_text())
    negative = []
    def reject(label: str, value: dict, name: str) -> None:
        try:
            validate(value, name)
        except Exception:
            negative.append(label)
        else:
            raise AssertionError("Accepted negative case: " + label)
    v = copy.deepcopy(lab); v["spec"]["unexpected"] = True; reject("unknown fields", v, "lab")
    v = copy.deepcopy(lab); v["spec"]["machines"][0]["spec"]["nics"][1]["defaultRoute"] = True
    reject("multiple normal default routes", v, "lab")
    v = copy.deepcopy(lab); v["spec"]["machines"][0]["spec"]["nics"][1]["networkRef"] = "absent"
    reject("unresolved network", v, "lab")
    v = copy.deepcopy(lab); v["spec"]["machines"][0]["dependsOn"] = [{"machine": "target-server", "ready": "running", "timeoutSeconds": 10}]
    reject("dependency cycle", v, "lab")
    v = copy.deepcopy(lab); v["spec"]["machines"][1]["id"] = "workstation"
    reject("duplicate machine identity", v, "lab")
    v = copy.deepcopy(lab); v["spec"]["networks"][2]["spec"]["ipv4"]["dhcp"]["enabled"] = True
    reject("host DHCP on guest-only network", v, "lab")
    v = copy.deepcopy(lab); v["spec"]["networks"][1]["spec"]["ipv4"]["cidr"] = "not-a-cidr"
    reject("invalid CIDR", v, "lab")
    v = copy.deepcopy(manifest); v["entrypoints"]["linux/amd64"]["path"] = "../escape"
    reject("plugin entrypoint traversal", v, "plugin-manifest")
    v = copy.deepcopy(manifest); v["protocol"]["maxVersion"] = "2.0"
    reject("unimplemented protocol version", v, "plugin-manifest")
    reject("RPC both result and error", {"jsonrpc":"2.0","id":"h-1","result":{},"error":{"code":-32010,"message":"x"}}, "plugin-rpc-envelope")
    reject("numeric RPC identifier", {"jsonrpc":"2.0","id":1,"method":"ping"}, "plugin-rpc-envelope")
    links, broken, labels = 0, [], set()
    for p in ROOT.rglob("*.md"):
        text = p.read_text()
        labels.update(re.findall(r"\[S\d{2}\]", text))
        # Check only actual Markdown destinations, not protocol text/code syntax.
        for target in re.findall(r"(?<!!)\[[^\]\n]+\]\(([^)]+)\)", text):
            if target.startswith(("http:", "https:", "mailto:", "#")):
                continue
            rel = target.split("#", 1)[0]
            links += 1
            if not (p.parent / rel).is_file():
                broken.append(f"{p.relative_to(ROOT)} -> {target}")
    assert not broken, broken
    reftext = (ROOT / "docs/16-references-and-input-audit.md").read_text()
    defined = set(re.findall(r"### (\[S\d{2}\])", reftext))
    assert labels <= defined, sorted(labels - defined)
    requirements = json.loads((ROOT / "contracts/requirements.json").read_text())["requirements"]
    assert len({r["id"] for r in requirements}) == len(requirements)
    print(json.dumps({"status": "passed", "schema_count": len(schemas), "valid_examples": examples,
                      "negative_checks": negative, "internal_links_checked": links,
                      "source_labels_resolved": len(labels), "product_acceptance_scenarios_documented": len(requirements),
                      "scope": "Specification aids only; no VM or hardware acceptance performed"}, indent=2))

if __name__ == "__main__":
    main()
