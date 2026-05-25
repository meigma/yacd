---
id: 016
title: New YACD session
started: 2026-05-24
---

## 2026-05-24 22:15 — Kickoff
Goal for the session: Start a new journal session and wait for the user's substantive request.
Current state of the world: `journal/jmgilman` is clean and up to date. Session 015 was the most recent session and has not been closed: its `NOTES.md` records phase 6 assessment work and the start of progress-probe implementation on the `feat/dbsync-progress-probes` Worktrunk branch at `.wt/feat-dbsync-progress-probes`, which currently carries uncommitted changes (`+518/-58`, modified and untracked files). The local dev stack is still running, owned by that same implementation worktree per `.run/yacd-dev/`. Last closed session was 014 (PR #24, managed Postgres for `CardanoDBSync`). Per `TECH_NOTES.md`, YACD has `CardanoNetwork`, Kupo, faucet, `CardanoDBSync` external Postgres, and managed Postgres foundations; `DBSyncReady`/`Synced`/aggregate `Ready` are still `RuntimeProbesPending` pending sync-lag probes.
Plan: Wait for the user's actual request. If they want to resume the in-progress progress-probe work, prefer `/session-continue 015` so the existing notes and worktree stay coherent rather than fragmenting across two open sessions; otherwise proceed per the new request. For new implementation work, select or create the appropriate Worktrunk implementation worktree, load task-relevant skills (e.g. `k8s-operator`) before touching APIs/controllers/tests, and confirm the dev stack state before reusing or restarting it.

## 2026-05-24 22:55 — Publisher Cobra/Viper scaffold
Goal for the checkpoint: Land scaffolding for a clean `containers/cardano-testnet/publisher` Cobra/Viper module, no logic ported.
Current state of the world: Created Worktrunk implementation branch `feat/dbsync-publisher-cli-scaffold` at `.wt/feat-dbsync-publisher-cli-scaffold/`. The new nested module lives at `containers/cardano-testnet/publisher/` (module path `github.com/meigma/yacd/containers/cardano-testnet/publisher`) with `cmd/main.go`, and `internal/cli/{root,publish,version}.go`. `publish` declares all 15 existing env-var options as flags; Viper's `YACD` prefix plus `-`/`_` key replacer means each flag also resolves from the matching `YACD_<UPPER_SNAKE>` env var, preserving the existing env-var contract byte-for-byte. `version` prints `<binary> <ver> (<commit>) built <date>`. The existing `containers/cardano-testnet` module, `Dockerfile`, old `cmd/yacd-cardano-testnet-publisher/`, and `internal/artifactpublisher/` are intentionally untouched and still build. Both modules verified with `go build ./...` and `go vet ./...`; `--help` and env/flag publish smoke commands exit 0.
Validation: PR #25 (`feat(cardano-testnet): scaffold publisher Cobra/Viper module`) opened against `master`.
Plan: Follow-up session ports `internal/artifactpublisher` logic into the new module, then updates `Dockerfile` (build path `./cmd/yacd-cardano-testnet-publisher` → new module entry) and Moon project discovery.
