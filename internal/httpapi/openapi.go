package httpapi

const swaggerHTML = `<!doctype html>
<html lang="ru">
<head>
  <meta charset="utf-8">
  <title>Bank Processing API Swagger</title>
  <link rel="stylesheet" href="https://unpkg.com/swagger-ui-dist@5/swagger-ui.css">
  <style>
    body { margin: 0; background: #f7f7f7; }
    .swagger-ui .topbar { display: none; }
  </style>
</head>
<body>
  <div id="swagger-ui"></div>
  <script src="https://unpkg.com/swagger-ui-dist@5/swagger-ui-bundle.js"></script>
  <script>
    window.ui = SwaggerUIBundle({
      url: "/openapi.json",
      dom_id: "#swagger-ui",
      deepLinking: true,
      persistAuthorization: true,
      displayRequestDuration: true
    });
  </script>
</body>
</html>`

const openapiSpec = `{
  "openapi": "3.0.3",
  "info": {
    "title": "Bank Processing Data Subsystem API",
    "version": "0.1.0",
    "description": "Go API для авторизации, создания платежей, очереди банкира и интеграции worker с внешним processing."
  },
  "servers": [
    {
      "url": "http://localhost:8000",
      "description": "Local Docker Compose API"
    }
  ],
  "tags": [
    { "name": "Health" },
    { "name": "Monitoring" },
    { "name": "Auth" },
    { "name": "Payments" },
    { "name": "Banker" },
    { "name": "Admin" }
  ],
  "paths": {
    "/health": {
      "get": {
        "tags": ["Health"],
        "summary": "Проверка доступности API",
        "responses": {
          "200": {
            "description": "API работает",
            "content": {
              "application/json": {
                "schema": { "$ref": "#/components/schemas/HealthResponse" },
                "example": { "status": "ok" }
              }
            }
          }
        }
      }
    },
    "/metrics": {
      "get": {
        "tags": ["Monitoring"],
        "summary": "Prometheus metrics",
        "responses": {
          "200": {
            "description": "Метрики API в Prometheus text format",
            "content": {
              "text/plain": {
                "schema": { "type": "string" }
              }
            }
          }
        }
      }
    },
    "/api/auth/register": {
      "post": {
        "tags": ["Auth"],
        "summary": "Регистрация клиента",
        "description": "Публичная регистрация создает только CLIENT. Первый ADMIN может быть создан этим endpoint для bootstrap, затем админы и банкиры создаются через /api/admin/users.",
        "requestBody": {
          "required": true,
          "content": {
            "application/json": {
              "schema": { "$ref": "#/components/schemas/RegisterRequest" },
              "examples": {
                "client": {
                  "summary": "Клиент",
                  "value": {
                    "email": "client@test.ru",
                    "password": "123456",
                    "full_name": "Клиент Тест",
                    "phone": "+79990000001",
                    "role": "CLIENT",
                    "balance": 50000000
                  }
                }
              }
            }
          }
        },
        "responses": {
          "201": {
            "description": "Пользователь создан",
            "content": {
              "application/json": {
                "schema": { "$ref": "#/components/schemas/AuthResponse" }
              }
            }
          },
          "400": { "$ref": "#/components/responses/BadRequest" },
          "409": { "$ref": "#/components/responses/Conflict" }
        }
      }
    },
    "/api/auth/login": {
      "post": {
        "tags": ["Auth"],
        "summary": "Вход пользователя",
        "requestBody": {
          "required": true,
          "content": {
            "application/json": {
              "schema": { "$ref": "#/components/schemas/LoginRequest" },
              "example": {
                "email": "client@test.ru",
                "password": "123456"
              }
            }
          }
        },
        "responses": {
          "200": {
            "description": "Успешный вход",
            "content": {
              "application/json": {
                "schema": { "$ref": "#/components/schemas/AuthResponse" }
              }
            }
          },
          "401": { "$ref": "#/components/responses/Unauthorized" }
        }
      }
    },
    "/api/auth/me": {
      "get": {
        "tags": ["Auth"],
        "summary": "Текущий пользователь",
        "security": [{ "bearerAuth": [] }],
        "responses": {
          "200": {
            "description": "Профиль пользователя",
            "content": {
              "application/json": {
                "schema": { "$ref": "#/components/schemas/User" }
              }
            }
          },
          "401": { "$ref": "#/components/responses/Unauthorized" }
        }
      }
    },
    "/api/payments": {
      "post": {
        "tags": ["Payments"],
        "summary": "Создать платеж",
        "description": "Создает платеж в Go API и отправляет задачу в RabbitMQ. Worker затем вызывает внешний processing по HTTPS.",
        "security": [{ "bearerAuth": [] }],
        "requestBody": {
          "required": true,
          "content": {
            "application/json": {
              "schema": { "$ref": "#/components/schemas/CreatePaymentRequest" },
              "example": {
                "recipient_id": 1,
                "amount": 15000000,
                "description": "Тестовый платеж",
                "payment_type": "SINGLE"
              }
            }
          }
        },
        "responses": {
          "201": {
            "description": "Платеж создан",
            "content": {
              "application/json": {
                "schema": { "$ref": "#/components/schemas/Payment" }
              }
            }
          },
          "400": { "$ref": "#/components/responses/BadRequest" },
          "401": { "$ref": "#/components/responses/Unauthorized" }
        }
      },
      "get": {
        "tags": ["Payments"],
        "summary": "Список платежей",
        "security": [{ "bearerAuth": [] }],
        "responses": {
          "200": {
            "description": "Список платежей",
            "content": {
              "application/json": {
                "schema": {
                  "type": "array",
                  "items": { "$ref": "#/components/schemas/Payment" }
                }
              }
            }
          },
          "401": { "$ref": "#/components/responses/Unauthorized" }
        }
      }
    },
    "/api/payments/{id}": {
      "get": {
        "tags": ["Payments"],
        "summary": "Получить платеж по ID",
        "security": [{ "bearerAuth": [] }],
        "parameters": [
          { "$ref": "#/components/parameters/PathID" }
        ],
        "responses": {
          "200": {
            "description": "Платеж",
            "content": {
              "application/json": {
                "schema": { "$ref": "#/components/schemas/Payment" }
              }
            }
          },
          "401": { "$ref": "#/components/responses/Unauthorized" },
          "403": { "$ref": "#/components/responses/Forbidden" },
          "404": { "$ref": "#/components/responses/NotFound" }
        }
      }
    },
    "/api/banker/queue": {
      "get": {
        "tags": ["Banker"],
        "summary": "Очередь платежей на проверку",
        "security": [{ "bearerAuth": [] }],
        "responses": {
          "200": {
            "description": "Платежи со статусом PENDING",
            "content": {
              "application/json": {
                "schema": {
                  "type": "array",
                  "items": { "$ref": "#/components/schemas/Payment" }
                }
              }
            }
          },
          "401": { "$ref": "#/components/responses/Unauthorized" },
          "403": { "$ref": "#/components/responses/Forbidden" }
        }
      }
    },
    "/api/banker/approve/{id}": {
      "post": {
        "tags": ["Banker"],
        "summary": "Одобрить платеж",
        "security": [{ "bearerAuth": [] }],
        "parameters": [
          { "$ref": "#/components/parameters/PathID" }
        ],
        "responses": {
          "200": {
            "description": "Платеж одобрен",
            "content": {
              "application/json": {
                "schema": { "$ref": "#/components/schemas/StatusResponse" },
                "example": { "status": "APPROVED" }
              }
            }
          },
          "400": { "$ref": "#/components/responses/BadRequest" },
          "401": { "$ref": "#/components/responses/Unauthorized" },
          "403": { "$ref": "#/components/responses/Forbidden" }
        }
      }
    },
    "/api/banker/reject/{id}": {
      "post": {
        "tags": ["Banker"],
        "summary": "Отклонить платеж",
        "security": [{ "bearerAuth": [] }],
        "parameters": [
          { "$ref": "#/components/parameters/PathID" }
        ],
        "requestBody": {
          "required": false,
          "content": {
            "application/json": {
              "schema": { "$ref": "#/components/schemas/RejectRequest" },
              "example": { "reason": "Подозрительная операция" }
            }
          }
        },
        "responses": {
          "200": {
            "description": "Платеж отклонен",
            "content": {
              "application/json": {
                "schema": { "$ref": "#/components/schemas/StatusResponse" },
                "example": { "status": "REJECTED" }
              }
            }
          },
          "400": { "$ref": "#/components/responses/BadRequest" },
          "401": { "$ref": "#/components/responses/Unauthorized" },
          "403": { "$ref": "#/components/responses/Forbidden" }
        }
      }
    },
    "/api/admin/users": {
      "get": {
        "tags": ["Admin"],
        "summary": "Список пользователей",
        "security": [{ "bearerAuth": [] }],
        "parameters": [
          { "name": "q", "in": "query", "schema": { "type": "string" }, "description": "Поиск по email, ФИО, телефону или ID" },
          { "name": "role", "in": "query", "schema": { "type": "string", "enum": ["CLIENT", "BANKER", "ADMIN"] } },
          { "name": "limit", "in": "query", "schema": { "type": "integer", "default": 100 } }
        ],
        "responses": {
          "200": {
            "description": "Пользователи",
            "content": {
              "application/json": {
                "schema": {
                  "type": "array",
                  "items": { "$ref": "#/components/schemas/User" }
                }
              }
            }
          },
          "403": { "$ref": "#/components/responses/Forbidden" }
        }
      },
      "post": {
        "tags": ["Admin"],
        "summary": "Создать клиента, банкира или администратора",
        "security": [{ "bearerAuth": [] }],
        "requestBody": {
          "required": true,
          "content": {
            "application/json": {
              "schema": { "$ref": "#/components/schemas/RegisterRequest" }
            }
          }
        },
        "responses": {
          "201": {
            "description": "Пользователь создан",
            "content": {
              "application/json": {
                "schema": { "$ref": "#/components/schemas/User" }
              }
            }
          },
          "400": { "$ref": "#/components/responses/BadRequest" },
          "403": { "$ref": "#/components/responses/Forbidden" },
          "409": { "$ref": "#/components/responses/Conflict" }
        }
      }
    },
    "/api/banker/clients": {
      "get": {
        "tags": ["Banker"],
        "summary": "Поиск клиентов",
        "security": [{ "bearerAuth": [] }],
        "parameters": [
          { "name": "q", "in": "query", "schema": { "type": "string" }, "description": "Поиск по email, ФИО, телефону или ID" },
          { "name": "limit", "in": "query", "schema": { "type": "integer", "default": 100 } }
        ],
        "responses": {
          "200": {
            "description": "Клиенты",
            "content": {
              "application/json": {
                "schema": {
                  "type": "array",
                  "items": { "$ref": "#/components/schemas/User" }
                }
              }
            }
          },
          "403": { "$ref": "#/components/responses/Forbidden" }
        }
      }
    },
    "/api/banker/clients/{id}": {
      "get": {
        "tags": ["Banker"],
        "summary": "Карточка клиента с платежами и статистикой",
        "security": [{ "bearerAuth": [] }],
        "parameters": [{ "$ref": "#/components/parameters/PathID" }],
        "responses": {
          "200": {
            "description": "Клиентская карточка",
            "content": {
              "application/json": {
                "schema": { "$ref": "#/components/schemas/ClientProfile" }
              }
            }
          },
          "403": { "$ref": "#/components/responses/Forbidden" },
          "404": { "$ref": "#/components/responses/NotFound" }
        }
      }
    },
    "/api/banker/stats": {
      "get": {
        "tags": ["Banker"],
        "summary": "Статистика текущего банкира по решениям",
        "security": [{ "bearerAuth": [] }],
        "responses": {
          "200": {
            "description": "Статистика",
            "content": {
              "application/json": {
                "schema": { "$ref": "#/components/schemas/BankerStats" }
              }
            }
          },
          "403": { "$ref": "#/components/responses/Forbidden" }
        }
      }
    },
    "/api/banker/history": {
      "get": {
        "tags": ["Banker"],
        "summary": "История решений текущего банкира",
        "security": [{ "bearerAuth": [] }],
        "responses": {
          "200": {
            "description": "Платежи, по которым банкир принял решение",
            "content": {
              "application/json": {
                "schema": {
                  "type": "array",
                  "items": { "$ref": "#/components/schemas/Payment" }
                }
              }
            }
          },
          "403": { "$ref": "#/components/responses/Forbidden" }
        }
      }
    },
    "/api/admin/stats": {
      "get": {
        "tags": ["Admin"],
        "summary": "Общая статистика платежей и решений банкиров",
        "security": [{ "bearerAuth": [] }],
        "responses": {
          "200": {
            "description": "Статистика админки",
            "content": {
              "application/json": {
                "schema": { "type": "object" }
              }
            }
          },
          "403": { "$ref": "#/components/responses/Forbidden" }
        }
      }
    },
    "/api/admin/audit": {
      "get": {
        "tags": ["Admin"],
        "summary": "Последние записи audit log",
        "security": [{ "bearerAuth": [] }],
        "responses": {
          "200": {
            "description": "Audit log",
            "content": {
              "application/json": {
                "schema": {
                  "type": "array",
                  "items": { "$ref": "#/components/schemas/AuditEntry" }
                }
              }
            }
          },
          "403": { "$ref": "#/components/responses/Forbidden" }
        }
      }
    },
    "/api/admin/bankers/{id}/history": {
      "get": {
        "tags": ["Admin"],
        "summary": "История решений конкретного банкира",
        "security": [{ "bearerAuth": [] }],
        "parameters": [{ "$ref": "#/components/parameters/PathID" }],
        "responses": {
          "200": {
            "description": "Решения банкира",
            "content": {
              "application/json": {
                "schema": {
                  "type": "array",
                  "items": { "$ref": "#/components/schemas/Payment" }
                }
              }
            }
          },
          "403": { "$ref": "#/components/responses/Forbidden" }
        }
      }
    },
    "/api/admin/clients/{id}/history": {
      "get": {
        "tags": ["Admin"],
        "summary": "История клиента: профиль, платежи и audit",
        "security": [{ "bearerAuth": [] }],
        "parameters": [{ "$ref": "#/components/parameters/PathID" }],
        "responses": {
          "200": {
            "description": "История клиента",
            "content": {
              "application/json": {
                "schema": { "type": "object" }
              }
            }
          },
          "403": { "$ref": "#/components/responses/Forbidden" }
        }
      }
    }
  },
  "components": {
    "securitySchemes": {
      "bearerAuth": {
        "type": "http",
        "scheme": "bearer",
        "bearerFormat": "JWT"
      }
    },
    "parameters": {
      "PathID": {
        "name": "id",
        "in": "path",
        "required": true,
        "schema": {
          "type": "integer",
          "format": "int64",
          "minimum": 1
        }
      }
    },
    "responses": {
      "BadRequest": {
        "description": "Некорректный запрос",
        "content": {
          "application/json": {
            "schema": { "$ref": "#/components/schemas/ErrorResponse" }
          }
        }
      },
      "Unauthorized": {
        "description": "Нет или неверный Bearer JWT",
        "content": {
          "application/json": {
            "schema": { "$ref": "#/components/schemas/ErrorResponse" }
          }
        }
      },
      "Forbidden": {
        "description": "Недостаточно прав",
        "content": {
          "application/json": {
            "schema": { "$ref": "#/components/schemas/ErrorResponse" }
          }
        }
      },
      "NotFound": {
        "description": "Объект не найден",
        "content": {
          "application/json": {
            "schema": { "$ref": "#/components/schemas/ErrorResponse" }
          }
        }
      },
      "Conflict": {
        "description": "Конфликт данных",
        "content": {
          "application/json": {
            "schema": { "$ref": "#/components/schemas/ErrorResponse" }
          }
        }
      }
    },
    "schemas": {
      "HealthResponse": {
        "type": "object",
        "properties": {
          "status": { "type": "string", "example": "ok" }
        }
      },
      "RegisterRequest": {
        "type": "object",
        "required": ["email", "password", "full_name"],
        "properties": {
          "email": { "type": "string", "format": "email" },
          "password": { "type": "string", "minLength": 1 },
          "full_name": { "type": "string" },
          "phone": { "type": "string" },
          "role": {
            "type": "string",
            "enum": ["CLIENT", "BANKER", "ADMIN"],
            "default": "CLIENT"
          },
          "balance": {
            "type": "integer",
            "format": "int64",
            "description": "Баланс в копейках"
          }
        }
      },
      "LoginRequest": {
        "type": "object",
        "required": ["email", "password"],
        "properties": {
          "email": { "type": "string", "format": "email" },
          "password": { "type": "string" }
        }
      },
      "AuthResponse": {
        "type": "object",
        "properties": {
          "token": { "type": "string" },
          "user": { "$ref": "#/components/schemas/User" }
        }
      },
      "User": {
        "type": "object",
        "properties": {
          "id": { "type": "integer", "format": "int64" },
          "email": { "type": "string" },
          "full_name": { "type": "string" },
          "phone": { "type": "string" },
          "role": { "type": "string", "enum": ["CLIENT", "BANKER", "ADMIN"] },
          "balance": { "type": "integer", "format": "int64" },
          "daily_limit": { "type": "integer", "format": "int64" },
          "monthly_limit": { "type": "integer", "format": "int64" },
          "is_blocked": { "type": "boolean" },
          "created_at": { "type": "string", "format": "date-time" }
        }
      },
      "CreatePaymentRequest": {
        "type": "object",
        "required": ["recipient_id", "amount"],
        "properties": {
          "recipient_id": { "type": "integer", "format": "int64" },
          "amount": {
            "type": "integer",
            "format": "int64",
            "minimum": 1,
            "description": "Сумма в копейках"
          },
          "description": { "type": "string" },
          "payment_type": {
            "type": "string",
            "enum": ["SINGLE", "RECURRING", "MASS_PAYOUT"],
            "default": "SINGLE"
          }
        }
      },
      "Payment": {
        "type": "object",
        "properties": {
          "id": { "type": "integer", "format": "int64" },
          "sender_id": { "type": "integer", "format": "int64" },
          "recipient_id": { "type": "integer", "format": "int64" },
          "amount": { "type": "integer", "format": "int64" },
          "commission": { "type": "integer", "format": "int64" },
          "status": {
            "type": "string",
            "enum": ["PENDING", "APPROVED", "REJECTED", "COMPLETED", "CANCELLED"]
          },
          "payment_type": { "type": "string" },
          "description": { "type": "string" },
          "fraud_score": { "type": "integer" },
          "approved_by": { "type": "integer", "format": "int64", "nullable": true },
          "rejection_reason": { "type": "string" },
          "created_at": { "type": "string", "format": "date-time" },
          "processed_at": { "type": "string", "format": "date-time", "nullable": true }
        }
      },
      "ClientStats": {
        "type": "object",
        "properties": {
          "sent_count": { "type": "integer", "format": "int64" },
          "received_count": { "type": "integer", "format": "int64" },
          "sent_amount": { "type": "integer", "format": "int64" },
          "received_amount": { "type": "integer", "format": "int64" },
          "pending_payments": { "type": "integer", "format": "int64" },
          "approved_payments": { "type": "integer", "format": "int64" },
          "rejected_payments": { "type": "integer", "format": "int64" }
        }
      },
      "ClientProfile": {
        "type": "object",
        "properties": {
          "user": { "$ref": "#/components/schemas/User" },
          "stats": { "$ref": "#/components/schemas/ClientStats" },
          "payments": {
            "type": "array",
            "items": { "$ref": "#/components/schemas/Payment" }
          }
        }
      },
      "BankerStats": {
        "type": "object",
        "properties": {
          "banker_id": { "type": "integer", "format": "int64" },
          "approved": { "type": "integer", "format": "int64" },
          "rejected": { "type": "integer", "format": "int64" },
          "total": { "type": "integer", "format": "int64" },
          "last_decision": { "type": "string", "format": "date-time", "nullable": true }
        }
      },
      "AuditEntry": {
        "type": "object",
        "properties": {
          "id": { "type": "integer", "format": "int64" },
          "user_id": { "type": "integer", "format": "int64", "nullable": true },
          "action": { "type": "string" },
          "entity_type": { "type": "string" },
          "entity_id": { "type": "integer", "format": "int64", "nullable": true },
          "details": { "type": "string" },
          "ip_address": { "type": "string" },
          "created_at": { "type": "string", "format": "date-time" }
        }
      },
      "RejectRequest": {
        "type": "object",
        "properties": {
          "reason": { "type": "string" }
        }
      },
      "StatusResponse": {
        "type": "object",
        "properties": {
          "status": { "type": "string" }
        }
      },
      "MessageResponse": {
        "type": "object",
        "properties": {
          "message": { "type": "string" }
        }
      },
      "ErrorResponse": {
        "type": "object",
        "properties": {
          "error": { "type": "string" }
        }
      }
    }
  }
}`
