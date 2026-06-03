# Fund an account on a local network

Get test ADA into an address on a local network in three steps: enable the
faucet on the Environment, find the address and faucet details with `yacd info`,
then send lovelace with `yacd topup`.

!!! warning "The faucet is local-only"
    The faucet exposes a spending endpoint, so it is opt-in and intended for
    local development networks. The faucet auth token is host-only and is never
    placed in the in-pod environment. See
    [Security](../concepts/security.md) for the trust model.

## 1. Enable the faucet

The faucet is off by default. Turn it on in the network's `chainAPI`. Optionally
enable the pre-funded developer wallet too, so you have a funded address from day
zero (the wallet requires the faucet and Kupo, and is local-mode only):

```yaml
spec:
  network:
    chainAPI:
      faucet:
        enabled: true
      wallet:
        enabled: true
```

Apply the manifest and wait for the network to become `Ready`. The faucet and
wallet defaults (sources, per-request lovelace bounds, wallet funding amount)
are documented in
[the CardanoNetwork reference](../reference/cardanonetwork.md). A complete
copy-paste manifest lives in the [recipes](../recipes.md).

`topup` refuses to run until the network publishes a faucet: it requires the
`Ready` and `FaucetReady` conditions to be `True` and fresh, and it reads the
faucet endpoint and auth Secret straight from status.

## 2. Find the address and faucet details

Print the network's status and connection information:

```sh
yacd info my-net
```

The text output includes an `Endpoints` section with the `faucet` URL, a
`Faucet` section with the auth Secret name, and — when the developer wallet is
enabled — a `Wallet` section with its `addr_test...` address and funded state:

```text
Wallet:
  Address: addr_test1...
  Key Secret: my-net-wallet-keys
  Funded: true
```

Use that wallet `Address` as your funding target, or supply any other testnet
address you control. Add `--json` for machine-readable output:

```sh
yacd info my-net --json
```

## 3. Send lovelace

The faucet is a cluster-internal Service, so run `topup` through `yacd run`,
which forwards the faucet to a local port and exposes it as `$YACD_FAUCET_URL`:

```sh
yacd run my-net -- sh -c \
  'yacd topup my-net --address addr_test1... --lovelace 1000000 --faucet-url "$YACD_FAUCET_URL"'
```

`--address` and `--lovelace` are both required, and `--lovelace` must be greater
than zero and within the faucet's configured min/max bounds. `topup` reads the
auth token from the published Secret automatically, and the loopback URL that
`run` exposes is exempt from the [trust gate](../concepts/security.md). On
success it prints the transaction ID, source, lovelace, and destination. Add
`--json` for a machine-readable result.

!!! note "Running `topup` without `run`"
    Without `--faucet-url`, `topup` targets the faucet URL the cluster published
    (`http://<network>-faucet.<namespace>.svc.cluster.local:<port>`). That name
    resolves only from inside the cluster, so a bare `yacd topup` works for
    in-cluster callers (such as a CI job running in a Pod) but not from your
    host. From your host, bridge it with `yacd run` as shown above.

The full `topup` flag set — including `--source`, `--faucet-url`, the
`--trust-faucet-url` / `--allow-insecure-faucet-url` trust gates, and the
`--await` options — is documented in
[the CLI reference](../reference/cli.md).

## Confirm on-chain

By default `topup` returns as soon as the faucet accepts the request. To block
until the funding transaction is actually confirmed on-chain, add `--await`,
which polls [Kupo](https://cardanosolutions.github.io/kupo/) for the new output:

```sh
yacd topup my-net --address addr_test1... --lovelace 1000000 --await
```

`--await` requires a Kupo URL. Pass `--kupo-url`, or run under `yacd run`, which
sets `YACD_KUPO_URL` automatically:

```sh
yacd run my-net -- sh -c \
  'yacd topup my-net --address "$ADDR" --lovelace 1000000 --faucet-url "$YACD_FAUCET_URL" --await'
```

The loopback faucet URL exposed by `run` is exempt from the trust gate, so no
`--trust-faucet-url` is needed there. When the output appears, `topup` prints
`Confirmed on-chain.` and exits. See
[Host access and the `YACD_*` contract](connecting-tools.md) for how `run`
bridges the cluster-internal endpoints to your host.

## See also

- [CLI reference](../reference/cli.md) — every `topup` flag and default.
- [CardanoNetwork reference](../reference/cardanonetwork.md) — faucet and wallet
  spec fields and defaults.
- [Security](../concepts/security.md) — the faucet trust gate and host-only
  token rationale.
