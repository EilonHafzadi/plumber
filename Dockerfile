# ---------- Build stage ----------
FROM alpine:latest AS build

RUN apk add --no-cache \
    build-base \
    go

COPY . /usr/cmd/plumber

WORKDIR /usr/cmd/plumber

RUN go build ./cmd/plumber

# ---------- Runtime stage ----------
FROM alpine:latest AS runtime

RUN adduser --disabled-password --home /home/container container

USER container
ENV  USER=container HOME=/home/container

WORKDIR /home/container

COPY --from=build /usr/cmd/plumber/plumber /home/container/plumber
COPY ./entrypoint.sh /entrypoint.sh

ENV CONFIG_PATH=.
CMD ["/bin/sh", "/entrypoint.sh"]