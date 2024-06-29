FROM alpine:latest

RUN apk add --update fuse

ARG USERNAME=pbruser
ARG USER_UID=1000
ARG USER_GID=$USER_UID

RUN addgroup -S -g $USER_GID $USERNAME \
  && adduser -S -u $USER_GID -G $USERNAME $USERNAME

COPY pbr /app/pbr

WORKDIR /app

ENTRYPOINT ["/app/pbr"]

USER $USERNAME
