FROM golang:1.25.5-bookworm AS builder

WORKDIR /build

# hadolint ignore=DL3015
RUN apt-get update && \
    apt-get install -y \
        git \
        gcc \
        unzip \
        curl \
        zlib1g-dev && \
    rm -rf /var/lib/apt/lists/*

COPY go.mod go.sum ./
RUN go mod tidy

COPY install.sh ./
COPY . .

RUN chmod +x install.sh && \
    ./install.sh -n --quiet --skip-summary && \
    CGO_ENABLED=1 go build -v -trimpath -ldflags="-w -s" -o app ./cmd/app/


# ========================= RUNTIME IMAGE =========================

FROM debian:bookworm-slim

# Base dependencies + ffmpeg
RUN apt-get update && \
    apt-get install -y \
        ffmpeg \
        curl \
        unzip \
        ca-certificates \
        zlib1g \
        nodejs \
        npm && \
    rm -rf /var/lib/apt/lists/*

# ---------------- yt-dlp ----------------
RUN curl -fL \
    https://github.com/yt-dlp/yt-dlp/releases/latest/download/yt-dlp_linux \
    -o /usr/local/bin/yt-dlp && \
    chmod 0755 /usr/local/bin/yt-dlp

# ---------------- Deno ----------------
ENV DENO_INSTALL=/usr/local/deno
RUN curl -fsSL https://deno.land/install.sh | sh && \
    mv /root/.deno $DENO_INSTALL

ENV PATH=$DENO_INSTALL/bin:$PATH

# ---------------- Bun ----------------
ENV BUN_INSTALL=/usr/local/bun
RUN curl -fsSL https://bun.sh/install | bash && \
    mv /root/.bun $BUN_INSTALL

ENV PATH=$BUN_INSTALL/bin:$PATH

# ---------------- Node.js (LTS from NodeSource) ----------------
RUN curl -fsSL https://deb.nodesource.com/setup_20.x | bash - && \
    apt-get install -y nodejs && \
    rm -rf /var/lib/apt/lists/*

# ---------------- yt-dlp EJS helpers ----------------
# This installs the JS challenge solver support
RUN npm install -g yt-dlp-ejs

# ---------------- Certificates ----------------
COPY --from=builder /etc/ssl/certs /etc/ssl/certs

# ---------------- App user ----------------
RUN useradd -r -u 10001 appuser && \
    mkdir -p /app && \
    chown -R appuser:appuser /app

WORKDIR /app

COPY --from=builder /build/app /app/app
RUN chown appuser:appuser /app/app

USER appuser

ENTRYPOINT ["/app/app"]
