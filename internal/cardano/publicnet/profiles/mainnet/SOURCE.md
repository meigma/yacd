Mainnet profile assets in this directory were copied from:

https://book.play.dev.cardano.org/environments/mainnet/

Mithril verification keys were copied from the release-mainnet network
configuration:

https://raw.githubusercontent.com/input-output-hk/mithril/main/mithril-infra/configuration/release-mainnet/genesis.vkey
https://raw.githubusercontent.com/input-output-hk/mithril/main/mithril-infra/configuration/release-mainnet/ancillary.vkey

The planner embeds these files so the controller can render a deterministic
mainnet public-network workload without reaching the network at reconcile time.
