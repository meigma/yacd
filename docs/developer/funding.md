# Fund an account on a local network

Every local YACD network is created with a genesis-funded `faucet` wallet. You
spend from it to fund developer wallets (or any testnet address) with the `yacd
wallet` verbs, which build, sign, and submit transactions directly against the
network's Ogmios and Kupo endpoints. There is no in-cluster faucet service.

!!! note "Local networks only"
    The genesis-funded `faucet` wallet is created only for `local` networks; it
    is funded at genesis from the localnet's initial UTxOs. Public networks
    (preview/preprod/mainnet) have no faucet wallet — fund those from your own
    funded keys.

## Create and fund a wallet

The quickest path is `yacd wallet add`, which generates a managed wallet and,
with `--topup`, funds it from the `faucet` wallet in one step. Add `--await` to
block until the funding transaction is confirmed on-chain:

```sh
yacd wallet add my-net --topup 5000000 --await
```

This prints the new wallet's name and `addr_test...` address, the funding
transaction id, and `Confirmed on-chain.` once Kupo sees the output. Omit
`--topup` to create an unfunded wallet, and pass `--name` to choose the name
instead of the generated adjective-noun default.

Managed wallet keys are stored as labeled Kubernetes Secrets
(`<network>-wallet-<name>`) in the network's namespace; the CLI reads them to
sign locally. See [Security](../concepts/security.md) for the custody model.

## Fund an existing wallet or address

`yacd wallet topup` funds an existing target with an exact lovelace amount. The
`WALLET` argument is a managed wallet name, a public key, or a bech32
`addr_test...` address, so you can also fund an address you do not manage:

```sh
yacd wallet topup my-net bright-sun 1000000 --await
yacd wallet topup my-net addr_test1... 1000000
```

By default the funds come from the `faucet` wallet; pass `--from <wallet>` to
spend from another managed wallet instead. `topup` forwards Ogmios and Kupo
itself, so no `yacd run` wrapper or URL flags are needed. Add `--json` for a
machine-readable result.

## List, export, and remove wallets

```sh
yacd wallet list my-net
yacd wallet export my-net bright-sun
yacd wallet remove my-net bright-sun
```

`list` shows each managed wallet's name, address, and source (the genesis
`faucet` wallet is included). `export` writes the wallet's `.skey`, `.vkey`, and
`.addr` files to `.yacd/<namespace>/<network>/wallets/<name>/` (override with
`--out`). The `faucet` wallet is reserved and operator-owned, so the CLI will not
remove it.

## See also

- [CLI reference](../reference/cli.md#wallet) — every `yacd wallet` flag and default.
- [Security](../concepts/security.md) — wallet key custody and local signing.
- [Connecting tools & tests](connecting-tools.md) — funding a wallet from a test run.
