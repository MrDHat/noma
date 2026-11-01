# noma-server: one static binary on distroless, no shell.
# Build context: a directory holding a linux noma-server binary (make images).
FROM gcr.io/distroless/static-debian12:nonroot@sha256:afa5c872c891853ca7fcf1f12c3edb23f7eeef36189728842dd51042ff57f7ab
COPY noma-server /noma-server
ENTRYPOINT ["/noma-server"]
