# ---------- Build stage ----------
FROM golang:1.26-alpine3.24 AS build

# sqlite requires gcc and musl-dev in order to compile and use it.
RUN apk add --no-cache gcc musl-dev

WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download

COPY . .

ENV CGO_ENABLED=1
RUN go build -o /app/plumber ./cmd/plumber

# ---------- Runtime stage ----------
FROM alpine:3.24 AS runtime

RUN adduser --disabled-password --home /home/container container

USER container
ENV USER=container HOME=/home/container

WORKDIR /home/container

COPY --from=build --chown=container:container /app/plumber /app/plumber
COPY --chown=container:container ./entrypoint.sh /entrypoint.sh

CMD ["/bin/sh", "/entrypoint.sh"]