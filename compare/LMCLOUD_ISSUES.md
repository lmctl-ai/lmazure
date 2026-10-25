# lmcloud comparison notes

The comparison fixtures use lmcloud's built-in terraform_data provider only. They do not exercise AzureRM, credentials, OIDC, or network access. lmcloud exposes the required init, format, validate, plan, apply, output, state, and destroy methods, but its current DSL host returns a generic success envelope; the fixture preserves plan and state artifacts locally for comparison.

Capability gap: lmcloud has no provider mock command that models Azure resource inventory, ownership metadata, or Azure-specific cleanup semantics. The fixture therefore compares lifecycle mechanics and normalized fixture intent, not live Azure behavior.

Observed v0.1.9 API pitfall: variables set with `tofu.var()` are process-local. A separate cleanup DSL process must set `tofu.var("run_id", ...)` again, or persist `auto.tfvars.json` in the work directory. Without that, destroy returns false and the fixture state remains. With persistent `auto.tfvars.json` plus the cleanup var, destroy succeeds and `state_list` is empty.


Release metadata issue: the checked `v0.1.9` linux-x64 asset reports `lmcloud 0.1.0 (opentofu 1.13.2)` from `--version`. The download checksum is valid and the required API works, but the embedded version differs from its tag. Repro: download the public v0.1.9 asset, verify SHA256SUMS, then run `--version`. No lmcloud repository files were edited.

No false destroy-success issue is claimed: callers must check the returned destroy boolean, not the VM's success envelope. Persisted auto.tfvars.json is used by both create and cleanup processes. Provider errors and raw state are retained only in private workspaces, not public comparison evidence.
