# opds-web-client

Web frontend for any OPDS catalog server.

## Features

- Browse OPDS navigation and acquisition feeds
- Download files in any format offered by the server
- Optional HTTP Basic Auth per server
- Light/dark theme
- Runs on port 80; put a reverse proxy (nginx, NPM) in front for HTTPS

## Requirements

- Docker
- Docker Compose

## Usage

```bash
docker compose up -d --build
```

Open `http://localhost`. On first visit you will be prompted for the OPDS server URL and optional credentials. The configuration is stored in an httpOnly cookie.

To change the server later, click the gear icon in the top bar.

## Configuration

No environment variables are required. Resource limits in `docker-compose.yml` default to 64 MB RAM / 0.25 CPU — adjust to taste.

## Stack

- Backend: Go (standard library only), single binary, `scratch`-based image
- Frontend: vanilla HTML/CSS/JS, no build step
