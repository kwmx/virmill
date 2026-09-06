# Schema scope

These Draft 2020-12 schemas cover the baseline declarative VM, network, lab, backup-policy and plugin-manifest inputs plus an approved-plan structural contract and generic plugin RPC framing. They are design aids, not a complete generated runtime API. The implementation must publish method-specific requests/responses, the advanced configuration schemas and matching validators before release. Schema `$id` URLs are stable identifiers chosen for the specification; no claim is made that an online schema service has been deployed.

All baseline objects reject undeclared fields. Namespaced `extensions` are the explicit extensibility boundary. Schema validation does not prove host feasibility, source integrity, free space, valid cron/timezone/CIDR, unique MAC/IDs, dependency acyclicity, resource ownership, route intent or policy enforcement. Those require the semantic/preflight checks specified in the documents.

For `guest-only`, an `ipv4.cidr` describes the guest address space; it does **not** authorize putting a host IP or DHCP service on the bridge. For external bridges, guest address intent does not authorize changing external DHCP. `source.path` is local, approved input; no schema permission bypass exists.

The backup example references an already configured encrypted repository. Template references and public-key files in examples must be replaced/resolved from approved local resources; no guest image or credential is bundled. `plugin-manifest` permits source/development entries without digests. Distribution verification must require the signed, complete inventory defined in the packaging contract.

## Virmill schema identity

All declarative objects use `apiVersion: virmill/v1`. Schema `$id` and cross-schema `$ref` values use `https://virmill.example/schemas/v1/` as offline logical identifiers; these are not network endpoints. Register the bundled schemas locally, as `qa/validate_package.py` does. The namespace update changes names only, not the required v1 workflows or their validation rules.
