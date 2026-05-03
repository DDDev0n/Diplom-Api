#!/bin/bash

# Bank API - Phase 2: Nginx & SSL Setup
# Запустите на сервере: bash scripts/setup-nginx-ssl.sh

set -e

# Colors
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
NC='\033[0m' # No Color

# Check if running as root
if [[ $EUID -ne 0 ]]; then
   echo -e "${RED}This script must be run as root${NC}"
   exit 1
fi

echo "=========================================="
echo "Bank API - Nginx & SSL Configuration"
echo "=========================================="
echo ""

# Get application directory
APP_DIR="${1:-.}"
cd "$APP_DIR" || { echo -e "${RED}ERROR: Cannot access $APP_DIR${NC}"; exit 1; }

if [ ! -f "docker-compose.yml" ]; then
    echo -e "${RED}ERROR: docker-compose.yml not found. Please run this script from application directory.${NC}"
    exit 1
fi

echo -e "${YELLOW}Step 1: Configure Nginx as Reverse Proxy${NC}"

# Backup existing Nginx config if it exists
if [ -f "/etc/nginx/sites-available/bank-api" ]; then
    cp /etc/nginx/sites-available/bank-api /etc/nginx/sites-available/bank-api.backup
    echo -e "${YELLOW}Backed up existing Nginx config${NC}"
fi

cat > /etc/nginx/sites-available/bank-api << 'NGINX_EOF'
upstream bank_api {
    server localhost:8000;
}

upstream grafana {
    server localhost:3001;
}

upstream prometheus {
    server localhost:9090;
}

upstream cadvisor {
    server localhost:8081;
}

# HTTP server - redirect to HTTPS or serve on IP-only
server {
    listen 80 default_server;
    listen [::]:80 default_server;
    server_name _;

    # If we have a domain, redirect to HTTPS
    # Otherwise serve HTTP
    location /.well-known/acme-challenge/ {
        # Allow Let's Encrypt validation
        root /var/www/certbot;
    }

    location / {
        # Check if SSL certificate exists
        # This is a temporary config that will be replaced if domain is provided
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

    location /cadvisor/ {
        proxy_pass http://cadvisor/;
        proxy_http_version 1.1;
        proxy_set_header Host $host;
        proxy_set_header X-Real-IP $remote_addr;
        proxy_set_header X-Forwarded-For $proxy_add_x_forwarded_for;
        proxy_set_header X-Forwarded-Proto $scheme;
    }
}
NGINX_EOF

# Remove default site if exists
rm -f /etc/nginx/sites-enabled/default

# Enable the config
ln -sf /etc/nginx/sites-available/bank-api /etc/nginx/sites-enabled/bank-api

# Test Nginx config
if nginx -t 2>&1 | grep -q "successful"; then
    echo -e "${GREEN}Nginx configuration valid${NC}"
    systemctl restart nginx
    echo -e "${GREEN}Nginx restarted${NC}"
else
    echo -e "${RED}Nginx configuration error. Rolling back...${NC}"
    if [ -f "/etc/nginx/sites-available/bank-api.backup" ]; then
        cp /etc/nginx/sites-available/bank-api.backup /etc/nginx/sites-available/bank-api
    fi
    exit 1
fi

echo ""
echo -e "${YELLOW}Step 2: Setup SSL Certificate (Optional)${NC}"
echo ""
echo "Do you have a domain name?"
echo "1) Yes, setup SSL with Let's Encrypt"
echo "2) No, use HTTP on IP address only"
echo ""
read -p "Choose option (1 or 2): " SSL_CHOICE

if [ "$SSL_CHOICE" = "1" ]; then
    read -p "Enter domain name (e.g., api.example.com): " DOMAIN
    
    if [ -z "$DOMAIN" ]; then
        echo -e "${RED}Domain cannot be empty${NC}"
        exit 1
    fi
    
    read -p "Enter email for Let's Encrypt notifications: " EMAIL
    
    if [ -z "$EMAIL" ]; then
        EMAIL="admin@$DOMAIN"
    fi
    
    echo -e "${YELLOW}Setting up SSL for: $DOMAIN${NC}"
    
    # Create certbot directories
    mkdir -p /var/www/certbot
    
    # Obtain certificate
    certbot certonly --webroot -w /var/www/certbot -d "$DOMAIN" -d "www.$DOMAIN" \
        --non-interactive --agree-tos --email "$EMAIL" --register-unsafely-without-email 2>/dev/null || {
        echo -e "${YELLOW}Could not auto-obtain certificate. Ensure domain DNS points to this server and firewall allows port 80.${NC}"
        read -p "Continue anyway? (y/n): " CONTINUE
        [ "$CONTINUE" != "y" ] && exit 1
    }
    
    # Update Nginx config for HTTPS
    cat > /etc/nginx/sites-available/bank-api << NGINX_HTTPS_EOF
upstream bank_api {
    server localhost:8000;
}

upstream grafana {
    server localhost:3001;
}

upstream prometheus {
    server localhost:9090;
}

upstream cadvisor {
    server localhost:8081;
}

# HTTP server - redirect to HTTPS
server {
    listen 80;
    listen [::]:80;
    server_name _;

    location /.well-known/acme-challenge/ {
        root /var/www/certbot;
    }

    location / {
        return 301 https://\$host\$request_uri;
    }
}

# HTTPS server
server {
    listen 443 ssl http2;
    listen [::]:443 ssl http2;
    server_name _;

    ssl_certificate /etc/letsencrypt/live/${DOMAIN}/fullchain.pem;
    ssl_certificate_key /etc/letsencrypt/live/${DOMAIN}/privkey.pem;
    
    # SSL configuration
    ssl_protocols TLSv1.2 TLSv1.3;
    ssl_ciphers HIGH:!aNULL:!MD5;
    ssl_prefer_server_ciphers on;
    ssl_session_cache shared:SSL:10m;
    ssl_session_timeout 10m;
    ssl_stapling on;
    ssl_stapling_verify on;
    ssl_trusted_certificate /etc/letsencrypt/live/${DOMAIN}/chain.pem;

    client_max_body_size 10M;

    location / {
        proxy_pass http://bank_api;
        proxy_http_version 1.1;
        proxy_set_header Upgrade \$http_upgrade;
        proxy_set_header Connection 'upgrade';
        proxy_set_header Host \$host;
        proxy_set_header X-Real-IP \$remote_addr;
        proxy_set_header X-Forwarded-For \$proxy_add_x_forwarded_for;
        proxy_set_header X-Forwarded-Proto \$scheme;
        proxy_cache_bypass \$http_upgrade;
        proxy_connect_timeout 60s;
        proxy_send_timeout 60s;
        proxy_read_timeout 60s;
    }

    location /grafana/ {
        proxy_pass http://grafana/;
        proxy_http_version 1.1;
        proxy_set_header Host \$host;
        proxy_set_header X-Real-IP \$remote_addr;
        proxy_set_header X-Forwarded-For \$proxy_add_x_forwarded_for;
        proxy_set_header X-Forwarded-Proto \$scheme;
    }

    location /prometheus/ {
        proxy_pass http://prometheus/;
        proxy_http_version 1.1;
        proxy_set_header Host \$host;
        proxy_set_header X-Real-IP \$remote_addr;
        proxy_set_header X-Forwarded-For \$proxy_add_x_forwarded_for;
        proxy_set_header X-Forwarded-Proto \$scheme;
    }

    location /cadvisor/ {
        proxy_pass http://cadvisor/;
        proxy_http_version 1.1;
        proxy_set_header Host \$host;
        proxy_set_header X-Real-IP \$remote_addr;
        proxy_set_header X-Forwarded-For \$proxy_add_x_forwarded_for;
        proxy_set_header X-Forwarded-Proto \$scheme;
    }
}
NGINX_HTTPS_EOF

    nginx -t && systemctl restart nginx
    
    # Enable certbot auto-renewal
    systemctl enable certbot.timer
    systemctl start certbot.timer
    
    echo -e "${GREEN}SSL certificate installed and auto-renewal enabled${NC}"
    
else
    echo -e "${YELLOW}Using HTTP on IP address (no SSL)${NC}"
    echo -e "${YELLOW}Make sure firewall allows port 80${NC}"
fi

echo ""
echo -e "${YELLOW}Step 3: Create Backup Script${NC}"

mkdir -p /opt/backups

cat > /opt/backups/backup.sh << 'BACKUP_EOF'
#!/bin/bash
set -e

BACKUP_DIR="/opt/backups"
DATE=$(date +%Y%m%d_%H%M%S)
LOG_FILE="${BACKUP_DIR}/backup.log"

echo "[${DATE}] Starting backup..." >> "$LOG_FILE"

cd /opt/bank-api

# Backup PostgreSQL
echo "[${DATE}] Backing up PostgreSQL..." >> "$LOG_FILE"
if docker-compose exec -T postgres pg_dump -U bank_user_prod bank_processing_prod 2>/dev/null | gzip > "${BACKUP_DIR}/db-${DATE}.sql.gz"; then
    DB_SIZE=$(du -h "${BACKUP_DIR}/db-${DATE}.sql.gz" | cut -f1)
    echo "[${DATE}] PostgreSQL backup completed: ${DB_SIZE}" >> "$LOG_FILE"
else
    echo "[${DATE}] PostgreSQL backup failed" >> "$LOG_FILE"
fi

# Backup application data
echo "[${DATE}] Backing up application data..." >> "$LOG_FILE"
if tar -czf "${BACKUP_DIR}/app-data-${DATE}.tar.gz" \
    --exclude='.git' \
    --exclude='node_modules' \
    --exclude='__pycache__' \
    --exclude='.docker' \
    . 2>/dev/null; then
    APP_SIZE=$(du -h "${BACKUP_DIR}/app-data-${DATE}.tar.gz" | cut -f1)
    echo "[${DATE}] Application data backup completed: ${APP_SIZE}" >> "$LOG_FILE"
else
    echo "[${DATE}] Application data backup failed" >> "$LOG_FILE"
fi

# Backup Nginx/SSL config
echo "[${DATE}] Backing up Nginx configuration..." >> "$LOG_FILE"
tar -czf "${BACKUP_DIR}/nginx-ssl-${DATE}.tar.gz" /etc/nginx /etc/letsencrypt 2>/dev/null || true

# Cleanup old backups (keep only 7 days)
echo "[${DATE}] Cleaning up old backups..." >> "$LOG_FILE"
find ${BACKUP_DIR} -name "db-*.sql.gz" -mtime +7 -delete
find ${BACKUP_DIR} -name "app-data-*.tar.gz" -mtime +7 -delete
find ${BACKUP_DIR} -name "nginx-ssl-*.tar.gz" -mtime +7 -delete

echo "[${DATE}] Backup completed successfully" >> "$LOG_FILE"
BACKUP_EOF

chmod +x /opt/backups/backup.sh

# Add to crontab (daily at 2 AM)
CRON_JOB="0 2 * * * /opt/backups/backup.sh"
(crontab -l 2>/dev/null | grep -v "backup.sh"; echo "$CRON_JOB") | crontab -

echo -e "${GREEN}Backup script created and scheduled (daily at 2 AM)${NC}"

echo ""
echo "=========================================="
echo -e "${GREEN}Configuration Complete!${NC}"
echo "=========================================="
echo ""

# Get server IP
SERVER_IP=$(hostname -I | awk '{print $1}')

if [ "$SSL_CHOICE" = "1" ]; then
    echo "API URL: https://$DOMAIN"
    echo "Grafana: https://$DOMAIN/grafana"
    echo "Prometheus: https://$DOMAIN/prometheus"
    echo "cAdvisor: https://$DOMAIN/cadvisor"
else
    echo "API URL: http://$SERVER_IP"
    echo "Grafana: http://$SERVER_IP/grafana"
    echo "Prometheus: http://$SERVER_IP/prometheus"
    echo "cAdvisor: http://$SERVER_IP/cadvisor"
fi

echo ""
echo "Service Status:"
docker-compose ps
echo ""
echo "Recent logs:"
docker-compose logs -n 20 api | tail -10
echo ""
