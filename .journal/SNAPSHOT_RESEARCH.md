# Cardano Snapshot Research

Status: research notes backing [`SNAPSHOT_DESIGN.md`](./SNAPSHOT_DESIGN.md).

This captures the essence of a deep research pass on Cardano Mithril snapshots
and the slot/time re-anchoring problem, focused on what it means for YACD. All
findings rest on primary sources (IOG/Cardano docs, the `input-output-hk/mithril`
repo and its security advisory, the peer-reviewed STM paper, Yaci DevKit docs);
they were cross-checked and are high-confidence unless noted. Version-specific
numbers are point-in-time (mid-2025 era) and should be treated as the mechanism,
not a fixed contract.

## 1. What Mithril Is

Mithril is a **stake-based threshold multi-signature (STM)** protocol. Eligible
stakeholders individually sign a message in a stake-weighted lottery; an
aggregator combines those signatures into one compact multi-signature **only
once supporting stake crosses a quorum threshold**. A Mithril *snapshot* is a
separate application layer on top: a certified copy of part of the Cardano chain
database, with a certificate chain anchored to a published genesis verification
key.

Roles:

- **Signers** (run by SPOs) produce individual signatures.
- **Aggregator** combines them into multi-signatures and serves certified
  snapshots.
- **Client** (`mithril-client`) downloads and verifies artifacts, recomputing
  the signed message from the downloaded files and checking it against the
  certificate chain up to the trusted genesis key.

Primary use case: fast-bootstrap a node on a public network (mainnet restore in
~20 min vs. syncing from genesis) with the chain's own security guarantees.

## 2. What a Mithril Snapshot Does and Does NOT Contain

This was the central question, and the answer is decisive: **a Mithril snapshot
is not self-contained.**

- The multi-signature covers **only the immutable (finalized) chain files** —
  and even then, the signed message is computed from **all immutable files
  except the last one** (still being created), which is excluded.
- **Ledger state and the latest immutable file are explicitly excluded from the
  Mithril signature.** They ship only as separately-downloaded, opt-in
  **"ancillary"** files, protected by a *different and weaker* trust mechanism:
  an Ed25519 signature checked against an ancillary verification key (the subject
  of security advisory GHSA-qv97-5qr8-2266). The protocol "cannot currently"
  multi-sign them because they differ across signers.
- **As of `mithril-client` v0.12.1 (2025-05-06), ancillary files are NOT
  downloaded by default.** Without them, fast bootstrap is disabled and the node
  must **recompute ledger state from genesis** at startup. Fast bootstrap
  requires `--include-ancillary` plus the ancillary verification key.
- In **all** cases the restored node sits **behind the chain tip** and must
  still **sync the volatile/recent blocks** from peers to become current.

## 3. Product-Fit Verdict for YACD

**Mithril is the wrong foundation for YACD's localnet test-scenario snapshots,
but the right tool for one specific other path.** These are two separate
features:

| Use case | Mechanism |
|----------|-----------|
| Fast-join a **public** network (preprod/preview/mainnet) | **Consume** existing Mithril snapshots via `mithril-client` (don't reinvent) |
| **Localnet** test scenarios (CI fixtures, deterministic state) | **Bespoke** YACD snapshot format |

Why Mithril is unfit for localnet test scenarios:

1. **Content emphasis is inverted (the disqualifier).** Mithril signs only
   *immutable* (finalized) data, sealed only after `k` blocks. A test fixture's
   whole value is the state you *just* produced; on a localnet that lives in the
   **volatile** DB and is not yet immutable. A Mithril-shaped snapshot would
   systematically exclude the very state the fixture exists to capture. Even the
   ancillary escape hatch carries ledger state + last immutable, never volatile
   blocks.
2. **Trust model is moot.** Mithril's value is proving a quorum of *independent*
   stakeholders signed. On a localnet you own 100% of the stake; self-signing
   proves nothing. The expensive part of Mithril is exactly the worthless part
   here.
3. **Infra weight.** An aggregator + signer population + genesis-VK ceremony is
   wildly disproportionate to an ephemeral CI localnet.

"Certified test snapshots" are still worth it — but at a *much* lower altitude.
The value in a test/CI context is **integrity + provenance**, not consensus
trust, which is solved by a content hash + optional detached signature
(cosign/sigstore or plain ed25519), composing with the OCI-artifact packaging
already sketched in the design doc. This is the cheap, correct version of
Mithril's "certificate" idea without the STM machinery.

## 4. What the Bespoke Localnet Format Must Capture

It is strictly *more* than Mithril, and partly inverted (volatile-first). For a
localnet to restore to a reproducible, **block-producing** network:

- **Genesis material** (Byron/Shelley/Alonzo/Conway) — network identity, initial
  funds, protocol params, system start.
- **Full chain DB** — immutable **+ volatile + ledger state** (the inverse of
  Mithril's emphasis).
- **Block-production keys** — KES/VRF/cold keys + operational cert. Mithril
  never carries these; a Mithril-restored node is a follower, not a producer.
- **Wallet/faucet material** — funded addresses, signing keys, faucet funding
  state, so the harness has known spendable UTxOs.
- **Manifest** — scenario name, `cardano-node` version, ledger backend (UTxO-HD
  InMemory vs on-disk LMDB), era/protocol version, network magic, **tip slot +
  slot length + original systemStart** (needed for re-anchoring; see §5), and a
  content hash.

This aligns with `SNAPSHOT_DESIGN.md` §3, which already says node snapshots must
capture the full node DB plus generated network material (not just `db/ledger`)
and preserve block-production material exactly.

## 5. The Slot/Time Re-Anchoring Problem (Biggest Engineering Risk)

Cardano slots are wall-clock-derived from genesis `systemStart`. Restore a chain
DB at a later wall-clock time and the node believes it is massively behind the
current slot. The real failure mode is **not** "replay empty slots" — it is the
**forecast/stability window**: a producer computes its leadership schedule only
a bounded number of slots ahead (≈ `3k/f`). If the current wall-clock slot is
far beyond the chain tip's slot, the node cannot forecast leadership across the
gap and **wedges** (the `OutsideForecastRange` / `PraosCannotForecast` family).
A related failure is **KES key expiry**: KES periods are slot-derived, so a long
real-time gap can push the current KES period outside the operational cert's
validity window, blocking minting.

### Has anyone solved this?

Partially, and **no off-the-shelf tool automates restore-after-arbitrary-delay.**

- **Yaci DevKit (closest analogue) punts.** Its `stop`/`start` resumes "from the
  last block it was stopped at," but the docs warn verbatim: *"Based on the
  `securityParam` configuration in devnet node, the node may get stuck if it is
  stopped for a long time."* Its actual snapshot commands
  (`take-db-snapshot` / `rollback-to-db-snapshot`) are explicitly *"non-
  consensus"* rollback simulation, not time-travel restore. So even the leading
  dev product treats this as a documented gotcha, mitigated by cranking
  `securityParam`.
- **`systemStart` re-anchoring is the real lever.** Local-testnet generators
  already set `systemStart` relative to launch (e.g.
  `--start-time "now + 2 minutes"`). The restore technique: rewrite
  `systemStart = now − (tipSlot × slotLength)` so the tip re-aligns with the
  current wall clock. It works because Cardano time is slot-relative and
  hard-fork transition points are slot/epoch-based, not calendar-based; historic
  blocks simply become "recently in the past." Re-anchoring so
  `currentSlot ≈ tipSlot` closes the forecast gap **and** keeps the KES period
  inside the cert window in the same move. No tool was found that automates this
  *on restore* (vs. only at genesis).
- **`libfaketime`** (LD_PRELOAD clock interception) could freeze the node's
  perceived time to the snapshot time — a plausible building block, but not
  documented in public sources combined with cardano-node, and riskier
  (continuously fights the clock vs. a one-time genesis fix).
- **`securityParam` / slot-length tuning** is the blunt mitigation: widen the
  stability/forecast window so longer pauses are tolerated. Buys time; does not
  solve arbitrary-delay restore.

### Implication for YACD

The re-anchoring approach is viable and is the thing to build. The manifest must
record `tipSlot`, `slotLength`, and original `systemStart`; on restore an init
container rewrites genesis `systemStart` to re-anchor the tip to ~now before the
node starts, with a generous `securityParam` and long KES period in the scenario
genesis as belt-and-suspenders.

This **refines `SNAPSHOT_DESIGN.md` answered-question #5**: preserving pool/KES/
VRF/opcert material "exactly" is necessary but not sufficient. Without time
re-anchoring, an exactly-preserved localnet snapshot will still wedge (forecast
window) or fail to mint (KES expiry) when restored after a delay — which is the
normal case for a CI fixture stored and reused later. Re-anchoring on restore
should be treated as a first-class YACD capability, explicitly called out as
something competitors (DevKit) leave to the user.

## 6. Cross-References Into the Design Doc

- §2 here confirms `SNAPSHOT_DESIGN.md` §4's plan to use
  `mithril-client cardano-db download --include-ancillary` for public node
  bootstrap, and explains *why* `--include-ancillary` is required (default omits
  ledger state → recompute from genesis). It also reinforces recording the
  resolved digest for `latest`.
- §3 here supports the design's two-track split (YACD-native vs. public
  consumption) and the "don't repackage public artifacts" principle.
- §3's certification note maps to design open-question #9 (manifest
  authentication): detached signatures / attestations are the right altitude;
  Mithril-grade multisig is not needed.
- §5 here adds a new requirement to the manifest (`tipSlot`, `slotLength`,
  `systemStart`) and refines answered-question #5.

## References

- Mithril STM paper: https://eprint.iacr.org/2021/916.pdf
- Mithril repo: https://github.com/input-output-hk/mithril
- Mithril client: https://mithril.network/doc/manual/develop/nodes/mithril-client/
- Mithril bootstrap: https://mithril.network/doc/manual/getting-started/bootstrap-cardano-node/
- Ancillary-default change (client v0.12.1): https://mithril.network/doc/dev-blog/2025/05/06/client-breaking-change/
- Ledger-state exclusion advisory: https://github.com/input-output-hk/mithril/security/advisories/GHSA-qv97-5qr8-2266
- Cardano docs (Mithril): https://docs.cardano.org/developer-resources/scalability-solutions/mithril
- Yaci DevKit commands (stop/start, db-snapshot caveats): https://devkit.yaci.xyz/commands
- cardano-testnet (relative start-time): https://developers.cardano.org/docs/get-started/cardano-testnet/
- libfaketime: https://github.com/wolfcw/libfaketime
