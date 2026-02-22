## Project Structure

```text
ai-paper-evaluator/
├── cmd/
│   ├── gateway/
│   │   └── main.go
│   ├── upload-service/
│   │   └── main.go
│   ├── grading-service/
│   │   └── main.go
│   └── results-service/
│       └── main.go
│
├── internal/
│   ├── models/
│   │   └── models.go
│   │
│   ├── database/
│   │   ├── db.go              # PGX connection
│   │
│   ├── auth/
│   │   ├── jwt.go              # JWT token handling
│   │   ├── password.go         # Password hashing
│   │   ├── repository.go       # Database ops
│   │   └── middleware.go       # Auth middleware
│   │
│   ├── upload/
│   │   ├── service.go          # Business logic
│   │   └── repository.go       # Database operations
│   │
│   ├── grading/
│   │   ├── handler.go
│   │   ├── service.go
│   │   ├── repository.go
│   │   └── openai.go           # OpenAI client
│   │
│   ├── results/
│   │   ├── handler.go
│   │   ├── service.go
│   │   └── repository.go
│   │
│   ├── s3/
│   │   └── client.go           # S3 operations
│   │
│   ├── kafka/
│   │   ├── producer.go         # Kafka producer
│   │   ├── consumer.go         # Kafka consumer
│   │   └── outbox.go           # Outbox pattern
│   │
│   └── pdf/
│       └── parser.go           # PDF text extraction
│
├── migrations/
│   ├── 001_schema.sql
│   └── 001_schema.down.sql
│
├── .env.example
├── .gitignore
├── docker-compose.yml
├── Makefile
├── go.mod
├── go.sum
└── README.md
```

## Results Service API

All `/results` endpoints are protected and must be called through the API gateway with a valid JWT.

### 1) List user results

```bash
curl -X GET "http://localhost:8080/results?limit=20&offset=0" \
  -H "Authorization: Bearer <ACCESS_TOKEN>"
```

Response shape:

```json
{
  "results": [
    {
      "submission": {
        "id": "...",
        "user_id": "...",
        "status": "graded",
        "created_at": "2026-02-23T12:00:00Z"
      },
      "grade": {
        "id": "...",
        "submission_id": "...",
        "score": 84,
        "feedback": "{\"overall_score\":84,\"summary\":\"...\",\"criteria\":[...]}"
      }
    }
  ],
  "count": 1
}
```

### 2) Get a single submission result

```bash
curl -X GET "http://localhost:8080/results/<submission_id>" \
  -H "Authorization: Bearer <ACCESS_TOKEN>"
```

Response shape:

```json
{
  "submission": {
    "id": "...",
    "user_id": "...",
    "status": "graded",
    "created_at": "2026-02-23T12:00:00Z"
  },
  "grade": {
    "id": "...",
    "submission_id": "...",
    "score": 84,
    "feedback": "{\"overall_score\":84,\"summary\":\"...\",\"criteria\":[...]}"
  }
}
```

If grading is still in progress, `submission.status` may be `uploaded` or `processing` and `grade` may be `null`.

## Quick Local Setup

### Prerequisites

- Go 1.25+
- Docker + Docker Compose
- AWS S3 bucket and credentials
- Google Vision API key
- OpenRouter API key

### 1) Configure environment

```bash
cp .env.example .env
```

Update `.env` with real values for:

- `DATABASE_URL`
- `AWS_REGION`, `AWS_ACCESS_KEY_ID`, `AWS_SECRET_ACCESS_KEY`, `S3_BUCKET_NAME`
- `GOOGLE_VISION_API_KEY`
- `OPENROUTER_API_KEY`
- `GLOBAL_GRADING_RUBRIC`

### 2) Start local infrastructure

```bash
docker compose up -d postgres zookeeper kafka
```

### 3) Run migrations

```bash
docker exec -i ai-grader-db psql -U ai-grader -d ai_grader < migrations/001_schema.sql
```

### 4) Start services (separate terminals)

```bash
go run ./cmd/upload
go run ./cmd/grader
go run ./cmd/results
go run ./cmd/api
```

### 5) Smoke check

```bash
curl http://localhost:8080/health
curl http://localhost:8081/health
curl http://localhost:8083/health
```

Then register/login via gateway, upload a PDF, and query `/results`.