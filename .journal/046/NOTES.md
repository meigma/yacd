---
id: 046
title: F0 redesign — PR-A continuation (serve sidecar + manifest)
started: 2026-05-31
---

## 2026-05-31 13:08 — Kickoff
Goal for the session: continue the F0 redesign work begun in session 043 (the
"manager is not an authoritative config source / remove the network-artifacts
ConfigMap entirely" redesign). The session-043 NOTES end with a full
"HANDOFF — START HERE" section; the immediate next step is **PR-A / A2** (wire
the always-on cardano-tools `serve` native sidecar into the primary Deployment
and get a `manifest.json` written into the served dir).

Current state of the world:
- `master` is clean at `2f28360 fix(cli): harden review findings (#73)` (session
  044). The personal journal branch `journal/jmgilman` is clean/up to date after
  the session-045 checkpoint.
- Implementation worktree already exists: `.wt/feat-f0-public-profile-pvc`,
  branch `feat/f0-public-profile-pvc` @ `41def22`, attached and clean. It carries
  4 commits on top of master and is **1 commit BEHIND** current `master`
  (it was last rebased onto `dbaa886`; `#73`/`2f28360` landed afterward) — rebase
  onto `2f28360` before resuming.
- The 4 banked commits (all behavior-additive / golden-locked, no PR opened):
  `0f00ad0` publicpins registry, `83cdb7f` fetch→publicpins adapter, `cd87128`
  publicpins static per-profile identity, `41def22` **A1** served-artifact
  manifest contract (`internal/cardano/networkartifacts/manifest.go` + `ManifestKey`
  added to optional contract keys + golden fix). `moon run root:test` and
  `root:check` were green at `41def22`.
- Items 7/8/9/10 of the original F0 plan are DONE+merged on master (cardano-tools
  image seam/PR-CI/static-musl guard; published
  `ghcr.io/meigma/yacd/cardano-tools:11.0.1-yacd.4`
  `@sha256:9ca9e03348c3f9d22408be36f1525c3ef518ab6e0b0053b0a05f2b8401a6039e`).
- Session 045 (CardanoNetwork node sync status, branch
  `feat/cardanonetwork-sync-status`) remains `in-progress`/dormant and is a
  SEPARATE work-stream — not touched by this session.
- Dev stack: `.run/yacd-dev` is owned by the OLD PR1 worktree
  (`.wt/feat-cardano-tools-image-foundation`), context `kind-yacd-dev`. For an
  in-cluster smoke from this branch it must be repointed (`dev-down` then
  `dev-up` from `.wt/feat-f0-public-profile-pvc`). Not started yet this session.

Redesign (DECIDED in 043 — do not relitigate): manager holds no configs, only
`publicpins` metadata; local generates / public fetches configs onto the node
state PVC at `/state/profile`; `cardano-node` reads from the PVC (no ConfigMap);
every other consumer (db-sync, CLI, external) fetches over HTTP from an always-on
cardano-tools `serve` sidecar + owned ClusterIP Service; integrity/discovery via
a served `manifest.json` (schemaVersion + per-file sha256). PR order is
**A → C → B → D** (verified: A→B→C→D bricks db-sync). PR-A is additive (ConfigMap
stays) so build + chainsaw stay green throughout.

Key gotchas carried from 043: (1) commits are GPG-signed and need the user present
for pinentry, else they cancel silently — verify HEAD moved; (2) intermittent
Read-tool corruption was reported all session — cross-check suspicious reads with
`git show HEAD:<path>` before editing; (3) use `moon run root:test` (not plain
`go test`, which lacks KUBEBUILDER_ASSETS); (4) re-check `git symbolic-ref HEAD`
around rebase/amend (detach risk).

Plan: review complete; summarize current state + next steps for the user and
await their go-ahead before bringing up the dev stack and resuming PR-A / A2.
References: `.journal/043/SUMMARY.md` + the "HANDOFF — START HERE" section at the
end of `.journal/043/NOTES.md`; plan
`.claude/plans/ok-please-propose-a-curious-toucan.md` (A→C→B→D section).
