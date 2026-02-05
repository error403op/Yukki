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

# Base packages + aria2 (HUGE SPEED BOOST)
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


# -------------------------------------------------------
# Go DNS resolver + force IPv4
# (REAL fix for Railway / weird networks)
# -------------------------------------------------------
ENV GODEBUG=netdns=go+v4
ENV NTG_CALLS_IPV4_ONLY=1


# -------- yt-dlp NIGHTLY (CRITICAL) --------
RUN curl -L https://github.com/yt-dlp/yt-dlp-nightly-builds/releases/latest/download/yt-dlp \
    -o /usr/local/bin/yt-dlp && \
    chmod 0755 /usr/local/bin/yt-dlp


# -------- Deno --------
RUN curl -fsSL https://deno.land/install.sh | sh
ENV PATH="/root/.deno/bin:${PATH}"


# -------- Bun --------
RUN curl -fsSL https://bun.sh/install | bash
ENV PATH="/root/.bun/bin:${PATH}"


# -------- Runtime Verification (FAIL FAST) --------
RUN node -v && \
    bun -v && \
    deno --version && \
    yt-dlp --version


# -------- Performance / Stability --------
ENV YTDLP_CACHE_DIR=/tmp/yt-cache
ENV PYTHONUNBUFFERED=1
ENV YTDLP_NO_PART_FILES=1


# -------- Certificates --------
COPY --from=builder /etc/ssl/certs /etc/ssl/certs


# -------- App User --------
RUN useradd -r -u 10001 appuser && \
    mkdir -p /app && \
    chown -R appuser:appuser /app

WORKDIR /app

COPY --from=builder /build/app /app/app
RUN chown appuser:appuser /app/app

USER appuser

ENTRYPOINT ["/app/app"]
