# Развертывание на сервер aeza.ru

## 1. Рекомендации по конфигурации сервера

**Минимальные требования:**
- **CPU:** 2 ядра
- **RAM:** 4 GB
- **Storage:** 30 GB SSD
- **ОС:** Ubuntu 22.04 LTS

**Рекомендуемая конфигурация:**
- **CPU:** 4 ядра
- **RAM:** 8 GB
- **Storage:** 50+ GB SSD
- **ОС:** Ubuntu 22.04 LTS

## 2. Подготовка сервера

### 2.1 Подключитесь к серверу по SSH
```bash
ssh root@<IP_СЕРВЕРА>
```

### 2.2 Обновите систему
```bash
apt update && apt upgrade -y
apt install -y curl wget git build-essential
```

### 2.3 Установите Docker и Docker Compose
```bash
# Установка Docker
curl -fsSL https://get.docker.com -o get-docker.sh
sh get-docker.sh

# Добавьте текущего пользователя в группу docker
usermod -aG docker $USER
newgrp docker

# Установка Docker Compose
curl -L "https://github.com/docker/compose/releases/latest/download/docker-compose-$(uname -s)-$(uname -m)" -o /usr/local/bin/docker-compose
chmod +x /usr/local/bin/docker-compose

# Проверка
docker --version
docker-compose --version
```

### 2.4 Установите Nginx как reverse proxy
```bash
apt install -y nginx
```

## 3. Развертывание приложения

### 3.1 Клонируйте репозиторий
```bash
cd /opt
git clone <ВАШ_РЕПОЗИТОРИЙ> bank-api
cd bank-api
```

### 3.2 Создайте production .env файл
```bash
POSTGRES_PASSWORD=$(openssl rand -hex 32)
RABBITMQ_PASSWORD=$(openssl rand -hex 24)
JWT_SECRET=$(openssl rand -hex 48)
GRAFANA_PASSWORD=$(openssl rand -hex 24)

cat > .env << EOF
# Database
POSTGRES_DB=bank_processing_prod
POSTGRES_USER=bank_user_prod
POSTGRES_PASSWORD=${POSTGRES_PASSWORD}
DATABASE_URL=postgres://bank_user_prod:${POSTGRES_PASSWORD}@postgres:5432/bank_processing_prod?sslmode=disable

# Cache & Queue
REDIS_URL=redis://redis:6379/0
RABBITMQ_DEFAULT_USER=bank_user_prod
RABBITMQ_DEFAULT_PASS=${RABBITMQ_PASSWORD}
RABBITMQ_URL=amqp://bank_user_prod:${RABBITMQ_PASSWORD}@rabbitmq:5672/

# JWT
JWT_SECRET=${JWT_SECRET}
JWT_ALGORITHM=HS256
ACCESS_TOKEN_EXPIRE_MINUTES=120

# External Processing Service
PROCESSING_SERVICE_URL=https://pay.projectl.ru
PROCESSING_LOGIN_PATH=/api/v1/auth/login
PROCESSING_PROCESS_PATH=/api/v1/payments/process
PROCESSING_AUTH_TOKEN=<YOUR_TOKEN>
PROCESSING_AUTH_USERNAME=tester
PROCESSING_AUTH_ROLE=USER
PROCESSING_INSECURE_SKIP_VERIFY=false

# Payments
PAYMENT_REVIEW_THRESHOLD=100000

# Grafana
GRAFANA_ADMIN_USER=admin
GRAFANA_ADMIN_PASSWORD=${GRAFANA_PASSWORD}
EOF
```

**Важно:** Сохраните созданные пароли в защищенном месте!

### 3.3 Запустите приложение
```bash
docker-compose up -d --build
```

Проверьте статус контейнеров:
```bash
docker-compose ps
```

## 4. Настройка Nginx как reverse proxy

### 4.1 Создайте конфиг для Nginx
```bash
cat > /etc/nginx/sites-available/bank-api << 'EOF'
upstream bank_api {
    server localhost:8000;
}

upstream grafana {
    server localhost:3001;
}

upstream prometheus {
    server localhost:9090;
}

server {
    listen 80;
    server_name _;
    client_max_body_size 10M;

    # API endpoints
    location / {
        proxy_pass http://bank_api;
        proxy_http_version 1.1;
        proxy_set_header Upgrade $http_upgrade;
        proxy_set_header Connection 'upgrade';
        proxy_set_header Host $host;
        proxy_set_header X-Real-IP $remote_addr;
        proxy_set_header X-Forwarded-For $proxy_add_x_forwarded_for;
        proxy_set_header X-Forwarded-Proto $scheme;
        proxy_cache_bypass $http_upgrade;
        proxy_connect_timeout 60s;
        proxy_send_timeout 60s;
        proxy_read_timeout 60s;
    }

    # Grafana dashboard
    location /grafana/ {
        proxy_pass http://grafana/;
        proxy_http_version 1.1;
        proxy_set_header Host $host;
        proxy_set_header X-Real-IP $remote_addr;
        proxy_set_header X-Forwarded-For $proxy_add_x_forwarded_for;
        proxy_set_header X-Forwarded-Proto $scheme;
    }

    # Prometheus metrics
    location /prometheus/ {
        proxy_pass http://prometheus/;
        proxy_http_version 1.1;
        proxy_set_header Host $host;
        proxy_set_header X-Real-IP $remote_addr;
        proxy_set_header X-Forwarded-For $proxy_add_x_forwarded_for;
        proxy_set_header X-Forwarded-Proto $scheme;
    }

    # RabbitMQ management UI (опционально)
    location /rabbitmq/ {
        proxy_pass http://localhost:15672/;
        proxy_http_version 1.1;
        proxy_set_header Host $host;
        proxy_set_header X-Real-IP $remote_addr;
        proxy_set_header X-Forwarded-For $proxy_add_x_forwarded_for;
        proxy_set_header X-Forwarded-Proto $scheme;
    }
}
EOF
```

### 4.2 Включите конфиг
```bash
ln -s /etc/nginx/sites-available/bank-api /etc/nginx/sites-enabled/
rm /etc/nginx/sites-enabled/default

# Проверьте конфиг
nginx -t

# Перезагрузите Nginx
systemctl restart nginx
```

## 5. SSL сертификат (Let's Encrypt)

### 5.1 Установите Certbot
```bash
apt install -y certbot python3-certbot-nginx
```

### 5.2 Получите сертификат
```bash
certbot certonly --standalone -d <ВАШ_ДОМЕН> -d www.<ВАШ_ДОМЕН>
```

### 5.3 Обновите Nginx конфиг для HTTPS
```bash
cat > /etc/nginx/sites-available/bank-api << 'EOF'
upstream bank_api {
    server localhost:8000;
}

upstream grafana {
    server localhost:3001;
}

upstream prometheus {
    server localhost:9090;
}

# Redirect HTTP to HTTPS
server {
    listen 80;
    server_name _;
    return 301 https://$host$request_uri;
}

server {
    listen 443 ssl http2;
    server_name _;

    ssl_certificate /etc/letsencrypt/live/<ВАШ_ДОМЕН>/fullchain.pem;
    ssl_certificate_key /etc/letsencrypt/live/<ВАШ_ДОМЕН>/privkey.pem;
    ssl_protocols TLSv1.2 TLSv1.3;
    ssl_ciphers HIGH:!aNULL:!MD5;

    client_max_body_size 10M;

    location / {
        proxy_pass http://bank_api;
        proxy_http_version 1.1;
        proxy_set_header Upgrade $http_upgrade;
        proxy_set_header Connection 'upgrade';
        proxy_set_header Host $host;
        proxy_set_header X-Real-IP $remote_addr;
        proxy_set_header X-Forwarded-For $proxy_add_x_forwarded_for;
        proxy_set_header X-Forwarded-Proto $scheme;
        proxy_cache_bypass $http_upgrade;
        proxy_connect_timeout 60s;
        proxy_send_timeout 60s;
        proxy_read_timeout 60s;
    }

    location /grafana/ {
        proxy_pass http://grafana/;
        proxy_http_version 1.1;
        proxy_set_header Host $host;
        proxy_set_header X-Real-IP $remote_addr;
        proxy_set_header X-Forwarded-For $proxy_add_x_forwarded_for;
        proxy_set_header X-Forwarded-Proto $scheme;
    }

    location /prometheus/ {
        proxy_pass http://prometheus/;
        proxy_http_version 1.1;
        proxy_set_header Host $host;
        proxy_set_header X-Real-IP $remote_addr;
        proxy_set_header X-Forwarded-For $proxy_add_x_forwarded_for;
        proxy_set_header X-Forwarded-Proto $scheme;
    }
}
EOF

nginx -t
systemctl restart nginx
```

## 6. Автоматическое обновление сертификата
```bash
systemctl enable certbot.timer
systemctl start certbot.timer
```

## 7. Backup стратегия

### 7.1 Создайте скрипт бэкапа
```bash
mkdir -p /opt/backups
cat > /opt/backups/backup.sh << 'EOF'
#!/bin/bash
set -e

BACKUP_DIR="/opt/backups"
DATE=$(date +%Y%m%d_%H%M%S)
BACKUP_NAME="bank-api-backup-${DATE}"

# Backup PostgreSQL
docker-compose -f /opt/bank-api/docker-compose.yml exec -T postgres pg_dump -U bank_user_prod bank_processing_prod > "${BACKUP_DIR}/db-${DATE}.sql"

# Backup volumes
tar -czf "${BACKUP_DIR}/volumes-${DATE}.tar.gz" -C /opt/bank-api docker-compose.yml

echo "Backup completed: ${BACKUP_NAME}"

# Keep only last 7 backups
find ${BACKUP_DIR} -name "db-*.sql" -mtime +7 -delete
find ${BACKUP_DIR} -name "volumes-*.tar.gz" -mtime +7 -delete
EOF

chmod +x /opt/backups/backup.sh
```

### 7.2 Добавьте в cron
```bash
# Бэкап каждый день в 2:00 AM
crontab -e
# Добавьте строку:
0 2 * * * /opt/backups/backup.sh
```

## 8. Мониторинг

### 8.1 Логи контейнеров
```bash
# Просмотр логов API
docker-compose -f /opt/bank-api/docker-compose.yml logs -f api

# Просмотр логов Worker
docker-compose -f /opt/bank-api/docker-compose.yml logs -f worker

# Просмотр логов всех сервисов
docker-compose -f /opt/bank-api/docker-compose.yml logs -f
```

### 8.2 Статус сервисов
```bash
docker-compose -f /opt/bank-api/docker-compose.yml ps
```

## 9. Обновление приложения

```bash
cd /opt/bank-api

# Получите последние изменения
git pull

# Пересоберите и перезапустите
docker-compose up -d --build

# Проверьте статус
docker-compose ps
```

## 10. Доступ к сервисам

После развертывания сервисы будут доступны по адресам:

| Сервис | URL | Примечание |
|--------|-----|-----------|
| API | `https://<домен>/health` | Проверка статуса |
| Swagger | `https://<домен>/swagger` | API документация |
| Grafana | `https://<домен>/grafana/` | Пароль из .env |
| Prometheus | `https://<домен>/prometheus/` | Метрики |
| RabbitMQ | `https://<домен>/rabbitmq/` | Управление очередями |

## 11. Troubleshooting

### Контейнер не запускается
```bash
docker-compose logs <service_name>
```

### Проверка доступности БД
```bash
docker-compose exec postgres psql -U bank_user_prod -d bank_processing_prod -c "SELECT 1"
```

### Перезагрузка всего стека
```bash
docker-compose down
docker-compose up -d --build
```

### Очистка неиспользуемых ресурсов
```bash
docker system prune -a --volumes
```

## 12. Безопасность

- ✅ Используйте strong пароли (автогенерируются в .env)
- ✅ Включите firewall: `ufw enable`
- ✅ Ограничьте SSH доступ: отключите root login
- ✅ Регулярно обновляйте образы Docker: `docker-compose pull && docker-compose up -d`
- ✅ Мониторьте логи на ошибки и подозрительную активность

## Контрольный список перед production

- [ ] Измените все default пароли в .env
- [ ] Настроен HTTPS сертификат
- [ ] Configured backup процесс
- [ ] Настроены DNS записи
- [ ] Включен firewall
- [ ] Проверены логи на ошибки
- [ ] Работает мониторинг (Prometheus, Grafana)
- [ ] Протестированы backup/restore процессы
