# A stand-in for a repo's own image (noma.yaml), for the e2e tests: Debian
# with git, or without it (GIT=no) to check Noma refuses such an image.
ARG DEBIAN=debian:bookworm-slim@sha256:99c0b25c46d0214d5a2091092e7908907dcaecd6a962cde0ac700acee222b6f3
FROM ${DEBIAN}
ARG GIT=yes
RUN if [ "$GIT" = yes ]; then \
      apt-get update && apt-get install -y --no-install-recommends ca-certificates git \
      && rm -rf /var/lib/apt/lists/*; \
    fi \
 && echo "noma e2e project image" > /etc/noma-e2e-project
USER 1000:1000
