# One Dockerfile for both commands; compose picks one with the CMD build arg.
FROM golang:1.26-alpine AS build
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
ARG CMD=fleetd
RUN CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o /out/app ./cmd/${CMD}

# Static binary on distroless: no shell or package manager, runs as non-root.
# Migrations, the OpenAPI spec and the demo seed are embedded in the binary.
FROM gcr.io/distroless/static-debian12:nonroot
COPY --from=build /out/app /app
ENTRYPOINT ["/app"]
