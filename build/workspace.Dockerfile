# Workspace image: Debian (glibc, so prebuilt tools and native modules work),
# git and noma-agent as PID 1. Claude Code is added in M2.6.
# Build context: a directory with a linux noma-agent binary and the dev CA.
FROM debian:bookworm-slim@sha256:99c0b25c46d0214d5a2091092e7908907dcaecd6a962cde0ac700acee222b6f3
RUN apt-get update \
 && apt-get install -y --no-install-recommends ca-certificates git \
 && rm -rf /var/lib/apt/lists/* \
 && mkdir -m 0755 /etc/noma
COPY --chmod=0644 ca.crt /etc/noma/ca.crt
COPY --chmod=0755 noma-agent /usr/local/bin/noma-agent
USER 1000:1000
ENTRYPOINT ["/usr/local/bin/noma-agent"]
