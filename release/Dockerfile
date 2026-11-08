# The koko server's image, ghcr.io/cieons/koko (docs/subsystems/release.md,
# "The image"). make image builds it for linux/amd64 and linux/arm64 with
# dist/ as its context, so it holds the release's own koko-linux-<arch>,
# byte for byte, never rebuilt. It runs nothing (no RUN step), so building
# for the other architecture needs no emulation. koko server run reads
# KOKO_RUN_AS, which drops root, only once #96 lands; no release is tagged
# before the container check (#97) gates it.

# An empty stage, whose root is copied to make the image's directories
# with their owner and mode, since no RUN step may make them.
FROM scratch AS empty

# distroless/static's variant whose default user is root: the container
# starts as root only to give /koko to uid 65532, then koko server run drops
# to it (KOKO_RUN_AS). Its /etc/passwd names uid 65532 nonroot. Dependabot
# bumps the digest.
FROM gcr.io/distroless/static-debian12:latest@sha256:d75cdd72874d4790092fcb1b058493ecf6bb5bf2b2b897045b00ff01d91843f2

ARG TARGETARCH
ARG VERSION
ARG REVISION

LABEL org.opencontainers.image.source=https://github.com/cieons/koko \
      org.opencontainers.image.version=$VERSION \
      org.opencontainers.image.revision=$REVISION \
      org.opencontainers.image.licenses=Apache-2.0

COPY --chmod=0755 koko-linux-$TARGETARCH /usr/local/bin/koko
# /koko holds the data directory and the Root Key; /run/koko the Unix
# socket, in a directory only uid 65532 can write. koko server run makes and
# fixes both again as root, so a mount over either works.
COPY --from=empty --chown=65532:65532 --chmod=0700 / /koko
COPY --from=empty --chown=65532:65532 --chmod=0755 / /run/koko

ENV KOKO_DATA_DIR=/koko/data \
    KOKO_ROOT_KEY_FILE=/koko/root.key \
    KOKO_RUN_AS=65532:65532
# A container without a mount keeps /koko in an anonymous volume, which
# outlives docker rm, rather than in its own layer.
VOLUME /koko
EXPOSE 7822

ENTRYPOINT ["koko"]
CMD ["server", "run"]
