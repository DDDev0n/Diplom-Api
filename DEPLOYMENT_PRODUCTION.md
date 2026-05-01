# Production Deployment Guide

## Требования к серверу aeza.ru

### Минимальные требования
- **vCPU:** 2
- **RAM:** 4 GB
- **Storage:** 30 GB SSD  
- **Пропускная способность:** 100 Mbps
- **Стоимость:** ~$5-10/месяц

### Рекомендуемые требования (для 100+ RPS)
- **vCPU:** 4
- **RAM:** 8 GB
- **Storage:** 50+ GB SSD
- **Пропускная способность:** 250+ Mbps
- **Стоимость:** ~$15-25/месяц

## Выбор конфигурации на aeza.ru

1. Перейдите на https://aeza.ru/virtual-servers
2. **Cloud VPS** -> выберите конфигурацию:
   - Disk: SSD (важно!)
   - Os: Ubuntu 22.04 LTS
   - Размер: от 30 GB SSD

3. Выберите регион ближайший к вашим пользователям

## Установка одной командой

### Вариант 1: Автоматизированная установка

После создания сервера (1-2 минуты после оплаты):

```bash
ssh root@<IP_СЕРВЕРА>

# Скачайте скрипт и запустите
curl -fsSL https://raw.githubusercontent.com/<OWNER>/<REPO>/main/scripts/setup-server.sh | bash

# Когда спросит домен - введите ваш (например: api.yourdomain.com)
# Когда закончит - скопируйте пароли в безопасное место
```

**Время выполнения:** ~10-15 минут

### Вариант 2: Ручная установка

Следуйте инструкциям в [DEPLOYMENT.md](./DEPLOYMENT.md)

## После установки

### 1. Добавьте DNS запись

На вашем DNS хостинге добавьте A-запись:

```
Type: A
Name: api (или ваше имя)
Value: <IP_СЕРВЕРА>
TTL: 3600
```

Примеры для популярных DNS провайдеров:
- **Route 53**: Откройте Hosted Zone -> Create Record
- **Cloudflare**: Add Record -> A -> yourdomain.com
- **Namecheap**: Dashboard -> DNS -> A Record
- **Google Domains**: DNS -> Custom records -> Create new record

### 2. Дождитесь распространения DNS (обычно 5-10 минут)

Проверьте:
```bash
nslookup api.yourdomain.com
# или
dig api.yourdomain.com
```

### 3. Протестируйте доступ

```bash
# API health
curl https://api.yourdomain.com/health

# Swagger
open https://api.yourdomain.com/swagger

# Grafana
open https://api.yourdomain.com/grafana
```

## Оптимизация для Production

### 1. Database Connection Pooling

В `.env` добавьте параметры пула подключений:

```env
DATABASE_URL=postgres://user:pass@postgres:5432/db?sslmode=disable
# Опционально для Go:
DATABASE_MAX_CONNECTIONS=20
DATABASE_MIN_CONNECTIONS=5
```

### 2. Кэширование

Убедитесь, что Redis используется для:
- Кэширования данных пользователя
- Лимитирования запросов (rate limiting)
- Сессий (если используется)

```env
REDIS_URL=redis://redis:6379/0
REDIS_MAX_RETRIES=3
```

### 3. Мониторинг и Alert'ы

Настройте в Grafana:

1. **Alert Rules:**
   - CPU > 80% на 5 минут
   - Memory > 85% на 5 минут
   - PostgreSQL connections > 50
   - Error rate > 1%

2. **Notification Channels:**
   - Email
   - Slack (рекомендуется)
   - PagerDuty (для критичных)

### 4. Масштабирование

Если нужно больше capacity:

**Горизонтальное (несколько серверов):**
- Добавьте load balancer (Nginx, HAProxy)
- Масштабируйте workers на нескольких машинах
- Используйте managed PostgreSQL (для высокой доступности)

**Вертикальное (больше ресурсов на одном сервере):**
- Увеличьте vCPU и RAM
- Увеличьте SSD storage

## Backup и Disaster Recovery

### Автоматические бэкапы

Скрипт сохраняет бэкапы в `/opt/backups/` каждый день.

Проверьте:
```bash
ssh root@<IP> "ls -lh /opt/backups/"
```

### Скачивание бэкапов локально

```bash
# Создайте папку локально
mkdir -p ~/backups/bank-api

# Скачайте все бэкапы
scp -r root@<IP>:/opt/backups/* ~/backups/bank-api/

# Или синхронизируйте (рекомендуется)
rsync -avz root@<IP>:/opt/backups/ ~/backups/bank-api/
```

### Восстановление БД

Если нужно восстановить БД из бэкапа:

```bash
ssh root@<IP> << 'EOF'
cd /opt/bank-api
DATE="20240501_020000"  # Дата бэкапа

# Восстановить БД
gunzip -c /opt/backups/db-${DATE}.sql.gz | docker-compose exec -T postgres psql -U bank_user_prod bank_processing_prod

echo "Database restored from backup"
EOF
```

## Логирование и Мониторинг

### Централизованное логирование (опционально)

Если нужно отправлять логи на внешний сервис:

1. **ELK Stack** (самохост):
   - Добавьте Elasticsearch, Logstash, Kibana в docker-compose.yml

2. **Managed сервисы:**
   - Datadog
   - New Relic
   - Papertrail
   - Sumologic

### Метрики

Prometheus собирает метрики автоматически. Доступны:

```promql
# HTTP запросы
rate(http_requests_total[5m])

# Ошибки
rate(http_requests_total{status=~"5.."}[5m])

# Задержка
histogram_quantile(0.95, http_request_duration_seconds_bucket)

# PostgreSQL
pg_stat_statements_calls
pg_slow_queries

# Docker
container_cpu_usage_seconds_total
container_memory_working_set_bytes
```

## Безопасность - Checklist

- [ ] SSH ключи (не password auth)
- [ ] SSH на нестандартный порт (опционально)
- [ ] Firewall включен и настроен
- [ ] Все пароли сильные (128+ bit entropy)
- [ ] HTTPS включен с Let's Encrypt
- [ ] WAF/DDoS protection (Cloudflare, AWS Shield)
- [ ] Rate limiting включен на API
- [ ] SQL injection protection (ORM или prepared statements)
- [ ] CORS правильно настроен
- [ ] API keys хранятся в .env (не в коде)
- [ ] Логирование безопасности включено
- [ ] Регулярные security updates

## Масштабирование примеры

### Конфиг для 100 RPS:
```yaml
# 1 сервер
- CPU: 2-4 cores
- RAM: 4-8 GB
- WORKER_CONCURRENCY: 10-20
```

### Конфиг для 1000 RPS:
```yaml
# 1 основной сервер + 2-3 worker сервера
API сервер:
- CPU: 4 cores
- RAM: 8 GB
- REPLICA: 2-3

Worker сервера:
- CPU: 2 cores каждый
- RAM: 4 GB каждый
- WORKER_CONCURRENCY: 5-10 на каждом

Database:
- Managed PostgreSQL (RDS, CloudSQL)
- CPU: 4+ cores
- RAM: 16+ GB
- SSD Storage: 100+ GB
```

## Вкладка Grafana дашбордов

После развертывания, дашборд будет доступен на:
```
https://<домен>/grafana
```

Встроенные дашборды:
- Bank API (наш дашборд)
- Node Exporter (системные метрики)
- PostgreSQL (БД метрики)
- Docker (контейнеры)

## Поддержка и Troubleshooting

### Проверка здоровья

```bash
# На сервере
ssh root@<IP> << 'EOF'
echo "=== Docker status ==="
docker-compose -f /opt/bank-api/docker-compose.yml ps

echo "=== Services health ==="
docker-compose -f /opt/bank-api/docker-compose.yml exec api curl -s http://localhost:8000/health

echo "=== Logs last 100 lines ==="
docker-compose -f /opt/bank-api/docker-compose.yml logs -n 100
EOF
```

### Контакты поддержки aeza.ru

- Support: https://aeza.ru/support
- Email: support@aeza.ru
- Telegram: @aeza_support

### Полезные команды для Debug

```bash
# Объем используемого дискового пространства
ssh root@<IP> "df -h"

# Использование памяти
ssh root@<IP> "free -h"

# Нагрузка на CPU
ssh root@<IP> "uptime"

# Статус контейнеров
ssh root@<IP> "docker stats"

# Размер БД
ssh root@<IP> "cd /opt/bank-api && docker-compose exec postgres du -sh /var/lib/postgresql/data"
```

## Обновление приложения

```bash
ssh root@<IP> << 'EOF'
cd /opt/bank-api

# Получить новую версию
git pull

# Перестроить и перезагрузить
docker-compose up -d --build

# Проверить статус
docker-compose ps

# Проверить логи
docker-compose logs -f api
EOF
```

---

**Поздравляем! Ваше приложение теперь работает на production сервере!** 🎉
