# Changelog

## 1.0.0 (2026-06-01)


### Features

* **api:** add CardanoDBSync CRD ([#17](https://github.com/meigma/yacd/issues/17)) ([c253903](https://github.com/meigma/yacd/commit/c25390355e73080c731705cc810508aad4fe444d))
* **api:** add CardanoNetwork CRD draft ([f918623](https://github.com/meigma/yacd/commit/f918623376744ad4a8eba3f574019f887318014a))
* **cardano-testnet:** rewrite publisher as Cobra/Viper module with hexagonal layers ([#25](https://github.com/meigma/yacd/issues/25)) ([ac4ece0](https://github.com/meigma/yacd/commit/ac4ece03d7ed4cffd640d62be2190499096c5876))
* **cardano-tools:** add unified Cardano artifact utility container ([#64](https://github.com/meigma/yacd/issues/64)) ([ad46e82](https://github.com/meigma/yacd/commit/ad46e829e9588283a19edb8be2f84c6ba4d03d37))
* **cardanodbsync:** add dbsync controller runtime ([#23](https://github.com/meigma/yacd/issues/23)) ([6cfe700](https://github.com/meigma/yacd/commit/6cfe70083362e4b9438b119fe059cdac97df5b2b))
* **cardanodbsync:** add managed postgres support ([#24](https://github.com/meigma/yacd/issues/24)) ([879c0d7](https://github.com/meigma/yacd/commit/879c0d72ce0f54dd9a34e2f83cf422b0fceb4351))
* **cardanodbsync:** probe dbsync progress ([#31](https://github.com/meigma/yacd/issues/31)) ([de42f99](https://github.com/meigma/yacd/commit/de42f995ccc0226578ba7e2a158beedaf5302e24))
* **cardanodbsync:** support primary sidecar placement ([#45](https://github.com/meigma/yacd/issues/45)) ([8e77d3d](https://github.com/meigma/yacd/commit/8e77d3d045099c3b48a295b42b2176dc9f18a7fd))
* **cardanodbsync:** support public sidecar placement ([#48](https://github.com/meigma/yacd/issues/48)) ([69a87d1](https://github.com/meigma/yacd/commit/69a87d107304c1f9aec6f11f1003f4b030b96c07))
* **cardanonetwork:** add kupo chain api ([#14](https://github.com/meigma/yacd/issues/14)) ([b52e923](https://github.com/meigma/yacd/commit/b52e923069ef0e98457477ed6e4e0c35cc7be0a1))
* **cardanonetwork:** apply primary node deployment ([044d441](https://github.com/meigma/yacd/commit/044d441d65052122ef162c55806c0cbacba2c0a1))
* **cardanonetwork:** expose ogmios chain api ([#12](https://github.com/meigma/yacd/issues/12)) ([fe8b4fd](https://github.com/meigma/yacd/commit/fe8b4fd9cf6fb50bb06bb20e52901e65222a994c))
* **cardanonetwork:** publish network artifacts configmap ([#20](https://github.com/meigma/yacd/issues/20)) ([9ac60de](https://github.com/meigma/yacd/commit/9ac60de1c0503bd9f4d2994e9be91f0f1066a636))
* **cardanonetwork:** publish node sync status ([#74](https://github.com/meigma/yacd/issues/74)) ([bfadcf6](https://github.com/meigma/yacd/commit/bfadcf6ade60409dbd091a45de137d181e444d5f))
* **cardanonetwork:** publish primary node readiness ([#11](https://github.com/meigma/yacd/issues/11)) ([c415f3e](https://github.com/meigma/yacd/commit/c415f3e23702e5edd9f37b8d990cdfd7f9150566))
* **cardanonetwork:** serve network artifacts over HTTP (F0 redesign, PR-A) ([#75](https://github.com/meigma/yacd/issues/75)) ([c61e0a6](https://github.com/meigma/yacd/commit/c61e0a62a9f7cfb88e18b40a64ac919e60fe2fc8))
* **cardanonetwork:** support public network profiles ([#47](https://github.com/meigma/yacd/issues/47)) ([385718b](https://github.com/meigma/yacd/commit/385718b8e279d021ec13cb12205353e076144d97))
* **cli:** add developer environment CLI ([#13](https://github.com/meigma/yacd/issues/13)) ([8bf1b26](https://github.com/meigma/yacd/commit/8bf1b26c921ec40034827b198572ab302b0fbb63))
* **cli:** add host-access kube ports and exit-code carrier ([#59](https://github.com/meigma/yacd/issues/59)) ([02710cd](https://github.com/meigma/yacd/commit/02710cdcca0353b614670254ab03405404fe4545))
* **cli:** add topup --await on-chain confirmation ([#66](https://github.com/meigma/yacd/issues/66)) ([7a9c66c](https://github.com/meigma/yacd/commit/7a9c66c87214afbc468509941df166883769c2d9))
* **cli:** add up/down/list lifecycle verbs and CLI-driven identity ([#58](https://github.com/meigma/yacd/issues/58)) ([c7825f8](https://github.com/meigma/yacd/commit/c7825f8a949bca3d06b49e069e6403d8e44b9fa9))
* **cli:** add yacd connect supervised forwards ([#63](https://github.com/meigma/yacd/issues/63)) ([a65f379](https://github.com/meigma/yacd/commit/a65f379acaf2952512a1b053d19b32d68e0c52b4))
* **cli:** add yacd exec in-pod verb ([#62](https://github.com/meigma/yacd/issues/62)) ([45c44f8](https://github.com/meigma/yacd/commit/45c44f84525ff539a91dea7aa8e5ff854e4bb052))
* **cli:** add yacd run host-access verb ([#61](https://github.com/meigma/yacd/issues/61)) ([a94afe5](https://github.com/meigma/yacd/commit/a94afe5ac82f107b1125a741a0d4b63dc4999043))
* **cli:** add YACD_* env contract and port-forward orchestration ([#60](https://github.com/meigma/yacd/issues/60)) ([bd3159d](https://github.com/meigma/yacd/commit/bd3159d8d2a1dedd5e1a5c058315e695116eaab3))
* **cli:** make yacd exec interactive with raw mode and window resize ([#72](https://github.com/meigma/yacd/issues/72)) ([dbaa886](https://github.com/meigma/yacd/commit/dbaa88683660b7065c4b69e67ff6cda0d48565b2))
* **faucet:** add authenticated top-up service ([#15](https://github.com/meigma/yacd/issues/15)) ([14bbcc1](https://github.com/meigma/yacd/commit/14bbcc17e062f210d0be9965c7f076e74f62ee76))
* **localnet:** build CardanoNetwork plan pipeline ([#4](https://github.com/meigma/yacd/issues/4)) ([86da9a4](https://github.com/meigma/yacd/commit/86da9a4faa36d2c6d88fbd514022c6486ea2392b))
* **operator:** add cardano-tools image seam, PR-CI build, and static-musl guard ([#68](https://github.com/meigma/yacd/issues/68)) ([f11486d](https://github.com/meigma/yacd/commit/f11486d7f18df4e0ce7108c3dfbc462a74e49734))


### Bug Fixes

* **build:** re-include embedded publicnet profiles in docker context ([#55](https://github.com/meigma/yacd/issues/55)) ([0bb852d](https://github.com/meigma/yacd/commit/0bb852da79717cb2fcfd01459b0479390c51e248))
* **cardanodbsync:** adopt restored managed postgres auth secret ([#57](https://github.com/meigma/yacd/issues/57)) ([1db7371](https://github.com/meigma/yacd/commit/1db7371cc81e7c8d4014a7a04a03927dca7024cd))
* **cardanodbsync:** derive accepted identity from PVC ([#52](https://github.com/meigma/yacd/issues/52)) ([76946d1](https://github.com/meigma/yacd/commit/76946d1ea3dae1298023866af715c93d41f86d6f))
* **cardanodbsync:** preserve primary sidecar incumbent ([#50](https://github.com/meigma/yacd/issues/50)) ([5939ecb](https://github.com/meigma/yacd/commit/5939ecb2ffc1b521f963f9c048774848db4d4db1))
* **cardanodbsync:** reject accepted placement handoffs ([#46](https://github.com/meigma/yacd/issues/46)) ([5b3185a](https://github.com/meigma/yacd/commit/5b3185afa2a731522268b9072d53cad0ac52de93))
* **cardanonetwork:** derive identity status from owned state ([#51](https://github.com/meigma/yacd/issues/51)) ([855d9fa](https://github.com/meigma/yacd/commit/855d9fa29009d034464aac236d20e9c534290e89))
* **cardanonetwork:** throttle artifact recovery rollouts ([#49](https://github.com/meigma/yacd/issues/49)) ([11b6ee7](https://github.com/meigma/yacd/commit/11b6ee76929547aef17bffbf17774a35471c4c70))
* **cli:** announce topup --await polling and pin the await-address invariant ([#71](https://github.com/meigma/yacd/issues/71)) ([c8d0470](https://github.com/meigma/yacd/commit/c8d047087713dcf1b3c37393fd52072687f5fe3f))
* **cli:** bound port-forward dial so cancellation returns promptly ([#69](https://github.com/meigma/yacd/issues/69)) ([af95ca0](https://github.com/meigma/yacd/commit/af95ca0163c8d203854536293e6fce37d361c362))
* **cli:** harden review findings ([#73](https://github.com/meigma/yacd/issues/73)) ([2f28360](https://github.com/meigma/yacd/commit/2f28360cf671edeca30850205fb13ad8d8f824bb))
* **controller:** fail closed on primary pvc loss ([#56](https://github.com/meigma/yacd/issues/56)) ([a28cc40](https://github.com/meigma/yacd/commit/a28cc401ff48ed1b3801ee91e97fc48fbe339395))
* **controller:** reconcile faucet auth secret recovery ([#54](https://github.com/meigma/yacd/issues/54)) ([3754bae](https://github.com/meigma/yacd/commit/3754baedc6fd96332cb102a9e142ee1c669a0202))
* **controller:** surface rejected PVC expansion in status ([dea708e](https://github.com/meigma/yacd/commit/dea708e5828a3f0643f2ba04d70189dd66709b84))
* **dev-stack:** rebuild cardano-testnet image locally so dev stack picks up publisher changes ([#42](https://github.com/meigma/yacd/issues/42)) ([f5bbfbb](https://github.com/meigma/yacd/commit/f5bbfbb016511d8351e2fe548098f436d2d782d9))
* **dev:** preserve ko faucet entrypoint ([#16](https://github.com/meigma/yacd/issues/16)) ([7b6dc37](https://github.com/meigma/yacd/commit/7b6dc37504d6d69d9f34cda764eca21c57d60747))

## Changelog
