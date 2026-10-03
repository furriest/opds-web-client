# opds-web-client

Web frontend for any OPDS catalog server.

## Features

- Browse OPDS 1.x navigation and acquisition feeds
- Download books in any format offered by the server (EPUB, FB2, MOBI, PDF, …)
- **Built-in EPUB reader** — "online" button appears on book cards with an EPUB file; opens a full-page reader with light/dark theme, font size controls, keyboard and swipe navigation, and reading progress
- Cover images when available; cards without covers get a unique color instead of a placeholder
- Book description shown in card (3 lines); full text on hover tooltip
- Clean browser URLs (`/browse/<path>`, `/dl/<path>`, `/read/<path>`) — the OPDS server address is never exposed in the address bar
- TLS fingerprint bypass via [utls](https://github.com/refraction-networking/utls) (Chrome preset) — works with servers that block non-browser TLS, e.g. flibusta.is
- Optional HTTP Basic Auth per server
- Light/dark theme (persists across the catalog and the reader)
- Runs on port 80; put a reverse proxy (nginx, Nginx Proxy Manager) in front for HTTPS

## Requirements

- Docker
- Docker Compose

## Usage

```bash
git clone https://github.com/furriest/opds-web-client
cd opds-web-client
docker compose up -d --build
```

Open `http://localhost`. On first visit you will be prompted for the OPDS server URL and optional credentials. The configuration is stored in an httpOnly cookie.

To change the server later, click the gear icon (⚙) in the top bar.

## Configuration

No environment variables are required. Resource limits in `docker-compose.yml` default to 64 MB RAM / 0.25 CPU — adjust to taste.

## Notes

- On-the-fly format conversion (e.g. flibusta's FB2→EPUB converter) can take 1–3 minutes. The reader shows progress messages and waits up to 10 minutes before giving up.
- epub.js and JSZip are downloaded from jsDelivr CDN **at Docker build time** and embedded in the image — no CDN dependency at runtime.

## Stack

- **Backend:** Go (standard library), single binary, `scratch`-based Docker image
- **Frontend:** Vanilla HTML / CSS / JS, no build step
- **TLS:** [refraction-networking/utls](https://github.com/refraction-networking/utls) v1.6.7
- **EPUB reader:** [epub.js](https://github.com/futurepress/epub.js) v0.3.93 + JSZip v3.10.1
