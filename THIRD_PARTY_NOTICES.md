# Third-party and separately licensed material

The root [`LICENSE`](LICENSE) applies to Simplus material offered under the PolyForm Noncommercial License 1.0.0. It does not replace licenses attached to third-party or separately licensed files.

## Bundled material

- **Zashboard v3.6.0** — unmodified release assets from <https://github.com/Zephyruso/zashboard>, licensed under the MIT License. See [`third_party/zashboard/LICENSE`](third_party/zashboard/LICENSE).
- **Mihomo v1.19.29** — the production netd image contains the unmodified official `linux-amd64-compatible` release, licensed under GPL-3.0. The image includes the license and tagged Simplus releases attach the checksum-verified corresponding upstream source. Exact inputs are recorded in [`third_party/mihomo/SOURCE`](third_party/mihomo/SOURCE).
- **Simplus strongSwan SIM AKA plugin** — separately licensed under `GPL-2.0-or-later` for strongSwan integration. See [`components/strongswan-simplus-simaka/LICENSE`](components/strongswan-simplus-simaka/LICENSE).
- **strongSwan `p-cscf` plugin** — built unmodified from the exact GPL-2.0-or-later Debian strongSwan source input recorded in [`packaging/strongswan-plugins/debian-13-amd64.lock`](packaging/strongswan-plugins/debian-13-amd64.lock). Release artifacts include the corresponding source and its license material.

Other dependencies remain subject to the licenses supplied by their respective copyright holders. Dependency versions and sources are recorded in the repository's package-manager manifests and release inputs.
