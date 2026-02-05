FROM golang:1.25.5-bookworm AS builder

WORKDIR /build

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


# ======================= RUNTIME =======================

FROM debian:bookworm-slim

# Base packages(VERY IMPORTANT)
RUN apt-get update && \
    apt-get install -y \
        ffmpeg \
        curl \
        unzip \
        ca-certificates \
        nodejs \
        npm \
        zlib1g && \
    update-ca-certificates && \
    rm -rf /var/lib/apt/lists/*


#########################################################
# Networking fixes (Railway / weird DNS environments)
#########################################################

ENV GODEBUG=netdns=go+v4
ENV NTG_CALLS_IPV4_ONLY=1


#########################################################
# yt-dlp NIGHTLY (DO NOT USE STABLE)
#########################################################

RUN curl -L https://github.com/yt-dlp/yt-dlp-nightly-builds/releases/latest/download/yt-dlp \
    -o /usr/local/bin/yt-dlp && \
    chmod 0755 /usr/local/bin/yt-dlp


#########################################################
# GLOBAL DENO INSTALL (NOT /root)
#########################################################

RUN curl -fsSL https://deno.land/install.sh | DENO_INSTALL=/usr/local/deno sh && \
    chmod -R 755 /usr/local/deno

ENV PATH="/usr/local/deno/bin:${PATH}"


#########################################################
# GLOBAL BUN INSTALL (NOT /root)
#########################################################

RUN curl -fsSL https://bun.sh/install | bash && \
    mv /root/.bun /usr/local/bun && \
    chmod -R 755 /usr/local/bun

ENV PATH="/usr/local/bun/bin:${PATH}"


#########################################################
# VERIFY RUNTIMES (FAIL FAST IF BROKEN)
#########################################################

RUN node -v && \
    bun -v && \
    deno --version && \
    yt-dlp --version


#########################################################
# Performance / Stability
#########################################################

ENV YTDLP_CACHE_DIR=/tmp/yt-cache
ENV PYTHONUNBUFFERED=1
ENV YTDLP_NO_PART_FILES=1
ENV GOMEMLIMIT=500MiB


#########################################################
# Certificates
#########################################################

COPY --from=builder /etc/ssl/certs /etc/ssl/certs


#########################################################
# App User
#########################################################

RUN useradd -r -u 10001 appuser && \
    mkdir -p /app /tmp/yt-cache && \
    chown -R appuser:appuser /app /tmp/yt-cache

WORKDIR /app

COPY --from=builder /build/app /app/app
RUN chown appuser:appuser /app/app

USER appuser

ENTRYPOINT ["/app/app"]
