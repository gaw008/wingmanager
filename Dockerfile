FROM --platform=$BUILDPLATFORM golang:1.27.3 AS build
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY cmd/ ./cmd/
COPY internal/ ./internal/
ARG TARGETOS
ARG TARGETARCH
ARG VERSION=dev
RUN CGO_ENABLED=0 GOOS=$TARGETOS GOARCH=$TARGETARCH go build -trimpath -ldflags="-s -w -X main.version=$VERSION" -o /wingmanager ./cmd/wingmanager
RUN mkdir /data

FROM gcr.io/distroless/static-debian12:nonroot
COPY --from=build /wingmanager /wingmanager
COPY --from=build --chown=nonroot:nonroot /data /data
USER nonroot:nonroot
VOLUME ["/data"]
EXPOSE 8080
ENTRYPOINT ["/wingmanager"]
CMD ["serve", "--addr", "0.0.0.0:8080", "--data-dir", "/data"]
