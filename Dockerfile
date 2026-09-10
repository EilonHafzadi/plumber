# ---------- Build stage ----------
FROM golang:1.26-alpine AS build

WORKDIR /usr/cmd/plumber
COPY . .

RUN go build -o /app/plumber ./cmd/plumber

# ---------- Runtime stage ----------
FROM golang:1.26-alpine AS runtime

RUN adduser --disabled-password --home /home/container container

USER container
ENV  USER=container HOME=/home/container

WORKDIR /home/container

COPY --from=build --chown=container:container /app/plumber /app/plumber
COPY --chown=container:container ./entrypoint.sh /entrypoint.sh

CMD ["/bin/sh", "/entrypoint.sh"]