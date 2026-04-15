# Tenor API Integration

Короткая справка по интеграции Tenor (GIF search + trending) в messaging-сервис. Закрывает чек-бокс "Изучить Tenor API" из [PHASES.md](../PHASES.md) Phase 5.

Код: [services/messaging/internal/tenor/client.go](../services/messaging/internal/tenor/client.go)

## Зачем

Поиск и trending-фид для GIF-вкладки в composer'е. Используется frontend'ом через messaging endpoints `GET /gifs/search` и `GET /gifs/trending` (прокси к Tenor v2 API).

## Endpoint и авторизация

- Base URL: `https://tenor.googleapis.com/v2`
- Используем два метода:
  - `GET /search?q=...&limit=...&pos=...` — поиск по запросу
  - `GET /featured?limit=...&pos=...` — trending фид
- Авторизация: query params `key` (API-ключ) + `client_key=orbit-messaging`
- HTTP client timeout: 10s
- User-Agent: `OrbitMessenger/1.0`

## Конфигурация

| Env | Назначение |
|-----|-----------|
| `TENOR_API_KEY` | API-ключ Tenor. Получить: https://developers.google.com/tenor/guides/quickstart |

Без ключа клиент возвращает `Internal("Tenor API key is not configured")` при первом вызове — сервис стартует, но GIF-tab не работает.

## Rate limits

**Официальный лимит Tenor:** v2 API бесплатный для authenticated запросов без декларированного hard-cap на request/min. Google рекомендует держать throughput "reasonable", при превышении возвращает `429 Too Many Requests`.

**Наш внутренний guard** (`checkRateLimit`):
- Ключ Redis: `ratelimit:tenor:{YYYYMMDDHHMM}` (поминутный bucket)
- Лимит: **100 запросов/минуту на весь messaging-сервис** (не на пользователя)
- При превышении: `apperror.TooManyRequests("Tenor rate limit exceeded")`
- Если Redis недоступен — fail-open (rate limit пропускается, логируется warning). Это сознательное решение: Tenor не критичный путь, ронять GIF-tab из-за Redis не хочется.

**При `429` от самого Tenor:** клиент возвращает `TooManyRequests("Tenor API rate limit exceeded")` пользователю, frontend показывает toast.

## Response format

Tenor возвращает:
```json
{
  "results": [
    {
      "id": "...",
      "title": "...",
      "content_description": "...",
      "media_formats": {
        "mp4":         { "url": "...", "preview": "...", "dims": [w, h] },
        "mediummp4":   { ... },
        "nanomp4":     { ... },
        "gif":         { ... },
        "mediumgif":   { ... },
        "tinygif":     { ... },
        "nanogif":     { ... }
      }
    }
  ],
  "next": "cursor-string"
}
```

### Выбор формата

`mapTenorResults` извлекает из `media_formats` один primary (видео) и один preview (плейсхолдер):

- **Primary** (что отправляем как GIF-сообщение): в порядке приоритета `mp4 → mediummp4 → nanomp4 → tinymp4 → gif → mediumgif → tinygif`. MP4 предпочитаем чтобы не тащить тяжёлые GIF-файлы — они ~5-10× больше при том же качестве.
- **Preview** (thumbnail для picker'а): в порядке `tinygif → nanogif → gif → mediumgif → tinymp4 → nanomp4`, сначала пробуем `.preview` URL, потом `.url`.

Если `primary.URL` пустой — результат пропускается (`continue`).

## Pagination

Tenor использует cursor-based pagination через параметр `pos`. Наш wrapper прозрачно пробрасывает:
- Запрос: `Search(ctx, query, limit, pos)` / `Trending(ctx, limit, pos)`
- Ответ: `(gifs, nextPos, err)` — `nextPos` передаётся обратно клиенту, на следующей странице передаётся как `pos`

Лимит на страницу: `defaultResultLimit=20`, `maxResultLimit=50` (выход за пределы силком сбрасывается на default).

## Caching

**Только для `Trending`** — search-запросы не кэшируем (бессмысленно, query уникальны).

- Ключ: `tenor:trending:{limit}:{pos|"first"}`
- TTL: **5 минут** (`trendingCacheTTL`)
- Storage: Redis через `Set` с TTL, payload — JSON `{gifs, next_pos}`
- Если Redis недоступен — fail-open, запрос идёт напрямую в Tenor

Почему именно trending: фид обновляется у Tenor редко (часы), а на frontend'е при открытии GIF-tab ходят сразу все активные пользователи. Без кэша это 150× одинаковых запросов в Tenor при каждом rollout'е.

## Security

- `apiKey` подставляется в query params **в последний момент** внутри `fetch()` — не логируется, не выходит из пакета
- `cloneValues(params)` используется перед добавлением ключа в query — input params не мутируются вызывающим кодом
- Response body при non-2xx читается через `io.LimitReader(resp.Body, 512)` — защита от злонамеренных больших error-ответов

## Что НЕ сделано (и почему не надо)

- **Per-user rate limiting внутри Tenor wrapper** — уже есть на уровне gateway (600 req/min/user для general API), дублировать бессмысленно
- **Search cache** — query-пространство слишком большое, hit rate был бы <5%
- **Fallback на GIPHY** — ТЗ требует именно Tenor, запасного провайдера не предусмотрено
- **Content moderation filter (`contentfilter=high`)** — не включён. Для corporate-мессенджера на 150 сотрудников можно добавить одной строкой в `params` если потребуется
