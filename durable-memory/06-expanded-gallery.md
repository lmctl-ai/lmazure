# Examples and pictures

`examples/catalog.json` is authoritative: **72 self-contained .lms programs and
72 PNGs** in `docs/images/`. Catalog categories are 25 existing inventory/audit,
12 service inventory, 12 metadata audit, four resource graphs, two matrices and
17 lifecycle/admission examples. Cost modes: 55 read-only, 16 owned lifecycles,
one budget-admission rejection. All gallery programs explicitly call `mock()`;
environment flags cannot turn these scripts into live Azure runs.

```sh
lmctl autorun https://raw.githubusercontent.com/lmctl-ai/lmazure/main/examples/resource_inventory.lms
```

Each installs `github:lmctl-ai/lmazure@latest` if needed, exports CSV/cost JSON,
and renders a PNG using lmplot or lmdraw. README is pictures-first, with every
raw autorun URL and “running this example costs about $X” caption. All amounts
are labeled estimated from price table, not yet measured live; actual Azure
charges for fake examples are zero. Read-only estimates are $0.00, owned cases
$0.0005–$0.0010; the admission case rejects before dispatch at a $0.0002 cap.

Charts/graphs describe mock metadata, not verified network or application
relationships. Generic live ARM listings cannot establish public/encryption
policy or synthesize absent child resources. Golden assertions are mock-only.

`scripts/test-examples.py` checks catalog/source/image/caption parity and, with
`LMAZURE_LMSCRIPT=lmscript`, runs all 72 in isolated directories, validating
900×560 PNGs below 300 KB and cost evidence/output. `scripts/render-examples.py`
refreshes committed pictures. Current-release fresh-cache raw URLs, image checks
and zero-ownership results are in [verification evidence](v0.1.3-verification.json).
