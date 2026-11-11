# Noma's own development image: what a workspace on the noma repo needs to
# build, test and lint Noma (decision M3-18). The repo's noma.yaml names it;
# Noma injects its agent and Claude at start (M3-17), so this image only
# needs glibc, git and the toolchain. The e2e tests and the TLA+ checks need
# minikube and Java, so they stay on the laptop and in CI.
ARG DEBIAN=debian:bookworm-slim@sha256:99c0b25c46d0214d5a2091092e7908907dcaecd6a962cde0ac700acee222b6f3

FROM ${DEBIAN} AS download
ARG TARGETARCH
# Go matches go.mod; golangci-lint matches CI.
ARG GO_VERSION=1.26.11
ARG GO_SHA256_amd64=9849f4e04ee35303714e34fbc3ac8ab44ea3e448a95aa182f91e3b9b79c710ec
ARG GO_SHA256_arm64=6c2f29b458b5add0835f18b776f69c8881a6d88428f1484a6da55755e620757e
ARG LINT_VERSION=2.15.0
ARG LINT_SHA256_amd64=9b7fe6a6bf6334b98decf8d86b11415ea7889afdabdd1959fdce23af2a9ebcea
ARG LINT_SHA256_arm64=b4d8f17068b35ee6060cc2f69a16fb48740d706fbba81661c318649c114a6998
ARG GH_VERSION=2.103.3
ARG GH_SHA256_amd64=4c806978ffce1cec70853302a56d70ac6da4f19bae7adca4e1a8ab00633dd91d
ARG GH_SHA256_arm64=ee32df2323c8847f5fac6cb6b4b69ca75828e9329a8f22f677021f7b3306ce63
RUN apt-get update && apt-get install -y --no-install-recommends ca-certificates curl
# fetch URL SHA256 downloads URL and checks it.
RUN cat >/usr/local/bin/fetch <<'SH' && chmod +x /usr/local/bin/fetch
#!/bin/sh
set -eu
curl -fsSLo /tmp/dl "$1" && echo "$2  /tmp/dl" | sha256sum -c - >/dev/null && cat /tmp/dl && rm /tmp/dl
SH
RUN eval "go=\$GO_SHA256_${TARGETARCH} lint=\$LINT_SHA256_${TARGETARCH} gh=\$GH_SHA256_${TARGETARCH}" \
 && mkdir -p /out/bin \
 && fetch "https://go.dev/dl/go${GO_VERSION}.linux-${TARGETARCH}.tar.gz" "$go" | tar -C /out -xz \
 && fetch "https://github.com/golangci/golangci-lint/releases/download/v${LINT_VERSION}/golangci-lint-${LINT_VERSION}-linux-${TARGETARCH}.tar.gz" "$lint" \
    | tar -C /out/bin -xz --strip-components=1 "golangci-lint-${LINT_VERSION}-linux-${TARGETARCH}/golangci-lint" \
 && fetch "https://github.com/cli/cli/releases/download/v${GH_VERSION}/gh_${GH_VERSION}_linux_${TARGETARCH}.tar.gz" "$gh" \
    | tar -C /out/bin -xz --strip-components=2 "gh_${GH_VERSION}_linux_${TARGETARCH}/bin/gh"

FROM ${DEBIAN}
RUN apt-get update \
 && apt-get install -y --no-install-recommends \
      ca-certificates git make gcc libc6-dev curl less openssh-client ripgrep jq \
 && rm -rf /var/lib/apt/lists/*
COPY --from=download /out/go /usr/local/go
COPY --from=download /out/bin/ /usr/local/bin/
ENV PATH=/usr/local/go/bin:$PATH
# Workspaces run as user 1000 with HOME on the workspace volume, so Go's
# module and build caches persist there.
USER 1000:1000
