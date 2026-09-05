# glance-status: сводный health-виджет для Glance

## Проблема

CPU/RAM/Disk сейчас видны только как спарклайн-графики (`glance-grafana-sparkline`) —
для "работает всё нормально или нет" приходится вглядываться в график. Скорость
интернета вообще нигде не видна на дашборде, хотя `speedtest-tracker` её уже
меряет. А статус сервисов размазан по индивидуальным точкам на карточках
`glance-services` — нет одного места, которое одним взглядом говорит "всё ок"
или "вот что сломано".

## Цель

Новый custom-виджет Glance (по образцу `glance-homeassistant` /
`glance-grafana-sparkline`) — компактная карточка с двумя блоками:

1. Метрики (CPU / RAM / Disk / Disk (ext) / Speed down / Speed up) — значение +
   стрелка тренда за последний час, стрелка скрывается если значение не
   изменилось.
2. Статус-строка — `All operational` / `Warning: ...` / `Error: ...` по всем
   отслеживаемым сервисам и инфраструктуре.

## Область охвата (v1)

### Метрики

| Метрика | Источник | Запрос |
|---|---|---|
| CPU | Grafana → Prometheus (`monitoring` стек) | `100 - (avg(rate(node_cpu_seconds_total{mode="idle"}[5m])) * 100)` |
| RAM | тот же | `100 * (1 - (node_memory_MemAvailable_bytes / node_memory_MemTotal_bytes))` |
| Disk (внутренний) | тот же, `mountpoint="/"` | `100 * (1 - (node_filesystem_avail_bytes{mountpoint="/"} / node_filesystem_size_bytes{mountpoint="/"}))` |
| Disk (внешний) | тот же, `mountpoint="/mnt/media-storage"` | тот же запрос с другим mountpoint |
| Speed down/up | speedtest-tracker REST API, последний результат | — |

Каждая Prometheus-метрика запрашивается дважды за один batch-вызов
`/api/ds/query`: текущее значение и то же выражение с `offset 1h` — тренд
считается из разницы, без своего хранимого состояния.

Speed: `GET /api/v1/results?sort=-created_at&per_page=2` с Bearer-токеном.
Первый результат — "сейчас", второй — "час назад". Если второй результат
старше ~2 часов (значит собственное расписание speedtest-tracker ещё не
настроено на почасовой прогон или сбоило) — тренд не считается, стрелка
скрывается, значение просто показывается без стрелки.

**Форматирование:** CPU/RAM/Disk — проценты. Speed — абсолютные Mbps
(download/upload отдельно), не проценты — для скорости "% от чего" не имеет
естественного смысла.

**Стрелка тренда:** направление всегда отражает знак изменения (▲ = выросло,
▼ = упало). Цвет — по смыслу метрики, не по направлению: для CPU/RAM/Disk
рост = плохо = `--color-negative`, падение = хорошо = `--color-primary`; для
Speed — наоборот, рост = хорошо = `--color-primary`. Стрелка скрывается, если
текущее и часовое-назад значения совпадают при округлении до целого числа
(проценты — до целого %, Mbps — до целого Mbps), либо если тренд-данные
недостаточны/устарели (см. Speed выше).

### Статус-строка

Три состояния: `All operational` (`--color-primary`) / `Warning` / `Error`
(оба — `--color-negative`, различаются заливкой индикатора: Error — сплошной
кружок, Warning — кружок-обводка; тот же приём, что у встроенного
`notice-icon-major`/`-minor` в самом Glance — **в теме Glance нет отдельного
"warning"-цвета, поэтому третье состояние кодируется не цветом, а весом
индикатора**).

Правило приоритета: любой сервис недоступен → `Error` со списком имён через
запятую (например `Error: Kavita, StirlingPDF`); иначе любая из четырёх
метрик (CPU/RAM/Disk×2) > `warning_threshold_percent` (по умолчанию 90) →
`Warning` со списком "имя метрики + текущее значение" через запятую
(например `Warning: Disk (ext) 91%`); иначе `All operational`.

Отслеживаемые сервисы:
- 10 клиентских (Jellyfin, Seerr, Kavita, Audiobookshelf, StirlingPDF,
  Transmute, Librarr, qBittorrent, TorrServer, speedtest-tracker) — через
  новый `GET /status.json` в `glance-services` (см. "Сопутствующее
  изменение" ниже), а не повторной реализацией той же проверки.
- npmplus (`:81`), Home Assistant (`:8123`) — прямой `GET`, тот же критерий
  2xx-399 = up, что уже в `glance-services/internal/services/check.go`.
- Prometheus + Grafana — отдельной проверки нет: если весь batch-запрос
  метрик к Grafana проваливается, эта пара считается недоступной и попадает
  в `Error` тем же фактом.

## Дизайн

**Стек:** Go 1.23, тот же паттерн, что у остальных виджетов —
`main.go` (HTTP-хендлеры, `/widget`, `/healthz`) + `config.go` +
`config.example.yml` (никакого `config.docker-default.yml` — см.
"Публичность репозитория" ниже) + пакеты:

- `internal/grafana/` — HTTP-клиент `/api/ds/query`, batch из 8 PromQL-целей
  (4 метрики × текущее/offset 1h), парсинг ответа в `map[string]float64`.
- `internal/speedtest/` — клиент REST API speedtest-tracker, парсинг
  последних 2 результатов, проверка свежести.
- `internal/status/` — агрегация: вызов `glance-services` `/status.json`,
  прямые проверки npmplus/HA, сведение всего в один объект
  `{Level: ok|warning|error, Problems: []string}`.
- `internal/render/` — HTML-фрагмент + инлайн-CSS в теме Glance (тот же
  паттерн `<style>` внутри фрагмента, что у `glance-services`), inline SVG
  для стрелок (не эмодзи/иконочный шрифт — не тянет внешних зависимостей и
  красится через `currentColor`/CSS-переменные).

**Параллелизм:** три вызова (Grafana, speedtest-tracker, статус) идут
одновременно (goroutines + `sync.WaitGroup`), как в `glance-services`.

**Конфиг** (`config.yml`, монтируется как том — см. ниже):
```yaml
title: Server Health
grafana:
  url: ...
  token: ...
  datasource_uid: ...
metrics:
  cpu_query: '...'
  ram_query: '...'
  disk_internal_query: '...'
  disk_external_query: '...'
speedtest:
  url: ...
  token: ...
services_status_url: ${SERVICES_STATUS_URL}
infra_checks:
  - name: npmplus
    check_url: ${INFRA_CHECK_NPMPLUS_URL}
  - name: Home Assistant
    check_url: ${INFRA_CHECK_HOMEASSISTANT_URL}
warning_threshold_percent: 90
```

## Сопутствующее изменение в `glance-services`

Добавить `GET /status.json`, отдающий текущий результат уже существующей
`CheckAll` в виде `[{"name": "...", "up": true}]`. Никакой новой
health-check-логики — просто новый HTTP-хендлер поверх уже работающего кода.
Отдельная маленькая задача в том же цикле реализации, в отдельном PR/ветке
этого (другого) репозитория.

## Обработка ошибок

- Grafana недоступна целиком → метрики показывают "no data" (как у
  `glance-grafana-sparkline`), пара "Prometheus + Grafana" → `Error`.
- Один PromQL-запрос из batch упал (например, точка монтирования исчезла) →
  "no data" только в этом тайле, остальное рендерится. Отдельной записи в
  статус-строке не создаём — сигнал уже виден в тайле.
- speedtest-tracker недоступен → тайлы Speed показывают "no data". Отдельной
  записи в статус-строке не создаём — доступность самого сервиса уже
  отслеживается через `glance-services` (он в списке 10 клиентских).
- `glance-services` `/status.json` недоступен → теряем все 10 статусов разом.
  Показываем `Error: service status unavailable` — честно про сам факт
  недоступности проверки, не выдумывая статус отдельных 10 сервисов.
- Виджет всегда отдаёт `200` с валидным HTML, даже при частичной деградации —
  никогда не показывает Glance-овый generic "widget failed".

## Публичность репозитория

**Публичный**, как `glance-homeassistant`/`glance-jellyfin`/
`glance-grafana-sparkline` — **не** как `glance-services` (тот приватный
из-за случайно закоммиченных операционных деталей инфраструктуры в
одной из прошлых сессий).
Здесь сознательно нет `config.docker-default.yml` — `config.example.yml`
содержит только плейсхолдеры (как у `glance-grafana-sparkline`:
`${GRAFANA_URL}`, `CHANGE_ME`), реальные IP/токены живут только в
примонтированном `config.yml` на самом сервере, никогда не в git — **это
правило распространяется на весь репозиторий, включая саму спеку и план**,
а не только на итоговый код. Финальное ревью перед мержем явно проверяет
весь committed-текст (не только диф последних задач) на случайно попавшие
туда инфраструктурные детали.

## Развёртывание

Тот же CI-паттерн (`test` → `docker` job, push в `main` → образ на
`ghcr.io/sidun-av/glance-status:latest`), деплой ещё одним контейнером в
тот же Docker-стек, где уже развёрнут Glance, `type: extension` в
`glance.yml`, `cache: 5m`.

**Ручная подготовка вне кода** (в README нового репозитория):
1. Speedtest-tracker: создать API-токен со scope `results:read`
   (Admin → API Tokens), включить собственное расписание замеров раз в час
   (Settings) — без этого тренд-стрелка Speed никогда не наберёт данных.
2. Grafana: переиспользовать тот же Service Account токен
   (Viewer + Data Source Reader), что уже настроен для
   `glance-grafana-sparkline`, либо создать новый с той же ролью.
3. Смонтировать заполненный `config.yml` в контейнер (как у
   `glance-grafana-sparkline`).

## Вне рамок (явно не делаем в v1)

- Не гоняем speedtest сами — только читаем уже существующую историю
  speedtest-tracker.
- Не добавляем степень деградации для сервисов (только up/down, без
  "медленно, но работает") — по решению пользователя.
- Не делаем `warning_threshold_percent` отдельным порогом на метрику — один
  общий порог на все четыре.
- Не добавляем инфраструктурные сервисы сверх выбранных трёх (npmplus,
  Prometheus+Grafana, Home Assistant) — Authentik и остальное вне охвата v1.
- Не рисуем историю/график — это уже делает `glance-grafana-sparkline`,
  здесь только снимок "сейчас vs час назад".

## Открытые допущения (не блокируют реализацию)

- Точный синтаксис `offset 1h` в связке с `rate(...[5m])` для CPU
  проверяется живым запросом на этапе реализации.
- Доступность admin-интерфейса npmplus и веб-интерфейса Home Assistant без
  авторизации для простого GET-пинга подтверждается на этапе реализации.
- Имя репозитория — `glance-status`.
