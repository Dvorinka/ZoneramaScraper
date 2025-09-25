# ZoneramaScraper API

This service scrapes album and photo metadata from public Zonerama pages.

- Base URL: `http://localhost:8080`
- CORS: `Access-Control-Allow-Origin: *`
- Default rendering: JavaScript rendering is ON by default (uses a local Chrome). You can disable it per request.
- Debugging: When `debug=true`, fetched HTML is saved under `debuging/` and served from `GET /debuging/`.

## Endpoints

- GET `/zonerama`
- GET `/zonerama-album`

Both endpoints return JSON.

---

## GET /zonerama
Scrape albums and their photos starting from a Zonerama profile or page link. The router auto-detects if the provided link is a profile (account) or a single album and proceeds accordingly.

Query parameters:
- `link` (required): A Zonerama URL.
  - Examples: `https://eu.zonerama.com/<Account>/<TabId>` or a profile URL.
- `album_limit` (optional, int): Maximum number of albums to process from a profile. Default: `5`. `0` = no limit.
- `photo_limit` (optional, int): Maximum photos to collect per album. Default: `10`. `0` = no limit.
- `rendered` (optional, bool): Enable/disable JS rendering. Default: `true`.
  - Aliases to disable rendering: `no-render=true` or `no_render=true`.
- `concurrency` (optional, int): Max concurrent album fetches when rendering. Default: `8` (capped by `album_limit`).
- `debug` (optional, bool): If `true`, saves fetched HTML into `debuging/` and serves via `GET /debuging/`.

Example:
```
GET /zonerama?link=https://eu.zonerama.com/SomeAccount/1419417&album_limit=3&photo_limit=25
```

Response shape:
```json
{
  "input_link": "https://eu.zonerama.com/SomeAccount/1419417",
  "albums": [
    {
      "id": "13903610",
      "title": "Trip",
      "url": "https://eu.zonerama.com/SomeAccount/Album/13903610",
      "date": "20. 9. 2025",
      "photos_count": 42,
      "views_count": 1234,
      "photos": [
        { "id": "1234567", "page_url": "/Photo/13903610/1234567", "image_1500": "https://eu.zonerama.com/photos/1234567_1500x1000.jpg" }
      ]
    }
  ]
}
```

Notes:
- Albums are sorted descending by date when dates can be parsed; otherwise by title.
- Photo URLs are normalized to `https://{host}/photos/{photoID}_1500x1000.jpg`.

---

## GET /zonerama-album
Scrape a single album by URL (the link must contain `/Album/`).

Query parameters:
- `link` (required): A Zonerama album URL.
  - Example: `https://eu.zonerama.com/<Account>/Album/<AlbumId>`
- `photo_limit` (optional, int): Maximum photos to collect from the album. Default: `10`. `0` = no limit.
- `rendered` (optional, bool): Enable/disable JS rendering. Default: `true`.
  - Aliases to disable: `no-render=true` or `no_render=true`.
- `debug` (optional, bool): If `true`, saves fetched HTML into `debuging/` and serves via `GET /debuging/`.

Example:
```
GET /zonerama-album?link=https://eu.zonerama.com/SomeAccount/Album/13903610&photo_limit=25
```

Response shape:
```json
{
  "input_link": "https://eu.zonerama.com/SomeAccount/Album/13903610",
  "albums": [
    {
      "id": "13903610",
      "title": "Trip",
      "url": "https://eu.zonerama.com/SomeAccount/Album/13903610",
      "date": "20. 9. 2025",
      "photos_count": 42,
      "views_count": 1234,
      "photos": [
        { "id": "1234567", "page_url": "/Photo/13903610/1234567", "image_1500": "https://eu.zonerama.com/photos/1234567_1500x1000.jpg" }
      ]
    }
  ]
}
```

---

## Data models

Album:
```json
{
  "id": "string",
  "title": "string",
  "url": "string",
  "date": "string (optional)",
  "photos_count": "int (optional)",
  "views_count": "int (optional)",
  "photos": [Photo]
}
```

Photo:
```json
{
  "id": "string",
  "page_url": "string (optional)",
  "image_1500": "string"
}
```

Root response:
```json
{
  "input_link": "string",
  "albums": [Album]
}
```

---

## Accounts vs Albums
- An "account" (profile) page lists albums. Use `/zonerama` with a profile URL to fetch albums for that account.
- An "album" page shows photos. Use `/zonerama-album` or `/zonerama` with an album URL.

---

## Operational notes
- Requires Go 1.22+.
- JS rendering requires a local Chrome installation available to the geziyor renderer.
- Debug files are written to the `debuging/` directory.
