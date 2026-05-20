# Technical Notes

- YACD is intended as a Kubernetes-native Cardano development environment
  manager for builders, not validators or stake pool operators. The first
  prototype should stay local-first and Kind/Tilt-friendly.
- The primary CRD should represent a Cardano environment/network rather than a
  single node. The first runtime can be one primary `cardano-node` StatefulSet
  with Ogmios as a default sidecar and ClusterIP chain API.
- Supporting services should be separate CRDs/controllers. Network-only
  services can run as independent workloads; heavy IPC services such as db-sync
  should prefer a dedicated follower-node Pod so they do not mutate or restart
  the primary node.
- db-sync is the first supporting-service priority. Yaci Store is a later
  optional Blockfrost-like/indexer candidate after the supporting-service model
  is proven.
- The faucet/topup path should stay narrow and use Ogmios for chain
  interaction. Avoid turning it into a general wallet platform.
- The companion CLI should compile one developer-facing config into Kubernetes
  CRDs and own imperative operations such as topup, wait, status, and connection
  info.
- Root `DESIGN.md` captures the current high-level architecture; `.journal/PLAN.md`
  captures the rough component sequence for the initial prototype.
