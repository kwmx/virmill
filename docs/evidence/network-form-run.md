# Guided network creation and inline declaration parity

This slice adds ordinary TUI controls to the existing network.create workflow.
The shared service accepts either a declaration file or one inline document;
both receive identical schema, policy, planning and apply validation. Recipe
versions, native identity and helper authority remain unchanged. Export writes
a new full declaration; preview/back/error/cancel preserve in-memory options.
See ADR 0048 and the network creation guide.

Three agents implemented the form, application/CLI parity tests and a guarded
native preview fixture. Root owns shared-service/CLI contracts, workspace and
export integration, review, remote operations and traceability. Pure fixture
predicates passed in network-form-fixture-unit-001. No mock, synthetic view or
successful plan qualifies network packets, routing or guest behavior.

`network-form-core-001` passed the integrated Go race suite with required IPC
fixtures. Seven profile variants compare file/inline semantic review, exact
durable recipes and readback; invalid and unsupported input reaches no provider
effects or reservations. CLI dispatch preserves existing file and structured
error behavior. Root's workspace tests cover preview/back, canceled reads,
connection refusal, complete declaration export and the advanced file path.

Independent integration review found background preparation could interrupt the
new modal and Esc could mislabel a pending apply as canceled. Both were corrected;
`network-form-ui-002` passed the full TUI race suite after the first fix, and
`network-form-submit-003` passed targeted form/workspace regressions after the
pending-apply guard. Vet passed. Unchanged cached tests are regression evidence,
not fresh native qualifications.
