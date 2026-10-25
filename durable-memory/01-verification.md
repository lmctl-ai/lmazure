# Verification record

The source was built with .NET 10 using:

```text
env PATH=/tmp/lmazure-dotnet:$PATH dotnet build src/LmAzure/LmAzure.csproj -c Release -p:PublishAot=false --no-restore
```

The build completed with zero warnings and zero errors. The fake transport
self-test currently reports 62 checks: 27 core fixture checks and 35 safety
checks. The reported breakdown is retained as the release acceptance target.

The broader checked-in validation counts are 31 CLI checks, 11 DSL checks on
Linux, 10 DSL checks on other hosts, and 27 example checks. The main and tag CI matrices passed on all five platforms; release artifacts
and all 27 fresh-cache README autoruns also passed. See 04-release.md.

The fixture covers all curated inventory kinds, valid nested queue resource
identifiers, deterministic ages and locations, public/encrypted unknown values,
ledger ownership, create conflict handling, metadata mismatch protection,
cleanup retention, dry sweeps, secret projection, bounded response handling,
and custom fake-handler injection. No test in this record contacts Azure or loads Azure
credentials.
