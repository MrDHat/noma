# Workspace image: Debian (glibc, so prebuilt tools and native modules work),
# git, Claude Code and noma-agent as PID 1.
# Build context: linux noma-agent and fakeclaude binaries. The CA that
# signs noma-server's agent endpoint comes at run time, from a ConfigMap.
# CLAUDE=real installs the pinned Claude Code; CLAUDE=fake (CI) uses fakeclaude.
ARG CLAUDE=real
ARG DEBIAN=debian:bookworm-slim@sha256:99c0b25c46d0214d5a2091092e7908907dcaecd6a962cde0ac700acee222b6f3

FROM ${DEBIAN} AS download
ARG TARGETARCH
ARG CLAUDE_VERSION=2.1.316
ARG CLAUDE_SHA256_arm64=55cdb8e162d190432696ea54010e0fca2e18d06fa7a7127fd563b9ffd4f22624
ARG CLAUDE_SHA256_amd64=fd13c70000fec3f2116dfbcbb6121210b4a29328bf0e46210a59dd594a67d824
RUN apt-get update && apt-get install -y --no-install-recommends ca-certificates curl
RUN platform="linux-$([ "$TARGETARCH" = amd64 ] && echo x64 || echo arm64)" \
 && sum="$([ "$TARGETARCH" = amd64 ] && echo "$CLAUDE_SHA256_amd64" || echo "$CLAUDE_SHA256_arm64")" \
 && curl -fsSLo /claude "https://downloads.claude.ai/claude-code-releases/${CLAUDE_VERSION}/${platform}/claude" \
 && echo "${sum}  /claude" | sha256sum -c - \
 && chmod 0755 /claude

FROM ${DEBIAN} AS base
RUN apt-get update \
 && apt-get install -y --no-install-recommends ca-certificates git \
 && rm -rf /var/lib/apt/lists/* \
 && mkdir -m 0755 /etc/noma

FROM base AS claude-real
COPY --from=download /claude /usr/local/bin/claude

FROM base AS claude-fake
COPY --chmod=0755 fakeclaude /usr/local/bin/claude

FROM claude-${CLAUDE}
ENV DISABLE_AUTOUPDATER=1
COPY --chmod=0755 noma-agent /usr/local/bin/noma-agent
USER 1000:1000
ENTRYPOINT ["/usr/local/bin/noma-agent"]
