# Simplus strongSwan plugin package

This directory defines the independently built
`simplus-strongswan-plugins` Debian package. It contains the GPL-licensed
Simplus SIM AKA bridge and strongSwan's upstream `p-cscf` plugin. The main
application remains controlled through VICI and Unix sockets and does not
link to strongSwan.

`debian-13-amd64.lock` records every source and runtime-ABI input by HTTPS
URL and SHA-256. `build-deb.sh` downloads those inputs into the ignored
`.dev/cache`, extracts the runtime libraries into a temporary sysroot,
configures the matching source tree, builds both plugins, and emits:

- an architecture-specific `.deb`;
- a corresponding-source archive containing all locked inputs;
- a checksummed manifest consumed by the release installer.

The build runs as an ordinary user and never installs packages or writes to
system paths. It currently supports Debian 13 on amd64 only; another target
requires a separately reviewed lock file and native CI evidence.

```bash
make build-strongswan-plugins-deb
make test-strongswan-plugins-package
```

The package is built from an exact Debian revision but permits security
updates within the same upstream `6.0.1` ABI series. A new Debian revision
must still update this lock and pass the package compatibility job
before being advertised as a reviewed release input.
