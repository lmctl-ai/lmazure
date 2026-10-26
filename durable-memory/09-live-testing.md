# Live procedure and genuinely open items

Status: **READY TO TEST; live Azure testing is pending the user**. Development,
local tests and CI have used fakes/builtin fixtures only. No authentication,
AzureRM provider plan, real resource lifecycle or billing calibration is claimed.

Follow [docs/LIVE_TESTING.md](../docs/LIVE_TESTING.md), not the mock gallery:

1. Download the matching v0.1.3 executable, verify SHA256SUMS and run `--self-test`
   (still fake; no .NET runtime needed).
2. With explicit user approval, use an existing local Azure CLI profile/Reader
   permissions. Set `LMAZURE_LIVE=1`, `LMAZURE_SUBSCRIPTION_ID`, `LMAZURE_REGION`,
   `AZURE_TOKEN_CREDENTIALS=AzureCliCredential`; leave writes disabled. Run the
   documented live inventory and inspect its output locally.
3. Separately approve writes. Use an existing storage account with already-held
   Blob/Queue Data Contributor permissions, set its blob/queue URLs and
   `LMAZURE_ALLOW_WRITES=1`, then run the documented owned-resource smoke script.
   No account, identity, credential or role assignment is created by the test.
4. Confirm teardown/zero leftovers, retain failed ledgers for dry sweep/recovery,
   and unset live/write flags. Never share credentials, endpoints or raw state.

For equivalents, follow [compare/README.md](../compare/README.md): additionally
set `LMAZURE_COMPARE_LIVE_APPROVED=1`, use the same subscription/account and
review exact-name preflight/cleanup rules before `compare/run.sh <name> --live`.
IAM live testing is excluded by the brief; identity examples are metadata-only.

Open: user-run authentication/RBAC/endpoint checks; actual AzureRM validation and
72 live comparison rows; retained-ledger recovery/zero Azure leftovers; regional
price review and optional billing calibration. Live figures remain “estimated
from price table, not yet measured live.”

Known live-report integration issue: `compare/run.py` emits `user_approved`, while
`compare/compare_results.py` requires `user_approved_actual` before labeling a live
record accepted. Thus a future successful live harness run would still be labeled
pending. Reconcile these fields with the approval checks and a regression test
before accepting live comparison evidence; do not simply relabel offline records.
This documentation refresh does not change executable behavior.
