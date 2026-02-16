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