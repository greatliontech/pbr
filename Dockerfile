FROM archlinux:latest

RUN pacman -Syu --noconfirm \
  && pacman -S --noconfirm gpgme device-mapper shadow fuse-overlayfs ca-certificates \
  && pacman -Scc --noconfirm

ARG USERNAME=pbruser
ARG USER_UID=1000
ARG USER_GID=$USER_UID

RUN groupadd --gid $USER_GID $USERNAME \
  && useradd --uid $USER_UID --gid $USER_GID -m $USERNAME

COPY pbr /app/pbr

WORKDIR /app

ENTRYPOINT ["/app/pbr"]

USER $USERNAME
