# Быстрый чек-лист развертывания

## Перед покупкой сервера

- [ ] **Конфигурация сервера:**
  - [ ] CPU: минимум 2 ядра (рекомендуется 4)
  - [ ] RAM: минимум 4 GB (рекомендуется 8)
  - [ ] Storage: минимум 30 GB SSD (рекомендуется 50+)
  - [ ] ОС: Ubuntu 22.04 LTS

- [ ] **Сеть:**
  - [ ] Получить IP адрес сервера
  - [ ] Зарегистрировать домен
  - [ ] Настроить DNS A-запись на IP сервера

## День развертывания

### Шаг 1: Первоначальная настройка (5 минут)

1. Подключитесь по SSH к серверу:
   ```bash
   ssh root@<IP_СЕРВЕРА>
   ```

2. Проверьте, что Ubuntu 22.04:
   ```bash
   lsb_release -a
   ```

### Шаг 2: Автоматическое развертывание (15 минут)

1. Скачайте и запустите скрипт установки:
   ```bash
   curl -fsSL https://raw.githubusercontent.com/<OWNER>/<REPO>/main/scripts/setup-server.sh | bash
   ```

2. Когда скрипт попросит домен - введите ваш домен (например: `api.example.com`)

3. Дождитесь завершения (будут выведены пароли - сохраните их!)

### Шаг 3: Проверка (5 минут)

1. **Проверьте API здоровье:**
   ```bash
   curl https://<домен>/health
   ```
   
   Результат:
   ```json
   {"status": "ok"}
   ```

2. **Проверьте контейнеры:**
   ```bash
   ssh root@<IP> "cd /opt/bank-api && docker-compose ps"
   ```
   
   Все контейнеры должны быть в статусе `Up`

3. **Откройте в браузере:**
   - API: `https://<домен>/health`
   - Swagger: `https://<домен>/swagger`
   - Grafana: `https://<домен>/grafana`
   - Prometheus: `https://<домен>/prometheus`

### Шаг 4: Финальные настройки (10 минут)

1. **Логин в Grafana:**
   - URL: `https://<домен>/grafana`
   - User: `admin`
   - Password: (выведен после установки, сохраните!)
   - ✅ Измените пароль: `admin` -> `Administration` -> `Users` -> `admin` -> `Change password`

2. **Добавьте Prometheus datasource:**
   - Grafana -> Connections -> Data sources
   - Add data source -> Prometheus
   - URL: `http://prometheus:9090`
   - Save

3. **Импортируйте дашборды:**
   - Grafana -> Dashboards -> Import
   - Upload JSON file -> выберите `bank-api.json` из проекта
   - или просто откройте готовый дашборд (должен быть auto-provisioned)

## Текущее состояние

### Сервисы и порты

| Сервис | Внутренний порт | Внешний URL |
|--------|----------------|------------|
| API | 8000 | `https://<домен>/` |
| Swagger | 8000 | `https://<домен>/swagger` |
| PostgreSQL | 5432 | Внутренний только |
| Redis | 6379 | Внутренний только |
| RabbitMQ Management | 15672 | `https://<домен>/rabbitmq` |
| Prometheus | 9090 | `https://<домен>/prometheus` |
| Grafana | 3001 | `https://<домен>/grafana` |
| postgres-exporter | 9187 | Внутренний только |
| node-exporter | 9100 | Внутренний только |
| cadvisor | 8081 | Внутренний только |

### Важные пути на сервере

```
/opt/bank-api/                    - основная папка приложения
/opt/bank-api/.env               - переменные окружения (НЕ коммитить!)
/opt/bank-api/docker-compose.yml - конфиг контейнеров
/opt/backups/                     - автоматические бэкапы БД
/etc/nginx/sites-available/      - конфиги Nginx
/etc/letsencrypt/                - SSL сертификаты
```

## Повседневные команды

### Просмотр логов
```bash
ssh root@<IP> "cd /opt/bank-api && docker-compose logs -f api"
ssh root@<IP> "cd /opt/bank-api && docker-compose logs -f worker"
```

### Перезагрузить сервис
```bash
ssh root@<IP> "cd /opt/bank-api && docker-compose restart api"
ssh root@<IP> "cd /opt/bank-api && docker-compose restart worker"
```

### Обновить приложение
```bash
ssh root@<IP> << 'EOF'
cd /opt/bank-api
git pull
docker-compose up -d --build
EOF
```

### Посмотреть статус
```bash
ssh root@<IP> "cd /opt/bank-api && docker-compose ps"
```

### Просмотр бэкапов
```bash
ssh root@<IP> "ls -lh /opt/backups/"
```

## Troubleshooting

### "Connection refused" - сервис не запустился
```bash
ssh root@<IP> "cd /opt/bank-api && docker-compose logs api"
# Проверьте переменные в .env
# Проверьте, есть ли достаточно памяти
```

### "Port already in use"
```bash
# Посмотрите какие порты заняты
ssh root@<IP> "sudo netstat -tlnp | grep LISTEN"
# Измените порты в docker-compose.yml
```

### БД не отвечает
```bash
ssh root@<IP> "cd /opt/bank-api && docker-compose exec postgres pg_isready"
ssh root@<IP> "cd /opt/bank-api && docker-compose logs postgres"
```

### Нет данных в Grafana
```bash
# Проверьте metrics endpoint
curl https://<домен>/metrics

# Проверьте Prometheus health
curl https://<домен>/prometheus/api/v1/query?query=up
```

## Безопасность

- ✅ Все пароли автоматически генерируются (32+ символа)
- ✅ HTTPS включен автоматически (Let's Encrypt)
- ✅ Firewall включен (только 22, 80, 443)
- ✅ Бэкапы БД ежедневно (сохраняются 7 дней)
- ✅ Сертификат автоматически обновляется

### Дополнительные рекомендации

1. **SSH:**
   ```bash
   # Отключите root login
   ssh root@<IP> "sudo sed -i 's/#PermitRootLogin yes/PermitRootLogin no/' /etc/ssh/sshd_config"
   ssh root@<IP> "sudo systemctl restart sshd"
   ```

2. **Мониторинг**:
   - Настройте alerts в Prometheus/Grafana
   - Настройте notification channels (email, Slack, etc)

3. **Backup**:
   - Периодически скачивайте бэкапы локально:
     ```bash
     scp root@<IP>:/opt/backups/* ~/backups/
     ```

## Полезные ссылки

- Документация: [DEPLOYMENT.md](./DEPLOYMENT.md)
- Docker Compose: https://docs.docker.com/compose/
- Nginx: https://nginx.org/en/docs/
- Let's Encrypt: https://letsencrypt.org/docs/
- Prometheus: https://prometheus.io/docs/
- Grafana: https://grafana.com/docs/

## Поддержка

Если что-то не работает:

1. Проверьте логи: `docker-compose logs <service>`
2. Проверьте конфиги: `docker-compose config`
3. Перезагрузите стек: `docker-compose down && docker-compose up -d`
4. Проверьте ресурсы: `docker stats`
