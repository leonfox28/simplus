# Mihomo container seed

Simplus production `simplus-netd` images contain the unmodified official
Mihomo `linux-amd64-compatible` release recorded in [`VERSION`](VERSION) and
[`SOURCE`](SOURCE). The binary is downloaded only by the image build, checked
against both the compressed and expanded SHA-256 values hard-coded in the
Dockerfile, and copied into a new instance by `data-init`.

The binary is licensed under GPL-3.0. The image includes the GPL-3.0 license at
`/usr/share/doc/simplus/mihomo-LICENSE`. Tagged Simplus releases attach the
corresponding, checksum-verified upstream source archive; the upstream tag and
source archive digest are recorded in [`SOURCE`](SOURCE).

No Mihomo binary or runtime configuration is stored in this repository.
