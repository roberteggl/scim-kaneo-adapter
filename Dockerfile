# syntax=docker/dockerfile:1

FROM golang:1.27-alpine AS build

WORKDIR /src

COPY go.mod go.sum* ./
RUN go mod download

COPY . .
RUN CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o /out/scim-kaneo-adapter ./cmd/scim-kaneo-adapter

FROM alpine:latest@sha256:f59abe656b692d901e184be3445e084786602108599d2a51d229c0f5a48c71bf AS runtime-base

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
