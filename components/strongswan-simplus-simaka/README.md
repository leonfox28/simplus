# Simplus strongSwan SIM AKA backend

This GPL-2.0-or-later plugin implements strongSwan's `simaka_card_t` client
interface by calling the separate root-only Simplus Agent HIL socket. It does
not accept arbitrary AT commands or APDUs. The only operation is a 3G AKA
challenge with fixed 16-byte RAND/AUTN input and typed RES/CK/IK or AUTS
output.

The same narrowly loaded HIL plugin adds `IDr=ims` to the first IKE_AUTH
request of the fixed `vowifi-ims` connection. 3GPP TS 24.302 uses this FQDN
identity to select the IMS APN; omitting IDr selects the default APN and can
produce an authenticated ePDG tunnel with no usable IMS data plane. The hook
does not modify other connections or later IKE_AUTH exchanges and leaves the
Vodafone responder identity constraint intact.

The plugin configuration is ephemeral and belongs under `/run`. It contains
the current Agent/topology fence, the keyed SIM pseudonym and the EAP identity.
The IMSI-derived EAP identity must never be written under `/etc` or `/var`,
printed on a command line, or logged. Agent responses and authentication
material are cleared from plugin-owned buffers after use.

This independently licensed component contains source only.
`scripts/dev/test-simplus-simaka-c.sh` runs the dependency-free
protocol/parser tests. The release pipeline builds it together with the
upstream `p-cscf` plugin into `simplus-strongswan-plugins.deb` against the
locked Debian source and runtime-library inputs under
`packaging/strongswan-plugins/`. Neither installation nor ordinary Go/Web
development requires a strongSwan source or build tree.
