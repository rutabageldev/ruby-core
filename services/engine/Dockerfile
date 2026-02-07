# Build stage
FROM golang:1.25.6 AS build

WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download

COPY . .
ARG VERSION=dev
ARG COMMIT_SHA=unknown
RUN CGO_ENABLED=0 go build -ldflags="-s -w -X main.version=${VERSION} -X main.commitSHA=${COMMIT_SHA}" -o /engine ./services/engine

# Runtime stage
FROM gcr.io/distroless/static-debian12:nonroot

COPY --from=build /engine /engine

ENTRYPOINT ["/engine"]
