FROM node:22-alpine AS ui
WORKDIR /src
COPY package.json package-lock.json tsconfig.json ./
COPY web ./web
RUN npm ci && npm run build

FROM golang:1.25-alpine AS server
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY cmd ./cmd
COPY internal ./internal
COPY web/embed.go ./web/embed.go
COPY --from=ui /src/web/dist ./web/dist
RUN CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o /lanplay ./cmd/lanplay

FROM scratch
COPY --from=server /etc/ssl/certs/ca-certificates.crt /etc/ssl/certs/ca-certificates.crt
COPY --from=server /lanplay /lanplay
COPY LICENSE /LICENSE
USER 65532:65532
EXPOSE 8080/tcp 50000-50100/udp
ENTRYPOINT ["/lanplay"]
CMD ["--data", "/data", "--udp-ports", "50000-50100"]
