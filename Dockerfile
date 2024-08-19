FROM cgr.dev/chainguard/go:latest AS build

WORKDIR /build
COPY go.mod go.sum ./
RUN go mod download

COPY . .
RUN CGO_ENABLED=0 go build -a -buildvcs=false -trimpath -ldflags="-s -w -buildid=" -o /app/app .

FROM bitnami/minideb:latest AS server
WORKDIR /app

RUN apt-get update && apt-get install -y --no-install-recommends \
    wget \
    apt-transport-https \
    gnupg \
    ca-certificates \
    ykcs11 \
    libengine-pkcs11-openssl \
    yubikey-manager \
    && rm -rf /var/lib/apt/lists/*

RUN wget -qO - https://packages.adoptium.net/artifactory/api/gpg/key/public | gpg --dearmor > /etc/apt/trusted.gpg.d/adoptium.gpg \
    && echo "deb https://packages.adoptium.net/artifactory/deb $(awk -F= '/^VERSION_CODENAME/{print$2}' /etc/os-release) main" > /etc/apt/sources.list.d/adoptium.list \
    && apt-get update && apt-get install -y --no-install-recommends temurin-21-jre \
    && rm -rf /var/lib/apt/lists/*

RUN wget -O /tmp/jsigner.deb https://github.com/ebourg/jsign/releases/download/6.0/jsign_6.0_all.deb \
    && dpkg -i /tmp/jsigner.deb \
    && rm /tmp/jsigner.deb

COPY --from=build /app/app /usr/local/bin/signing-server

CMD ["sh", "-c", "service pcscd start && /usr/local/bin/signing-server"]