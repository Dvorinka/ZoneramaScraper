# Zonerama Scraper (Go + Geziyor)

Blazing-fast JSON API to scrape Zonerama albums and photos using [geziyor/geziyor](https://github.com/geziyor/geziyor).

- Input: Zonerama profile/link page, e.g. `https://eu.zonerama.com/Fcbizoni/1419417`
- Output: JSON listing albums and photos.
- Photo links are normalized to `https://eu.zonerama.com/photos/{photoID}_1500x1000.jpg`.

## Requirements
- Go 1.22+

## Install deps
```
go get -u github.com/geziyor/geziyor
```

## Run the server
```
go run ./...
```
The server listens on `http://localhost:8080`.

## API

See full endpoint reference in `API.md`.

### Endpoints
- `/zonerama`
- `/zonerama-album`

### Common query parameters
- `rendered` (bool, default: `true`) — Enable/disable JS rendering. Aliases: `no-render=true` or `no_render=true` to disable.
- `debug` (bool, default: `false`) — If `true`, saves fetched HTML into `debuging/` and serves at `/debuging/`.

### /zonerama
Scrape albums and their photos starting from a Zonerama profile (account) or page URL.

Params:
- `link` (required): A Zonerama URL (profile or album link)
- `album_limit` (int, default: `5`): Max albums to process from a profile (`0` = no limit)
- `photo_limit` (int, default: `10`): Max photos per album (`0` = no limit)
- `concurrency` (int, default: `8`): Max concurrent album fetches when rendering (capped by `album_limit`)

Example:
```
curl "http://localhost:8080/zonerama?link=https://eu.zonerama.com/Fcbizoni/1419417&album_limit=3&photo_limit=25" | jq .
```

### /zonerama-album
Scrape a single album by URL (link must contain `/Album/`).

Params:
- `link` (required): `https://eu.zonerama.com/<Account>/Album/<AlbumId>`
- `photo_limit` (int, default: `10`): Max photos from the album (`0` = no limit)

Example:
```
curl "http://localhost:8080/zonerama-album?link=https://eu.zonerama.com/SomeAccount/Album/13903610&photo_limit=25" | jq .
```

## Notes
- This scraper parses:
  - Albums from the main/profile page by selecting `li.list-alb` and reading `data-url` or the nested anchor `href`.
  - Photos from each album page via `div.gallery-inner[data-type='photo']` and its `data-id` (photo ID).
- For each photo ID, it builds `https://{host}/photos/{id}_1500x1000.jpg`.
- If Zonerama changes their HTML structure, selectors may need to be updated.

Server starts at `:8080`. CORS is enabled.

For full details and response schemas, see `API.md`.

## Disclaimer
Be respectful of Zonerama's terms of service and rate limits. This project is for educational purposes.
