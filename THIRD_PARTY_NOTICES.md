# Third-party and separately licensed material

The root [`LICENSE`](LICENSE) applies to Simplus material offered under the PolyForm Noncommercial License 1.0.0. It does not replace licenses attached to third-party or separately licensed files.

## Bundled material

- **Zashboard v3.6.0** — unmodified release assets from <https://github.com/Zephyruso/zashboard>, licensed under the MIT License. See [`third_party/zashboard/LICENSE`](third_party/zashboard/LICENSE).
- **Simplus strongSwan SIM AKA plugin** — separately licensed under `GPL-2.0-or-later` for strongSwan integration. See [`components/strongswan-simplus-simaka/LICENSE`](components/strongswan-simplus-simaka/LICENSE).
- **strongSwan `p-cscf` plugin** — built unmodified from the exact GPL-2.0-or-later Debian strongSwan source input recorded in [`packaging/strongswan-plugins/debian-13-amd64.lock`](packaging/strongswan-plugins/debian-13-amd64.lock). Release artifacts include the corresponding source and its license material.

Other dependencies remain subject to the licenses supplied by their respective copyright holders. Dependency versions and sources are recorded in the repository's package-manager manifests and release inputs.
