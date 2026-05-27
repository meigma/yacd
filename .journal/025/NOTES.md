---
id: 025
title: Session 025
started: 2026-05-26
---

## 2026-05-26 21:00 — Kickoff
Goal for the session: start a new journal-backed work session; wait for the user's actual implementation or exploration request before making product changes.
Current state of the world: `master` is clean at `f5bbfbb` from PR #42, which fixed the dev stack by adding the `--default-cardano-testnet-image` override, chart value, and Tilt-local cardano-testnet rebuild path. The journal branch `journal/jmgilman` is clean and up to date, with sessions 022-024 read for context. Session 024 completed the post-refactor manual functional pass and left the dev stack stopped. The immediately preceding discussion explored the public-network db-sync topology problem: the current dedicated follower-node shape is clean for ownership but wasteful for preview/preprod/mainnet, and the leading architectural option was to keep `CardanoDBSync` as the service/config/status owner while letting `CardanoNetwork` be the sole primary Pod composition authority for any db-sync sidecar placement.
Plan: wait for the user's next request, then choose or create an implementation worktree, start `moon run root:dev-up` from that worktree before substantive implementation unless waived, and keep session notes updated at meaningful checkpoints.
