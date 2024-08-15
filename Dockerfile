FROM cgr.dev/chainguard/go:latest as build

WORKDIR /build

COPY go.mod go.sum ./
RUN go mod tidy

COPY . .
RUN CGO_ENABLED=0 go build -a -trimpath -ldflags="-s -w" -o /app/app .

FROM bitnami/minideb:latest
WORKDIR /app

RUN install_packages wget apt-transport-https gnupg ca-certificates
RUN wget -qO - https://packages.adoptium.net/artifactory/api/gpg/key/public | gpg --dearmor | tee /etc/apt/trusted.gpg.d/adoptium.gpg > /dev/null
RUN echo "deb https://packages.adoptium.net/artifactory/deb $(awk -F= '/^VERSION_CODENAME/{print$2}' /etc/os-release) main" | tee /etc/apt/sources.list.d/adoptium.list
RUN apt-get update && install_packages temurin-21-jre

# download jsigner to /usr/local/bin
RUN wget -O /tmp/jsigner.deb https://github.com/ebourg/jsign/releases/download/6.0/jsign_6.0_all.deb && \
    dpkg -i /tmp/jsigner.deb && \
    rm /tmp/jsigner.deb

COPY --from=build /app/app /usr/local/bin/signing-server

CMD service pcscd start && /usr/local/bin/signing-server