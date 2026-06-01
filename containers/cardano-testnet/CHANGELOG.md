# Changelog

## [11.0.1-yacd.5](https://github.com/meigma/yacd/compare/cardano-testnet/v11.0.1-yacd.4...cardano-testnet/v11.0.1-yacd.5) (2026-06-01)


### ⚠ BREAKING CHANGES

* **cardano-testnet:** the cardano-testnet image no longer contains the yacd-cardano-testnet-publisher binary.

### Features

* **cardanodbsync:** probe dbsync progress ([#31](https://github.com/meigma/yacd/issues/31)) ([de42f99](https://github.com/meigma/yacd/commit/de42f995ccc0226578ba7e2a158beedaf5302e24))


### Build

* **cardano-testnet:** pin the next release to 11.0.1-yacd.5 and document versioning ([#80](https://github.com/meigma/yacd/issues/80)) ([7c13d14](https://github.com/meigma/yacd/commit/7c13d14caa6d432b60c4254a06c553c9557c7988))
* **cardano-testnet:** remove the dead artifact publisher from the tools image (F0 PR-B2) ([#79](https://github.com/meigma/yacd/issues/79)) ([22a5e8f](https://github.com/meigma/yacd/commit/22a5e8fec1920c12bf4173cf27e3e197b3299012))

## [11.0.1-yacd.4](https://github.com/meigma/yacd/compare/cardano-testnet/v11.0.1-yacd.3...cardano-testnet/v11.0.1-yacd.4) (2026-05-25)


### Features

* **cardano-testnet:** rewrite publisher as Cobra/Viper module with hexagonal layers ([#25](https://github.com/meigma/yacd/issues/25)) ([ac4ece0](https://github.com/meigma/yacd/commit/ac4ece03d7ed4cffd640d62be2190499096c5876))

## [11.0.1-yacd.3](https://github.com/meigma/yacd/compare/cardano-testnet/v11.0.1-yacd.2...cardano-testnet/v11.0.1-yacd.3) (2026-05-24)


### Bug Fixes

* **cardano-testnet:** prune stale artifact keys ([#21](https://github.com/meigma/yacd/issues/21)) ([4ab18bb](https://github.com/meigma/yacd/commit/4ab18bb06df98cf64768bfc88328c147033e6157))

## [11.0.1-yacd.2](https://github.com/meigma/yacd/compare/cardano-testnet/v11.0.1-yacd.1...cardano-testnet/v11.0.1-yacd.2) (2026-05-24)


### Build

* **cardano-testnet:** add artifact publisher ([#18](https://github.com/meigma/yacd/issues/18)) ([ad3fb1e](https://github.com/meigma/yacd/commit/ad3fb1e6a23f792ac10729444d1f6e187619fd83))

## [11.0.1-yacd.1](https://github.com/meigma/yacd/compare/cardano-testnet/v11.0.1-yacd.0...cardano-testnet/v11.0.1-yacd.1) (2026-05-21)


### Build

* **cardano-testnet:** add localnet init wrapper ([#8](https://github.com/meigma/yacd/issues/8)) ([549859c](https://github.com/meigma/yacd/commit/549859c5630ff31398a7f437f84980b64eb3097e))
* **cardano-testnet:** add tools container ([#5](https://github.com/meigma/yacd/issues/5)) ([5fc50da](https://github.com/meigma/yacd/commit/5fc50dae32308ec1adb59f058e6d80fd6d20db6b))

## 11.0.1 (2026-05-21)


### Build

* **cardano-testnet:** add tools container ([#5](https://github.com/meigma/yacd/issues/5)) ([5fc50da](https://github.com/meigma/yacd/commit/5fc50dae32308ec1adb59f058e6d80fd6d20db6b))
