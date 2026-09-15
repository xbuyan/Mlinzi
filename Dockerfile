# Build stage
FROM golang:1.22-bookworm AS build
WORKDIR /src
COPY go.mod go.sum* ./
RUN go mod download
COPY . .
# CGO disabled for a fully static binary — no libc dependency in the final
# image, and no repeat of the "works on my machine" toolchain-lookup bug
# from the CLI's first version (see docs/AI_USAGE.md).
RUN CGO_ENABLED=0 GOOS=linux go build -o /out/mlinziweb ./cmd/mlinziweb

# Runtime stage — distroless, no shell, minimal attack surface
FROM gcr.io/distroless/static-debian12
COPY --from=build /out/mlinziweb /mlinziweb
EXPOSE 8080
ENTRYPOINT ["/mlinziweb"]
