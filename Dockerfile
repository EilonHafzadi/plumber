# ---------- Build stage ----------
FROM golang:alpine AS build

WORKDIR /usr/cmd/plumber
COPY . .

RUN go build -o /cmd/plumber ./cmd/plumber

# ---------- Runtime stage ----------
FROM golang:alpine AS runtime

RUN adduser --disabled-password --home /home/container container

USER container
ENV  USER=container HOME=/home/container

WORKDIR /home/container

COPY --from=build --chown=container:container /cmd/plumber /cmd/plumber
COPY --chown=container:container ./entrypoint.sh /entrypoint.sh

CMD ["/bin/sh", "/entrypoint.sh"]