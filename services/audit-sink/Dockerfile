# Build stage
FROM golang:1.25.6 AS build

WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download

COPY . .
ARG VERSION=dev
ARG COMMIT_SHA=unknown
RUN CGO_ENABLED=0 go build -ldflags="-s -w -X main.version=${VERSION} -X main.commitSHA=${COMMIT_SHA}" -o /audit-sink ./services/audit-sink

# Runtime stage
# Uses the root (non-nonroot) distroless variant because the audit-sink writes to
# a host-mounted volume (/data/audit). The volume directory is owned by root on
# initial creation by Docker, and changing ownership would require an init container.
# Running as root in the container is an accepted trade-off for a personal-project
# single-node deployment where the container has no network exposure beyond NATS.
FROM gcr.io/distroless/static-debian12:latest@sha256:20bc6c0bc4d625a22a8fde3e55f6515709b32055ef8fb9cfbddaa06d1760f838

COPY --from=build /audit-sink /audit-sink

ENTRYPOINT ["/audit-sink"]
