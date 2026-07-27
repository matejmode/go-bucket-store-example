## Build stage
FROM docker.io/library/golang:1.26-alpine AS build
WORKDIR /src

# No third-party dependencies (stdlib only), so there's no go.sum to restore
# separately - just copy the module and build.
COPY go.mod ./
COPY . .
RUN CGO_ENABLED=0 go build -o /out/server ./cmd/server

## Final stage
# :nonroot runs the container as uid 65532 instead of root - if you mount a
# volume for STORAGE_BACKEND=disk, make sure it's writable by that uid.
FROM gcr.io/distroless/static-debian12:nonroot
COPY --from=build /out/server /server

ENV PORT=8080
ENV STORAGE_BACKEND=memory
ENV DATA_DIR=/data

VOLUME ["/data"]
EXPOSE 8080

ENTRYPOINT ["/server"]