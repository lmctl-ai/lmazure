# lmazure durable memory

Current version: **0.1.3 — READY TO TEST**. Live Azure testing is pending the user;
no real subscription was accessed during development. Local build/tests are the
acceptance gate; CI is supplementary, not a user approval or merge gate.

`lmazure` is a public MIT Azure SDK host on the pinned lmcode-csharp VM/SDK.
It ships one .NET 10 NativeAOT executable per platform, a typed DSL schema and
72 illustrated, fake-only examples with 72 lmcloud equivalents.

- [Verification and local gate](01-verification.md)
- [SDK pins and legal inventory](02-sdk-and-legal.md)
- [Architecture, build and memory budget](03-build-and-runtime.md)
- [Published versions and release workflow](04-release.md)
- [Ownership safety and cost model](05-cost-ledger.md)
- [Examples and pictures](06-expanded-gallery.md)
- [lmcloud equivalents and limits](07-lmcloud-equivalents.md)
- [Curated services and typed API](08-services-and-api.md)
- [Live procedure and open items](09-live-testing.md)
- [Current-release machine-readable evidence](v0.1.3-verification.json)

Prefer source, catalog and test assertions over copied counts when changing code.
Keep these notes current and short; release detail belongs in the linked evidence.
