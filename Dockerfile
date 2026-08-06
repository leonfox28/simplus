# syntax=docker/dockerfile:1.7

ARG GO_IMAGE=golang:1.26.5-trixie@sha256:87ffdb09b6a2e29ff910748b745395e8a0299aa80b7c0551cdca9b55e3fd2b3e
ARG NODE_IMAGE=node:24.18.0-trixie-slim@sha256:ae91dcc111a68c9d2d81ff2a17bda61be126426176fde6fe7d08ab13b7f50573
ARG DEBIAN_IMAGE=debian:13-slim@sha256:3a39a0592364683e6bab97937b72cad5a8fa6dcbbee90edb3bb48c7f8e94f258

FROM ${GO_IMAGE} AS go-build
WORKDIR /src
COPY go.mod go.sum ./
RUN --mount=type=cache,target=/go/pkg/mod go mod download
COPY cmd ./cmd
COPY internal ./internal
ARG VERSION=dev
ARG COMMIT=unknown
RUN printf '%s' "$VERSION" | grep -Eq '^[A-Za-z0-9._+-]+$' \
    && printf '%s' "$COMMIT" | grep -Eq '^(unknown|[A-Fa-f0-9]+)$' \
    && install -d -m 0755 /out \
    && for command in simplusd simplus-agent simplus-netd simplusctl; do \
         CGO_ENABLED=0 go build -buildvcs=false -trimpath \
           -ldflags "-s -w -X github.com/leonfox28/simplus/internal/buildinfo.Version=$VERSION -X github.com/leonfox28/simplus/internal/buildinfo.Commit=$COMMIT" \
           -o "/out/$command" "./cmd/$command"; \
       done

FROM ${NODE_IMAGE} AS web-build
WORKDIR /src
COPY package.json pnpm-lock.yaml pnpm-workspace.yaml ./
COPY web/package.json ./web/package.json
RUN corepack enable \
    && corepack prepare pnpm@11.18.0 --activate \
    && corepack pnpm install --frozen-lockfile
COPY api ./api
COPY web ./web
RUN corepack pnpm --dir web build

FROM ${DEBIAN_IMAGE} AS strongswan-plugin-build
RUN apt-get update \
    && apt-get install -y --no-install-recommends \
       bash binutils build-essential bzip2 ca-certificates curl dpkg-dev git \
       make patch sed tar xz-utils \
    && rm -rf /var/lib/apt/lists/*
WORKDIR /src
COPY components/strongswan-simplus-simaka ./components/strongswan-simplus-simaka
COPY packaging/strongswan-plugins ./packaging/strongswan-plugins
COPY scripts/dev/build-simplus-simaka-plugin.sh \
     scripts/dev/build-strongswan-p-cscf-plugin.sh \
     scripts/dev/test-simplus-simaka-c.sh \
     scripts/dev/test-strongswan-plugins-package.sh ./scripts/dev/
ARG SIMPLUS_DEB_VERSION=0.0.0+container1-1
ARG SOURCE_DATE_EPOCH=0
RUN packaging/strongswan-plugins/build-deb.sh /out \
    && scripts/dev/test-strongswan-plugins-package.sh /out

FROM ${DEBIAN_IMAGE} AS mihomo-fetch
RUN apt-get update \
    && apt-get install -y --no-install-recommends ca-certificates curl gzip \
    && rm -rf /var/lib/apt/lists/*
ENV MIHOMO_VERSION=v1.19.29 \
    MIHOMO_ARCHIVE_SHA256=5612e698e96c8b8ad15abc4c0a4f098eba9234354b4f248cb97f2528e215b094 \
    MIHOMO_BINARY_SHA256=bd2a08ae155b7dffc12a1bdf610ff5f17c45058414a1d2c562e28eb9309abff6
WORKDIR /out
RUN archive=/tmp/mihomo.gz \
    && curl -fsSL --retry 3 --proto '=https' --tlsv1.2 \
       "https://github.com/MetaCubeX/mihomo/releases/download/${MIHOMO_VERSION}/mihomo-linux-amd64-compatible-${MIHOMO_VERSION}.gz" \
       -o "$archive" \
    && printf '%s  %s\n' "$MIHOMO_ARCHIVE_SHA256" "$archive" | sha256sum -c - \
    && install -d -m 0755 "$MIHOMO_VERSION" \
    && gzip -cd "$archive" >"$MIHOMO_VERSION/mihomo" \
    && printf '%s  %s\n' "$MIHOMO_BINARY_SHA256" "$MIHOMO_VERSION/mihomo" | sha256sum -c - \
    && chmod 0755 "$MIHOMO_VERSION/mihomo" \
    && "$MIHOMO_VERSION/mihomo" -v >/tmp/mihomo-version \
    && grep -F "$MIHOMO_VERSION" /tmp/mihomo-version \
    && printf '%s\n' "$MIHOMO_ARCHIVE_SHA256" >ARCHIVE_SHA256 \
    && printf '%s\n' "$MIHOMO_BINARY_SHA256" >BINARY_SHA256
COPY third_party/mihomo/SOURCE third_party/mihomo/README.md third_party/mihomo/VERSION /out/
RUN test "$(cat VERSION)" = "$MIHOMO_VERSION" \
    && grep -F "binary_archive_sha256=$MIHOMO_ARCHIVE_SHA256" SOURCE \
    && grep -F "binary_expanded_sha256=$MIHOMO_BINARY_SHA256" SOURCE

FROM ${DEBIAN_IMAGE} AS runtime-base
ARG VERSION=dev
ARG COMMIT=unknown
LABEL org.opencontainers.image.title="Simplus" \
      org.opencontainers.image.source="https://github.com/leonfox28/simplus" \
      org.opencontainers.image.version="$VERSION" \
      org.opencontainers.image.revision="$COMMIT" \
      org.opencontainers.image.licenses="LicenseRef-PolyForm-Noncommercial-1.0.0"
RUN apt-get update \
    && apt-get install -y --no-install-recommends ca-certificates passwd \
    && rm -rf /var/lib/apt/lists/* \
    && groupadd --gid 10001 simplus \
    && useradd --uid 10001 --gid 10001 --home-dir /nonexistent --shell /usr/sbin/nologin simplus \
    && groupadd --gid 10002 simplus-agent \
    && useradd --uid 10002 --gid 10002 --groups 10001 --home-dir /nonexistent --shell /usr/sbin/nologin simplus-agent \
    && install -d -o root -g root -m 0755 /usr/share/doc/simplus
COPY LICENSE THIRD_PARTY_NOTICES.md /usr/share/doc/simplus/
RUN chmod 0644 /usr/share/doc/simplus/LICENSE /usr/share/doc/simplus/THIRD_PARTY_NOTICES.md
STOPSIGNAL SIGTERM

FROM runtime-base AS control
COPY --from=go-build /out/simplusd /out/simplusctl /usr/local/bin/
COPY --from=web-build --chown=10001:10001 /src/web/dist/ /usr/share/simplus/web/
RUN find /usr/share/simplus/web -type d -exec chmod 0755 {} + \
    && find /usr/share/simplus/web -type f -exec chmod 0644 {} +
USER 10001:10001
EXPOSE 8080
ENTRYPOINT ["/usr/local/bin/simplusd"]

FROM runtime-base AS agent
RUN apt-get update \
    && apt-get install -y --no-install-recommends util-linux \
    && rm -rf /var/lib/apt/lists/* \
    && install -d -o 10002 -g 10001 -m 0750 /run/simplus-agent \
    && install -d -o 10002 -g 10002 -m 0700 /var/lib/simplus-agent
COPY --from=go-build /out/simplus-agent /out/simplusctl /usr/local/bin/
COPY containers/agent-entrypoint.sh /usr/local/libexec/simplus/agent-entrypoint
RUN chmod 0755 /usr/local/libexec/simplus/agent-entrypoint
USER 0:0
ENTRYPOINT ["/usr/local/libexec/simplus/agent-entrypoint"]

FROM runtime-base AS netd
COPY --from=strongswan-plugin-build /out/ /tmp/simplus-strongswan-plugins/
RUN apt-get update \
    && apt-get install -y --no-install-recommends \
       charon-systemd iproute2 libcharon-extra-plugins \
       libstrongswan-extra-plugins nftables strongswan-swanctl \
    && dpkg --install /tmp/simplus-strongswan-plugins/*.deb \
    && rm -rf /tmp/simplus-strongswan-plugins /var/lib/apt/lists/* \
    && test -x /usr/sbin/charon-systemd \
    && test -x /usr/sbin/ip \
    && test -x /usr/sbin/nft \
    && test -f /usr/lib/ipsec/plugins/libstrongswan-eap-aka.so \
    && test -f /usr/lib/ipsec/plugins/libstrongswan-simplus-simaka.so \
    && test -f /usr/lib/ipsec/plugins/libstrongswan-p-cscf.so \
    && install -m 0644 /usr/share/common-licenses/GPL-3 /usr/share/doc/simplus/mihomo-LICENSE \
    && install -d -o root -g 10001 -m 0750 /run/simplus-netd \
    && install -d -o root -g root -m 0755 /usr/share/simplus/zashboard
COPY --from=go-build /out/simplus-netd /out/simplusctl /usr/local/bin/
COPY --from=mihomo-fetch /out/ /usr/share/simplus/mihomo/
COPY third_party/zashboard/dist/ /usr/share/simplus/zashboard/
COPY third_party/zashboard/LICENSE third_party/zashboard/VERSION third_party/zashboard/SOURCE /usr/share/simplus/zashboard/
COPY containers/data-init.sh /usr/local/libexec/simplus/data-init
COPY containers/netd-entrypoint.sh /usr/local/libexec/simplus/netd-entrypoint
COPY containers/netd-preflight.sh /usr/local/libexec/simplus/netd-preflight
RUN find /usr/share/simplus/zashboard -type d -exec chmod 0755 {} + \
    && find /usr/share/simplus/zashboard -type f -exec chmod 0644 {} + \
    && test -x /usr/share/simplus/mihomo/v1.19.29/mihomo \
    && chmod 0755 /usr/local/libexec/simplus/data-init \
                  /usr/local/libexec/simplus/netd-entrypoint \
                  /usr/local/libexec/simplus/netd-preflight
USER 0:0
EXPOSE 19090
ENTRYPOINT ["/usr/local/libexec/simplus/netd-entrypoint"]
