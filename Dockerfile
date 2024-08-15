FROM cgr.dev/chainguard/go:latest as build

WORKDIR /build

COPY go.mod go.sum ./
RUN go mod tidy

COPY . .
RUN CGO_ENABLED=0 go build -a -trimpath -ldflags="-s -w" -o /app/app .

FROM debian:latest
WORKDIR /app

RUN apt-get update && apt-get install -y yubikey-manager osslsigncode ykcs11 libengine-pkcs11-openssl

COPY --from=build /app/app /usr/local/bin/signing-server

CMD ["signing-server"]