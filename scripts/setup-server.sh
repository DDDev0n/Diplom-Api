#!/bin/bash

# Bank API Server Setup Script
# Запустите на сервере: curl -fsSL https://raw.githubusercontent.com/.../setup.sh | bash

set -e

echo "=========================================="
echo "Bank API Server Setup"
echo "=========================================="

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

echo -e "${YELLOW}Step 1: System Update${NC}"
apt update && apt upgrade -y
apt install -y curl wget git build-essential ca-certificates gnupg lsb-release

echo -e "${YELLOW}Step 2: Docker Installation${NC}"

# Check if Docker is already installed
if ! command -v docker &> /dev/null; then
    curl -fsSL https://get.docker.com -o get-docker.sh
    sh get-docker.sh
    rm get-docker.sh
    echo -e "${GREEN}Docker installed${NC}"
else
    echo -e "${GREEN}Docker already installed${NC}"
fi

# Check if Docker Compose is already installed
if ! command -v docker-compose &> /dev/null; then
    curl -L "https://github.com/docker/compose/releases/latest/download/docker-compose-$(uname -s)-$(uname -m)" -o /usr/local/bin/docker-compose
    chmod +x /usr/local/bin/docker-compose
    echo -e "${GREEN}Docker Compose installed${NC}"
else
    echo -e "${GREEN}Docker Compose already installed${NC}"
fi

echo -e "${YELLOW}Step 3: Nginx Installation${NC}"
apt install -y nginx
systemctl enable nginx

echo -e "${YELLOW}Step 4: Certbot Installation (Let's Encrypt)${NC}"
apt install -y certbot python3-certbot-nginx

echo -e "${YELLOW}Step 5: Firewall Configuration${NC}"
apt install -y ufw
ufw default deny incoming
ufw default allow outgoing
ufw allow 22/tcp
ufw allow 80/tcp
ufw allow 443/tcp
echo "y" | ufw enable

echo -e "${YELLOW}Step 6: Create Application Directory${NC}"
mkdir -p /opt/bank-api
mkdir -p /opt/backups
cd /opt/bank-api

if [ ! -d ".git" ]; then
    echo "Enter Git repository URL:"
    read REPO_URL
    git clone "$REPO_URL" .
fi

echo -e "${YELLOW}Step 7: Generate Production .env${NC}"

# Generate secure passwords
POSTGRES_PASSWORD=$(openssl rand -base64 32)
RABBITMQ_PASSWORD=$(openssl rand -base64 20)
JWT_SECRET=$(openssl rand -base64 48)
GRAFANA_PASSWORD=$(openssl rand -base64 24)

cat > .env << EOF
# Database
POSTGRES_DB=bank_processing_prod
POSTGRES_USER=bank_user_prod
POSTGRES_PASSWORD=${POSTGRES_PASSWORD}
DATABASE_URL=postgres://bank_user_prod:${POSTGRES_PASSWORD}@postgres:5432/bank_processing_prod?sslmode=disable

# Cache & Queue
REDIS_URL=redis://redis:6379/0
RABBITMQ_URL=amqp://bank_user_prod:${RABBITMQ_PASSWORD}@rabbitmq:5672/

# JWT
JWT_SECRET=${JWT_SECRET}
JWT_ALGORITHM=HS256
ACCESS_TOKEN_EXPIRE_MINUTES=120

# External Processing Service
PROCESSING_SERVICE_URL=https://pay.projectl.ru
PROCESSING_LOGIN_PATH=/api/v1/auth/login
PROCESSING_PROCESS_PATH=/api/v1/payments/process
PROCESSING_AUTH_TOKEN=
PROCESSING_AUTH_USERNAME=tester
PROCESSING_AUTH_ROLE=USER
PROCESSING_INSECURE_SKIP_VERIFY=false

# Payments
PAYMENT_REVIEW_THRESHOLD=100000

# Grafana
GRAFANA_ADMIN_USER=admin
GRAFANA_ADMIN_PASSWORD=${GRAFANA_PASSWORD}
EOF

echo -e "${GREEN}Generated .env with secure passwords${NC}"
echo -e "${YELLOW}Save these credentials securely:${NC}"
echo "POSTGRES_PASSWORD: ${POSTGRES_PASSWORD}"
echo "RABBITMQ_PASSWORD: ${RABBITMQ_PASSWORD}"
echo "GRAFANA_PASSWORD: ${GRAFANA_PASSWORD}"

echo -e "${YELLOW}Step 8: Start Docker Services${NC}"
docker-compose up -d --build

echo -e "${YELLOW}Waiting for services to be healthy...${NC}"
sleep 10

docker-compose ps

echo -e "${YELLOW}Step 9: Configure Nginx${NC}"
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

server {
    listen 80;
    server_name _;
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
NGINX_EOF

ln -sf /etc/nginx/sites-available/bank-api /etc/nginx/sites-enabled/bank-api
rm -f /etc/nginx/sites-enabled/default

nginx -t && systemctl restart nginx
echo -e "${GREEN}Nginx configured${NC}"

echo -e "${YELLOW}Step 10: Create Backup Script${NC}"
cat > /opt/backups/backup.sh << 'BACKUP_EOF'
#!/bin/bash
set -e

BACKUP_DIR="/opt/backups"
DATE=$(date +%Y%m%d_%H%M%S)

cd /opt/bank-api

# Backup PostgreSQL
docker-compose exec -T postgres pg_dump -U bank_user_prod bank_processing_prod | gzip > "${BACKUP_DIR}/db-${DATE}.sql.gz"

# Backup volumes
tar -czf "${BACKUP_DIR}/volumes-${DATE}.tar.gz" -C /opt/bank-api . --exclude='.git' --exclude='node_modules' --exclude='__pycache__'

echo "Backup completed: ${DATE}"

# Keep only last 7 backups
find ${BACKUP_DIR} -name "db-*.sql.gz" -mtime +7 -delete
find ${BACKUP_DIR} -name "volumes-*.tar.gz" -mtime +7 -delete
BACKUP_EOF

chmod +x /opt/backups/backup.sh

# Add to crontab (daily at 2 AM)
(crontab -l 2>/dev/null; echo "0 2 * * * /opt/backups/backup.sh") | crontab -

echo -e "${GREEN}Backup script created and scheduled${NC}"

echo -e "${YELLOW}Step 11: Setup SSL Certificate${NC}"
echo "Enter your domain name (e.g., example.com):"
read DOMAIN

if [ ! -z "$DOMAIN" ]; then
    certbot certonly --standalone -d $DOMAIN -d www.$DOMAIN --non-interactive --agree-tos --email admin@$DOMAIN
    
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

server {
    listen 80;
    server_name _;
    return 301 https://\$host\$request_uri;
}

server {
    listen 443 ssl http2;
    server_name _;

    ssl_certificate /etc/letsencrypt/live/${DOMAIN}/fullchain.pem;
    ssl_certificate_key /etc/letsencrypt/live/${DOMAIN}/privkey.pem;
    ssl_protocols TLSv1.2 TLSv1.3;
    ssl_ciphers HIGH:!aNULL:!MD5;
    ssl_prefer_server_ciphers on;

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
}
NGINX_HTTPS_EOF

    nginx -t && systemctl restart nginx
    systemctl enable certbot.timer
    systemctl start certbot.timer
    
    echo -e "${GREEN}SSL certificate installed and auto-renewal enabled${NC}"
fi

echo ""
echo "=========================================="
echo -e "${GREEN}Installation Complete!${NC}"
echo "=========================================="
echo ""
echo "Application URL: https://$DOMAIN"
echo "Grafana: https://$DOMAIN/grafana (user: admin)"
echo "Prometheus: https://$DOMAIN/prometheus"
echo ""
echo "API Health Check:"
docker-compose exec api curl -s http://localhost:8000/health || echo "Checking..."
echo ""
echo "Full documentation: /opt/bank-api/DEPLOYMENT.md"
echo ""
