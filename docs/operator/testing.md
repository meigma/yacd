# Testing & CI

Use YACD to give an automated test suite a real, ephemeral Cardano network and
tear it down deterministically afterward. The pattern is three commands:

```sh
yacd up   my-net -f env.yaml --wait     # create, block until Ready
yacd run  my-net -- <test command>      # run tests with YACD_* wired in
yacd down my-net --wait                 # delete, block until gone
```

Your test runner stays YACD-agnostic: it reads the `YACD_*` environment
variables `run` injects and never parses a YACD file, so the same suite works on
a laptop and in CI. For the meaning of those variables and the `run`/`exec`
semantics, see [Host access and the `YACD_*` contract](../developer/connecting-tools.md);
for every flag and default, see the [CLI reference](../reference/cli.md).

## 1. Bring the network up, gated on Ready

`yacd up` server-side-applies the `CardanoNetwork` rendered from your
environment file and, with `--wait` (the default), blocks until the network
reports Ready before returning a zero exit code. A non-zero exit means the
network never became Ready, so a CI step can depend on `up` succeeding:

```sh
yacd up my-net -f env.yaml --wait
```

`--wait` is on by default; pass `--wait=false` to apply without blocking. The
readiness deadline is `--timeout` (default 12m). Tune it for slow runners or
larger profiles; raising it does not change what "Ready" means, only how long
`up` will wait for it. See the [CLI reference](../reference/cli.md) for the full
flag set.

## 2. Run the suite under `yacd run`

`yacd run NAME -- <command>` establishes scoped port-forwards to the network's
chain APIs, exports the `YACD_*` environment into the command, runs it on the
host, and tears the forwards down when it exits. The command's exit status is
propagated to your shell, so the test runner's pass/fail code is what CI sees:

```sh
yacd run my-net -- go test ./e2e/...
```

Always put `--` before the test command so its own flags are passed through to
it rather than parsed by `yacd`. Inside the command, the runner reads endpoints
from `YACD_OGMIOS_URL`, `YACD_KUPO_URL`, and `YACD_FAUCET_URL` (loopback URLs on
the host) plus `YACD_FAUCET_TOKEN` for the host-only faucet. The variable names
and the loopback/faucet-token details are documented once in
[Host access and the `YACD_*` contract](../developer/connecting-tools.md).

!!! warning "Use `exec`, not `run`, for `cardano-cli`"
    `run` forwards Ogmios, Kupo, and the faucet over TCP. Tools that reach the
    node over its local Unix socket (notably `cardano-cli`) need
    `yacd exec NAME -- …` instead, which runs in the node Pod and sets
    `CARDANO_NODE_SOCKET_PATH`. See
    [Choose a verb](../developer/connecting-tools.md#choose-a-verb).

## 3. Tear down deterministically

`yacd down` deletes the `CardanoNetwork` and, with `--wait` (the default),
blocks until the object and its garbage-collected children are gone before
returning. Deletion is idempotent: a network that is already absent is reported
as success, so teardown is safe to run unconditionally in a cleanup step.

```sh
yacd down my-net --wait
```

The removal deadline is `--timeout` (default 5m). Run teardown even when the
test step failed, so a broken run never leaks a cluster network.

## Minimal CI snippet

The three steps map directly onto a CI job. Bring the network up, run the suite,
and tear down whether or not the suite passed.

```yaml
steps:
  - name: Bring up the network
    run: yacd up my-net -f env.yaml --wait

  - name: Run the test suite
    run: yacd run my-net -- go test ./e2e/...

  - name: Tear down the network
    if: always()
    run: yacd down my-net --wait
```

`if: always()` (or your CI's equivalent) guarantees teardown runs after a
failed test step. Because `down` is idempotent, the cleanup step is also safe to
keep even if `up` never created the network.

## Selecting the cluster and namespace

`up`, `run`, and `down` all accept the global `--kubeconfig`, `--context`, and
`-n/--namespace` flags, so a CI runner can target a cluster without editing the
test command. These globals are documented once in the
[CLI reference](../reference/cli.md).

When `--namespace` is not set, the namespace defaults to the network name, so
`yacd up my-net …` applies the network into the `my-net` namespace and `up`
auto-creates that namespace if it does not exist.

!!! warning "Mainnet is not a CI target"
    `yacd up` refuses to apply a mainnet network without `--allow-mainnet`,
    because mainnet creates large persistent volumes and bootstraps from
    Mithril. Automated tests should use a local devnet or a public testnet
    profile, not mainnet.
