# The koko server's image, ghcr.io/cieons/koko (docs/subsystems/release.md,
# "The image"). make image builds it for linux/amd64 and linux/arm64 with
# dist/ as its context, so it holds the release's own kokod-linux-<arch>
# and koko-linux-<arch>, byte for byte, never rebuilt. It runs nothing (no
# RUN step), so building for the other architecture needs no emulation. It
# runs as uid and gid 65532 from the start, never as root, and needs no
# capability (ADR 0039).

# An empty stage, whose root is copied to make the image's directories
# with their owner and mode, since no RUN step may make them.
FROM scratch AS empty

# distroless/static's nonroot variant, whose default user is the numeric
# 65532, nonroot in its /etc/passwd, whose home is /home/nonroot; a numeric
# user satisfies Kubernetes' runAsNonRoot as it is. Dependabot bumps the
# digest.
FROM gcr.io/distroless/static-debian12:nonroot@sha256:afa5c872c891853ca7fcf1f12c3edb23f7eeef36189728842dd51042ff57f7ab

ARG TARGETARCH
ARG VERSION
ARG REVISION

LABEL org.opencontainers.image.source=https://github.com/cieons/koko \
      org.opencontainers.image.version=$VERSION \
      org.opencontainers.image.revision=$REVISION \
      org.opencontainers.image.licenses=Apache-2.0

# kokod is the koko server, which the image runs; koko is the CLI, which
# docker exec runs as the Local caller over the container's socket.
COPY --chmod=0755 kokod-linux-$TARGETARCH /usr/local/bin/kokod
COPY --chmod=0755 koko-linux-$TARGETARCH /usr/local/bin/koko
# /koko holds the data directory and the Root Key; /run/koko the Unix
# socket, in a directory only uid 65532 can write. A named volume on /koko
# starts as a copy of it; a bind mount must be 65532's already, since koko
# changes no owner.
COPY --from=empty --chown=65532:65532 --chmod=0700 / /koko
COPY --from=empty --chown=65532:65532 --chmod=0755 / /run/koko

# KOKO_IMAGE tells koko update that this koko is the image's, which a new
# image upgrades (ADR 0047), and kokod that it is upgraded by a new image,
# which koko server show names (ADR 0048).
ENV KOKO_DATA_DIR=/koko/data \
    KOKO_ROOT_KEY_FILE=/koko/root.key \
    KOKO_IMAGE=ghcr.io/cieons/koko
# A container without a mount keeps /koko in an anonymous volume, which
# outlives docker rm, rather than in its own layer.
VOLUME /koko
EXPOSE 7822

ENTRYPOINT ["kokod"]
CMD ["run"]
