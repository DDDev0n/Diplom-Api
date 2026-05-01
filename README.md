# Bank Processing Data Subsystem

Прототип подсистемы хранения и обработки данных для распределенного процессингового центра.

## Модули

- `api` - Go HTTP API: auth, payments, banker/admin endpoints.
- `worker` - Go обработчик очереди платежей: fraud/processing integration, смена статусов.
- `postgres` - транзакционное хранилище.
- `redis` - кэш для очереди банкира, профилей и быстрых счетчиков.
- `rabbitmq` - брокер сообщений для асинхронной обработки платежей.
- `prometheus` - сбор метрик Go API.
- `grafana` - дашборд поверх Prometheus.
- `postgres-exporter` - метрики PostgreSQL для Prometheus.
- `node-exporter` - системные метрики CPU/RAM/disk.
- `cadvisor` - метрики нагрузки Docker-контейнеров.
- `processing` - внешний сервис на `https://pay.projectl.ru/`; в этом проекте не хранится и не запускается.

Фронтенд в этом репозитории намеренно не реализуется.

## Запуск

```bash
cp .env.example .env
docker compose up --build
```

API будет доступен на `http://localhost:8000`.
Swagger UI - `http://localhost:8000/swagger`.
RabbitMQ UI - `http://localhost:15672`.
Prometheus - `http://localhost:9090`.
Grafana - `http://localhost:3001`, логин/пароль по умолчанию `admin` / `U2Ze8D&bD]+YaMX`.
PostgreSQL metrics - `http://localhost:9187/metrics`.
System metrics - `http://localhost:9100/metrics`.
Container metrics - `http://localhost:8081/metrics`.

Go worker ходит во внешний processing по HTTPS:

```bash
PROCESSING_SERVICE_URL=https://pay.projectl.ru
PROCESSING_LOGIN_PATH=/api/v1/auth/login
PROCESSING_PROCESS_PATH=/api/v1/payments/process
PROCESSING_AUTH_TOKEN=
PROCESSING_AUTH_USERNAME=tester
PROCESSING_AUTH_ROLE=USER
PROCESSING_INSECURE_SKIP_VERIFY=false
```

`PROCESSING_AUTH_TOKEN` нужен только если нужно принудительно использовать заранее выданный Bearer token. Если токен не задан, worker сам получает JWT во внешнем processing через `PROCESSING_LOGIN_PATH` с `PROCESSING_AUTH_USERNAME` и `PROCESSING_AUTH_ROLE`.
`PROCESSING_INSECURE_SKIP_VERIFY=true` можно использовать только для локальной проверки, если у внешнего HTTPS-сервера сертификат выписан на другой домен.

## Проверка

Быстрая проверка API:

```bash
curl http://localhost:8000/health
curl http://localhost:8000/metrics
```

Метрики в Prometheus:

```promql
pg_up
node_load1
node_memory_MemAvailable_bytes
rate(container_cpu_usage_seconds_total[1m])
container_memory_working_set_bytes
```

Интеграционные тесты поднимают стек через Docker Compose и проверяют HTTP API, Swagger/OpenAPI, метрики, Prometheus/Grafana, создание пользователей и платежа:

```bash
./scripts/integration-test.sh
```

Строгую проверку того, что worker реально получил ответ от внешнего processing, включай только когда внешний API доступен с этой машины:

```bash
REQUIRE_EXTERNAL_PROCESSING=true go test -tags=integration ./tests/integration -v
```

## Нагрузочное тестирование

Скрипт поднимает Docker Compose-стек, регистрирует тестовых пользователей, параллельно создает платежи через API и печатает задержки `min/p50/p95/max`:

```bash
./scripts/load-test.sh
```

Параметры можно менять через env:

```bash
LOAD_USERS=50 LOAD_PAYMENTS=1000 LOAD_CONCURRENCY=25 ./scripts/load-test.sh
```

Чтобы тест дополнительно ждал, пока worker обработает платежи через внешний processing:

```bash
LOAD_WAIT_PROCESSING=true LOAD_PAYMENTS=50 LOAD_CONCURRENCY=5 ./scripts/load-test.sh
```

Дефолты бережные: `20` пар пользователей, `200` платежей, `10` параллельных workers. Для внешнего processing лучше повышать нагрузку постепенно.
