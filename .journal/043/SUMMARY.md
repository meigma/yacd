---
id: 043
title: Session-041 review fixes + cardano-tools/F0 image foundation
date: 2026-05-30
status: complete
repos_touched: [yacd]
related_sessions: [030, 036, 041, 042]
---

## Goal
Session 043 began as an empty `session-new` placeholder and was then driven by
two parallel work-streams that shared this journal folder:

- **Stream A (cardano-tools / F0):** continue the `.journal/042` next steps —
  land the `cardano-tools` image foundation (manager seam, PR-CI build,
  static-musl guard), cut the first `cardano-tools` release, then begin the F0
  transport redesign (public profiles staged on a PVC, drop the manager
  `//go:embed`).
- **Stream B (session-041 review):** run a multi-agent review of the
  session-041 CLI code (host-access verbs + the `YACD_*` contract) and fix the
  findings.

## Outcome
- **Stream B — fully met.** A 9-dimension / adversarially-verified review
  produced 26 confirmed findings (0 critical, 0 high, 5 medium, 12 low, 9 nit);
  the code was judged solid and shippable. All five medium findings were fixed
  and **merged** across four PRs (#69, #70, #71, #72).
- **Stream A — partially met.** Items 7/8/9/10 are **done and merged** (PR #68
  image seam/PR-CI/guard; PR #65 release → published
  `ghcr.io/meigma/yacd/cardano-tools:11.0.1-yacd.4`
  `@sha256:9ca9e03348c3f9d22408be36f1525c3ef518ab6e0b0053b0a05f2b8401a6039e`).
  The **F0 transport redesign (PR2, items 1/2/3/6) is banked but unmerged**:
  slices 1 and 3 are committed and pushed to `feat/f0-public-profile-pvc` (no PR
  opened); the coupled controller-rewrite slices (2/4/5/6) were deliberately
  deferred for interactive, dev-stack-backed work. This is the session's main
  open thread.

## Key Decisions
### Stream B (review fixes)
- `correctness-kube-1` fix uses a `WrapTransport` on a `rest.CopyConfig` copy to
  inject a caller-derived, `forwardDialTimeout`-bounded context into the SPDY
  upgrade → `spdy.RoundTripperFor` ignores `rest.Config.Dial` (a `net.Dialer`
  timeout would be a no-op), but the round-tripper honors `req.Context()` across
  TCP+TLS. The ctx.Done branch uses a non-blocking `requestStop()` so cancel
  returns promptly; the shared `rest.Config` is untouched and the live stream
  rides the hijacked connection.
- `ux-1` exec made interactive only when **both** stdin and stdout are terminals
  (raw mode + SIGWINCH size queue). The size queue stays behind the
  `kube.Client` port via a port-local `kube.TerminalSize`/`TerminalSizeQueue`
  vocabulary + an adapter-internal shim → the `cli` layer never imports
  client-go's `remotecommand`; the `Client` signature is unchanged (no mock
  regen).
- `ux-2` topup `--await` notice goes to **stderr** (keeps `--json` clean) and is
  best-effort (the tx is already submitted). `tests-2` pins the "await at the
  **requested** address" invariant with a faucet that echoes a different address.
- PR-3 (#72) was **stacked** on PR-1's branch (shared `access.go`); after #69
  merged it was rebased onto master (the redundant PR-1 commit auto-dropped) and
  retargeted, so its squash diff is PR-3 only.

### Stream A (cardano-tools / F0) — locked for the PR2 resume
- **Genesis pinning = trust the remote source** (behavior-preserving): `fetch`
  pins only config.json + topology.json + Mithril vkeys (8 digests); genesis +
  checkpoints are downloaded UNPINNED and verified downstream by `cardano-node`
  via config.json inline hashes. Matches the already-merged `fetch/pins.go`.
- **Public fingerprint** becomes `sha256(json{schemaVersion, profile,
  networkMagic, requiresNetworkMagic, files:[pinned digests only]})` — genesis
  bytes excluded (manager won't have them post-embed). Goldens change
  intentionally (OK pre-1.0; no persisted public networks).
- **Custom profiles unchanged** — the PVC-fetch + manifest-only-ConfigMap model
  is curated-only; custom keeps its byte-based ConfigMap path.
- **Mithril vkeys stay embedded** (223B/221B; their content is passed as
  mithril-client env), dropping only the large genesis/config/topology embed.

## Changes
Merged to `master` this session:
- #68 `feat(operator): cardano-tools image seam, PR-CI build, static-musl guard`
  — new `internal/cardano/toolsimage`, `--default-cardano-tools-image` flag +
  Helm `cardanoTools.image.*`, `cardano-tools-image` CI job, Dockerfile static
  guard.
- #65 `chore(master): release cardano-tools 11.0.1-yacd.4` — first cardano-tools
  release (published the digest above).
- #69 `fix(cli): bound port-forward dial so cancellation returns promptly` —
  `cli/internal/kube/access.go` + tarpit/guard tests.
- #70 `test(cli): cover connect reconnect, backoff, and fatal-NotFound paths` —
  `cli/internal/cli/connect_test.go` (test-only).
- #71 `fix(cli): announce topup --await polling and pin the await-address
  invariant` — `cli/internal/cli/topup.go` + `topup_await_test.go`.
- #72 `feat(cli): make yacd exec interactive with raw mode and window resize` —
  `cli/internal/cli/exec.go`, `cli/internal/kube/access.go`,
  `docs/host-access.md` + tests.

Banked on `feat/f0-public-profile-pvc` (pushed, **no PR**):
- slice 1 `internal/cardano/publicpins` (464a960) — shared curated profile
  registry (pinned digests + per-profile static identity), cross-check test.
- slice 3 (eb96db9) — `containers/cardano-tools/internal/fetch/pins.go`
  collapsed to a thin adapter over `publicpins` (golden-locked, behavior-preserving).

## Open Threads
- **F0 PR2 — the coupled controller rewrite (slices 2/4/5/6).** NOT independently
  landable: removing the `//go:embed` only works once the fetch-init →
  `/state/profile` + node-mount repoint + manifest-only ConfigMap + mode-aware
  dataContract land together (one behavioral change for curated public networks,
  needs Kind/Tilt preview-network live-fetch validation). Full step-by-step
  resume checklist + locked design answers are in the final NOTES entries of
  this session (`2026-05-31 (later) — Pausing autonomous impl…`). Resume in a
  **new session** off fresh master, branch `feat/f0-public-profile-pvc` (slices
  1+3 already there), dev stack up. Pin the manager default to
  `@sha256:9ca9e033…` (tag `yacd.4`) in the final slice.
- **#72 interactive QA (carry-over):** the exec raw-mode/resize path was merged
  on the user's approval; its interactive behavior (single echo, line editing,
  resize reflow, clean restore) can only be verified at a real terminal — worth
  a manual pass against the dev stack.
- **Carried forward (pre-existing):** TEST_REPORT F2/F4; test-harness Phases 3
  (release), 4 (`yacd-env` Action), 5 (examples + how-to).
- **e2e-hardening backlog:** Docker Hub anon 429 on `cardanosolutions/ogmios` +
  `/kupo` (authenticated pulls or preload/mirror); the load-sensitive
  `TestCardanoNetworkControllerManagerAttachesPrimarySidecarDBSync` envtest flake
  (rerun `ci`; worth a longer `Eventually`/poll). Both bit this session's merges.
- **Worktrees left in place (parallel stream):**
  `.wt/feat-f0-public-profile-pvc` (PR2 in progress — do NOT remove) and
  `.wt/feat-cardano-tools-image-foundation` (PR1/#68 merged — removable). The
  shared `kind-yacd-dev` dev stack was left **running** (the F0 stream owns it).

## References
- Merged PRs: #68, #65, #69, #70, #71, #72 (all squash-merged; `master` at the
  `#72` commit).
- Published image: `ghcr.io/meigma/yacd/cardano-tools:11.0.1-yacd.4`
  `@sha256:9ca9e03348c3f9d22408be36f1525c3ef518ab6e0b0053b0a05f2b8401a6039e`.
- F0 branch: `feat/f0-public-profile-pvc` (slices 1+3).
- Review workflow script: `.journal/043/session-041-review.workflow.js`.
- Plans: `.claude/plans/ok-please-propose-a-curious-toucan.md` (F0/cardano-tools),
  `.claude/plans/ok-please-propose-a-lovely-milner.md` (review fixes).
- Prior: `.journal/041/SUMMARY.md` (reviewed work), `.journal/042/SUMMARY.md`
  (cardano-tools foundation + F0 next steps).

## Lessons
- One journal session folder ended up shared by two concurrent work-streams.
  It worked because each committed its own NOTES checkpoints and touched
  disjoint code (CLI vs controllers/containers), but the cross-stream picture
  was only reconstructable from the running log. Prefer one session per
  work-stream; if they must share, keep NOTES entries clearly stream-labeled.
- Stacked PRs + squash-merge: rebasing the child onto the squashed parent
  cleanly drops the now-redundant parent commit (`git rebase` auto-drops the
  emptied cherry-pick), leaving a child diff that is child-only. Retarget the
  base and force-push before merging.
