# ---------- Build stage ----------
FROM golang:latest AS build

WORKDIR /usr/cmd/plumber
COPY . .

RUN CGO_ENABLED=0 go build -o /cmd/plumber ./cmd/plumber

# ---------- Runtime stage ----------
FROM alpine:latest AS runtime

RUN adduser --disabled-password --home /home/container container

USER container
ENV  USER=container HOME=/home/container

WORKDIR /home/container

COPY --from=build --chown=container:container /cmd/plumber /cmd/plumber
COPY --chown=container:container ./entrypoint.sh /entrypoint.sh

CMD ["/bin/sh", "/entrypoint.sh"]