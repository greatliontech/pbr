FROM alpine:3.20.1

RUN apk add --no-cache fuse=2.9.9-r5

ARG USERNAME=pbruser
ARG USER_UID=1000
ARG USER_GID=$USER_UID

RUN addgroup -S -g $USER_GID $USERNAME \
  && adduser -S -u $USER_GID -G $USERNAME $USERNAME

COPY pbr /app/pbr

# Create data directory with proper permissions
RUN mkdir -p /data && chown -R $USERNAME:$USERNAME /data

WORKDIR /app

ENTRYPOINT ["/app/pbr"]

USER $USERNAME
