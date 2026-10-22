# syntax=docker/dockerfile:1

FROM golang:1.27-alpine AS build

WORKDIR /src

COPY go.mod go.sum* ./
RUN go mod download

COPY . .
RUN CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o /out/scim-kaneo-adapter ./cmd/scim-kaneo-adapter

FROM alpine:latest@sha256:71e9b7bae160ba167fa17414401b94cbcdaf3c32feeab9298bd794a2a0614c35 AS runtime-base

RUN apk add --no-cache ca-certificates tzdata \
  && adduser \
    --disabled-password \
    --gecos "" \
    --home "/nonexistent" \
    --shell "/sbin/nologin" \
    --no-create-home \
    --uid "10001" \
    "appuser" \
  && mkdir /data \
  && chown 10001:10001 /data

FROM scratch

COPY --from=runtime-base /usr/share/zoneinfo /usr/share/zoneinfo
COPY --from=runtime-base /etc/ssl/certs/ca-certificates.crt /etc/ssl/certs/
COPY --from=runtime-base /etc/passwd /etc/passwd
COPY --from=runtime-base /etc/group /etc/group
COPY --from=runtime-base --chown=10001:10001 /data /data

ENV TZ=Europe/Berlin

USER appuser:appuser

EXPOSE 8080

COPY --from=build /out/scim-kaneo-adapter /scim-kaneo-adapter

ENTRYPOINT ["/scim-kaneo-adapter"]
