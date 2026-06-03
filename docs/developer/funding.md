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

Fund an address with an exact lovelace amount. `topup` reaches the faucet on its
own — with no `--faucet-url` it opens a short-lived port-forward, POSTs, and
tears it down — so it works directly from your host with no `yacd run` wrapper:

```sh
yacd topup my-net 1000000 --address addr_test1...
```

`LOVELACE` is a positional argument, `--address` is required, and the amount must
be greater than zero and within the faucet's configured min/max bounds. `topup`
reads the auth token from the published Secret automatically, and the
self-forwarded loopback URL is exempt from the
[trust gate](../concepts/security.md). On success it prints the transaction ID,
source, lovelace, and destination. Add `--json` for a machine-readable result.

!!! note "Inside `yacd run`, or with an override"
    `topup` honors an ambient `YACD_FAUCET_URL` (set inside `yacd run`), so it
    works unchanged there without opening a second forward. An explicit
    `--faucet-url` suppresses self-forwarding; a custom non-loopback value then
    requires `--trust-faucet-url` (and `--allow-insecure-faucet-url` for
    `http://`).

The full `topup` flag set — including `--source`, `--faucet-url`, the
`--trust-faucet-url` / `--allow-insecure-faucet-url` trust gates, and the
`--await` options — is documented in
[the CLI reference](../reference/cli.md).

## Confirm on-chain

By default `topup` returns as soon as the faucet accepts the request. To block
until the funding transaction is actually confirmed on-chain, add `--await`,
which polls [Kupo](https://cardanosolutions.github.io/kupo/) for the new output:

```sh
yacd topup my-net 1000000 --address addr_test1... --await
```

When `topup` self-forwards it reuses that same session's Kupo, so `--await`
needs no extra flags. When the output appears, `topup` prints `Confirmed
on-chain.` and exits. If you override the faucet with `--faucet-url`, supply a
matching `--kupo-url` for `--await`. See
[Connecting tools and tests](connecting-tools.md) for how `run` bridges
cluster-internal endpoints to your host.

## See also

- [CLI reference](../reference/cli.md) — every `topup` flag and default.
- [CardanoNetwork reference](../reference/cardanonetwork.md) — faucet and wallet
  spec fields and defaults.
- [Security](../concepts/security.md) — the faucet trust gate and host-only
  token rationale.
