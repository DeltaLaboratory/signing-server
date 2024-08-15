FROM cgr.dev/chainguard/go:latest as build

WORKDIR /build

COPY go.mod go.sum ./
RUN go mod tidy

COPY . .
RUN CGO_ENABLED=0 go build -a -trimpath -ldflags="-s -w" -o /app/app .

FROM debian:latest
WORKDIR /app

COPY --from=build /app/app /usr/local/bin/signing-server

RUN install_packages yubikey-manager osslsigncode ykcs11 libengine-pkcs11-openssl

CMD ["signing-server"]