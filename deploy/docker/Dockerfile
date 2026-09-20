# Build the Runnerly binaries and ship them on a minimal base.
#
# Targets:
#   server  -> runnerly-server, the control plane
#   agent   -> runnerly-agent plus the CLI, for a runner machine
#
# The agent image is provided for completeness, but note that running an agent
# in a container means the runner it supervises also runs there, and that the
# container needs whatever your workflows need. Most machines are better off
# with the systemd unit.

FROM golang:1.25-alpine AS build

WORKDIR /src

# Dependencies first, so a source-only change does not refetch them.
COPY go.mod go.sum ./
RUN go mod download

COPY . .

ARG VERSION=dev
ARG COMMIT=none
ARG DATE=unknown
ENV CGO_ENABLED=0

RUN go build -trimpath \
      -ldflags "-s -w \
        -X github.com/bablilayoub/runnerly/internal/version.Version=${VERSION} \
        -X github.com/bablilayoub/runnerly/internal/version.Commit=${COMMIT} \
        -X github.com/bablilayoub/runnerly/internal/version.Date=${DATE}" \
      -o /out/ ./cmd/...

FROM alpine:3.20 AS base
# ca-certificates so outbound HTTPS to GitHub works at all.
RUN apk add --no-cache ca-certificates tzdata \
 && adduser -D -H -u 10001 runnerly
USER 10001:10001

FROM base AS server
COPY --from=build /out/runnerly-server /usr/local/bin/runnerly-server
COPY --from=build /out/runnerly /usr/local/bin/runnerly
EXPOSE 8080
ENTRYPOINT ["/usr/local/bin/runnerly-server"]

FROM base AS agent
COPY --from=build /out/runnerly-agent /usr/local/bin/runnerly-agent
COPY --from=build /out/runnerly /usr/local/bin/runnerly
ENTRYPOINT ["/usr/local/bin/runnerly-agent"]
