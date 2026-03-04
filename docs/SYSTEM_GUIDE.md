# AI Grader — Complete System Guide

> A comprehensive end-to-end walkthrough of the AI Grader system, covering architecture, every component, design decisions, and interview-ready Q&A.

---

## Table of Contents

1. [What Does This System Do?](#1-what-does-this-system-do)
2. [High-Level Architecture Diagram](#2-high-level-architecture-diagram)
3. [Technology Stack & Why Each Was Chosen](#3-technology-stack--why-each-was-chosen)
4. [Microservices Breakdown](#4-microservices-breakdown)
5. [End-to-End Request Flow](#5-end-to-end-request-flow)
6. [Database Schema & Design](#6-database-schema--design)
7. [Authentication System Deep Dive](#7-authentication-system-deep-dive)
8. [Transactional Outbox Pattern](#8-transactional-outbox-pattern)
9. [Kafka Event-Driven Pipeline](#9-kafka-event-driven-pipeline)
10. [AI/LLM Grading Pipeline](#10-aillm-grading-pipeline)
11. [Frontend Architecture](#11-frontend-architecture)
12. [Docker & Deployment](#12-docker--deployment)
13. [Folder Structure Explained](#13-folder-structure-explained)
14. [Design Patterns Used](#14-design-patterns-used)
15. [Interview Questions & Answers](#15-interview-questions--answers)

---

## 1. What Does This System Do?

AI Grader is a **full-stack web application** that lets users:

1. **Register / Login** to get a personal account.
2. **Upload a PDF answer sheet** along with a grading rubric / answer scheme.
3. The system **asynchronously grades the paper using a Vision-LLM** (AI that can see images).
4. Users **view detailed results** — overall score, per-question breakdown, and feedback.

Think of it as an automated exam evaluator: you give it the student's paper + the expected answers, and it returns a detailed scorecard.

---

## 2. High-Level Architecture Diagram

```
┌─────────────────────────────────────────────────────────────────────────┐
│                          FRONTEND (React + Vite)                        │
│  Landing │ Register │ Login │ Upload │ Dashboard │ Result               │
└─────────────────────────────┬───────────────────────────────────────────┘
                              │ HTTP (JSON / multipart)
                              ▼
┌─────────────────────────────────────────────────────────────────────────┐
│                       API GATEWAY  (Go, port 8080)                      │
│                                                                         │
│  • Public routes: /auth/register, /auth/login, /auth/refresh            │
│  • JWT Auth Middleware (validates Bearer token)                          │
│  • Reverse-proxy → Upload Service  (/upload/*)                          │
│  • Reverse-proxy → Results Service (/results/*)                         │
│  • Injects X-User-ID header into proxied requests                       │
└────────┬───────────────────────────────────┬────────────────────────────┘
         │                                   │
         ▼                                   ▼
┌────────────────────┐            ┌────────────────────────┐
│  UPLOAD SERVICE    │            │  RESULTS SERVICE       │
│  (Go, port 8081)   │            │  (Go, port 8083)       │
│                    │            │                        │
│  • Validate PDF    │            │  • List submissions    │
│  • Upload to S3    │            │  • Get submission +    │
│  • Create          │            │    grade details       │
│    submission +    │            │  • Pagination          │
│    outbox event    │            └────────────┬───────────┘
│    (in one TX)     │                         │
│  • Outbox          │                         │ SQL query
│    Publisher →     │                         ▼
│    publishes to    │            ┌────────────────────────┐
│    Kafka           │            │    PostgreSQL 16       │
└────────┬───────────┘            │                        │
         │                        │  Tables:               │
         │ Kafka topic:           │  • users               │
         │ "paper-uploaded"       │  • submissions         │
         │                        │  • grades              │
         ▼                        │  • outbox_events       │
┌────────────────────┐            │  • refresh_tokens      │
│    Apache Kafka    │            └────────────────────────┘
│                    │                         ▲
│  Topics:           │                         │
│  • paper-uploaded  │                         │
│  • paper-graded    │                         │
└────────┬───────────┘                         │
         │                                     │
         │ Consumer                            │
         ▼                                     │
┌────────────────────────────────┐              │
│     GRADER SERVICE (Go)        │              │
│                                │              │
│  1. Consume paper-uploaded     │              │
│  2. Download PDF from S3       │──── S3 ────►│
│  3. Render pages → JPEG images │              │
│  4. Send images + rubric to    │              │
│     Vision-LLM (OpenRouter)    │──── API ──► OpenRouter
│  5. Parse structured feedback  │              │
│  6. Save grade + outbox event  │──────────────┘
│     (in one TX)                │
│  7. Outbox Publisher →         │
│     publishes to Kafka         │
│     "paper-graded" topic       │
└────────────────────────────────┘

                    ┌─────────────────┐
                    │   AWS S3        │
                    │                 │
                    │  Stores PDFs    │
                    │  AES-256        │
                    │  encryption     │
                    └─────────────────┘
```

### Simplified Flow Diagram

```
User ──► API Gateway ──► Upload Service ──► S3 + PostgreSQL + Outbox
                                                      │
                                              Outbox Publisher
                                                      │
                                                      ▼
                                                    Kafka
                                              "paper-uploaded"
                                                      │
                                                      ▼
                                              Grader Service
                                                      │
                                          ┌───────────┼───────────┐
                                          ▼           ▼           ▼
                                     S3 Download   PDF→JPEG   Vision-LLM
                                                                  │
                                                                  ▼
                                                          Save grade to DB
                                                                  │
                                                                  ▼
                                                          Outbox → Kafka
                                                        "paper-graded"
                                                                  │
User ◄── API Gateway ◄── Results Service ◄── PostgreSQL ◄─────────┘
```

---

## 3. Technology Stack & Why Each Was Chosen

### Backend

| Technology | Purpose | Why This? |
|---|---|---|
| **Go (Golang)** | All backend services | Fast compilation, tiny memory footprint, built-in concurrency (goroutines), excellent for microservices and I/O-heavy workloads. No JVM warm-up. Single binary deployment. |
| **PostgreSQL 16** | Primary database | ACID transactions (critical for outbox pattern), JSONB for flexible payloads, UUID support, mature ecosystem, free. |
| **Apache Kafka** | Event streaming / async messaging | Durable message log, consumer groups, replay capability, high throughput. Better than RabbitMQ for event sourcing and audit trails. |
| **AWS S3** | File storage | Infinitely scalable object storage, cheap, server-side encryption (AES-256), pre-signed URLs for secure access. |
| **OpenRouter API** | Vision-LLM for grading | Unified API gateway to multiple LLM providers (Grok, GPT, Claude, Qwen). If one model is down, switch via env var — zero code changes. |
| **go-fitz (MuPDF)** | PDF → image rendering | Native C library (fast), renders any PDF page to an image. Needed because the LLM uses *vision* (sees images) rather than OCR text extraction. |
| **bcrypt** | Password hashing | Industry standard, adaptive cost factor, resistant to rainbow tables and brute-force. |
| **JWT (HS256)** | Authentication tokens | Stateless auth — no session store needed. Short-lived access tokens (15 min) + long-lived refresh tokens (30 days). |
| **pgx/v5** | PostgreSQL driver | Fastest Go PostgreSQL driver, native connection pooling, context support, prepared statements. |
| **gorilla/mux** | HTTP router (gateway) | Path variables, middleware support, subrouters — more flexible than Go's `net/http` default mux. |
| **segmentio/kafka-go** | Kafka client | Pure Go implementation (no CGO dependency for Kafka), clean API, consumer groups built-in. |
| **godotenv** | Environment config | Loads `.env` file for local development. In production, real env vars take over seamlessly. |

### Frontend

| Technology | Purpose | Why This? |
|---|---|---|
| **React 18** | UI library | Component-based, huge ecosystem, easy state management with hooks. |
| **Vite** | Build tool / dev server | 10–100x faster than Webpack for HMR (Hot Module Replace). Native ES modules. |
| **React Router v6** | Client-side routing | Declarative routes, nested layouts, protected route pattern. |
| **Tailwind CSS** | Styling | Utility-first CSS — no separate CSS files, rapid prototyping, tiny production bundle (purges unused classes). |
| **Context API** | Auth state management | Simpler than Redux for single-concern global state (logged-in user). No extra dependency. |
| **Fetch API** | HTTP client | Native browser API — no Axios dependency needed. Simpler bundle. |

### Infrastructure

| Technology | Purpose | Why This? |
|---|---|---|
| **Docker Compose** | Local orchestration | Spin up Postgres + Kafka + Zookeeper + all services with one command. Reproducible environments. |
| **Multi-stage Dockerfile** | Grader container | Builder stage compiles Go binary with CGO (MuPDF). Runtime stage uses slim Debian — 10x smaller final image. |

---

## 4. Microservices Breakdown

### Why Microservices Instead of a Monolith?

```
Monolith Problem:
┌─────────────────────────────┐
│  Upload + Grade + Results   │  ← If grading takes 2 min,
│  all in ONE process         │     the upload endpoint is
│                             │     blocked / slow
└─────────────────────────────┘

Microservices Solution:
┌──────────┐  ┌──────────┐  ┌──────────┐
│  Upload   │  │  Grader  │  │  Results │  ← Each scales
│  (fast)   │  │  (slow,  │  │  (fast)  │     independently
│           │  │   heavy) │  │          │
└──────────┘  └──────────┘  └──────────┘
```

### Service Responsibilities

#### 1. API Gateway (`cmd/api/`)

- **Port**: 8080
- **Role**: Single entry point for all client requests
- **What it does**:
  - Handles authentication (register, login, refresh)
  - Validates JWT tokens via middleware
  - Reverse-proxies requests to Upload and Results services
  - Injects `X-User-ID` header so downstream services know who the user is
- **Why a gateway?**: Clients call ONE URL. Auth is centralized. Backend services don't need to know about JWT.

#### 2. Upload Service (`cmd/upload/`)

- **Port**: 8081
- **Role**: Accepts PDF uploads
- **What it does**:
  - Validates the request (PDF magic bytes, file size ≤ 25 MB)
  - Uploads the file to S3
  - Creates a `submission` row + an `outbox_events` row **in a single database transaction**
  - Runs an Outbox Publisher that polls `outbox_events` and publishes to Kafka
- **Why separate?**: Upload is I/O heavy (receiving large files, writing to S3). Isolating it prevents slow uploads from affecting other APIs.

#### 3. Grader Service (`cmd/grader/`)

- **Port**: None (not an HTTP server — it's a Kafka consumer)
- **Role**: AI-powered paper grading
- **What it does**:
  - Consumes `paper-uploaded` events from Kafka
  - Downloads the PDF from S3
  - Renders PDF pages to JPEG images (using MuPDF)
  - Sends images + rubric to the Vision-LLM (OpenRouter API)
  - Parses the structured JSON feedback
  - Saves the grade + outbox event in a single transaction
  - Outbox Publisher publishes `paper-graded` to Kafka
- **Why separate?**: Grading is **CPU-intensive** (PDF rendering) and **slow** (LLM API call takes 20-60 seconds). Must not block any HTTP endpoint.

#### 4. Results Service (`cmd/results/`)

- **Port**: 8083
- **Role**: Query results
- **What it does**:
  - `GET /results` — list all submissions for the authenticated user (with grades)
  - `GET /results/:id` — get a specific submission with its grade
  - Supports pagination (limit/offset)
- **Why separate?**: Read-heavy service. Can be scaled independently. Could add caching later without affecting writes.

---

## 5. End-to-End Request Flow

### Step-by-Step: User Uploads a Paper and Gets Results

```
Step 1: User opens the Upload page in browser
        Browser ──GET──► Vite dev server (serves React SPA)

Step 2: User fills form and clicks "Submit for grading"
        Browser ──POST multipart/form-data──► http://localhost:8080/upload
        (with Authorization: Bearer <access_token>)

Step 3: API Gateway receives the request
        ├── Auth middleware validates the JWT token
        ├── Extracts user_id from JWT claims
        ├── Sets X-User-ID header
        └── Reverse-proxies to Upload Service (http://upload-service:8081/upload)

Step 4: Upload Service processes the request
        ├── Reads X-User-ID header
        ├── Parses multipart form (file + metadata)
        ├── Validates PDF (magic bytes: %PDF-)
        ├── Validates file size (≤ 25 MB)
        ├── Uploads file to AWS S3 → gets back s3_key
        ├── BEGIN TRANSACTION
        │   ├── INSERT into submissions table
        │   └── INSERT into outbox_events table (event_type: "paper-uploaded")
        │   COMMIT
        └── Returns { submission_id, status: "uploaded" }

Step 5: Outbox Publisher (background goroutine in Upload Service)
        ├── Polls outbox_events table every 5 seconds
        ├── Finds the new "paper-uploaded" event
        ├── Publishes it to Kafka topic "paper-uploaded"
        └── Marks the outbox event as "published"

Step 6: Grader Service (Kafka consumer)
        ├── Receives "paper-uploaded" message from Kafka
        ├── Parses the event: { submission_id, s3_key, user_id }
        ├── Updates submission status → "processing"
        ├── Downloads PDF from S3
        ├── Renders each page to a JPEG image (150 DPI)
        ├── Builds a prompt with:
        │   ├── System prompt (grading instructions)
        │   ├── Rubric text
        │   └── Base64-encoded page images
        ├── Sends to OpenRouter (Vision-LLM) API
        ├── Receives structured JSON: { overall_score, summary, criteria: [...] }
        ├── BEGIN TRANSACTION
        │   ├── INSERT into grades table
        │   ├── UPDATE submission status → "graded"
        │   └── INSERT into outbox_events (event_type: "paper-graded")
        │   COMMIT
        └── Outbox Publisher → publishes "paper-graded" to Kafka

Step 7: User navigates to Dashboard or Result page
        Browser ──GET──► http://localhost:8080/results
        (or /results/:submission_id)
        ├── API Gateway validates JWT, reverse-proxies to Results Service
        └── Results Service queries PostgreSQL (submission LEFT JOIN grades)

Step 8: If still processing, frontend polls every 5 seconds
        The ResultPage component has a setInterval that calls the API
        every 5 seconds until status is "graded" or "failed"
```

### Sequence Diagram

```
  Browser        API Gateway     Upload Svc     S3      PostgreSQL     Kafka      Grader Svc     OpenRouter
    │                │               │           │          │            │             │              │
    │──POST /upload──►               │           │          │            │             │              │
    │                │──validate JWT──►           │          │            │             │              │
    │                │──proxy────────►            │          │            │             │              │
    │                │               │──upload──► │          │            │             │              │
    │                │               │  ◄─s3_key──│          │            │             │              │
    │                │               │──BEGIN TX──────────── ►            │             │              │
    │                │               │  INSERT submission    │            │             │              │
    │                │               │  INSERT outbox_event  │            │             │              │
    │                │               │──COMMIT───────────── ►│            │             │              │
    │  ◄─────────────│◄──201 Created─│           │          │            │             │              │
    │                │               │           │          │            │             │              │
    │                │          [Outbox Publisher polls]     │            │             │              │
    │                │               │──publish──────────────────────── ►│             │              │
    │                │               │           │          │            │             │              │
    │                │               │           │          │            │──consume── ►│              │
    │                │               │           │          │◄─update status──────────│              │
    │                │               │           │◄─download PDF─────────────────────│              │
    │                │               │           │──pdf bytes──────────────────────── ►│              │
    │                │               │           │          │            │  render pages│              │
    │                │               │           │          │            │  ──send images+rubric────── ►│
    │                │               │           │          │            │  ◄──structured feedback──── │
    │                │               │           │          │◄─save grade TX──────────│              │
    │                │               │           │          │            │◄─publish graded────────────│
    │                │               │           │          │            │             │              │
    │──GET /results/:id──────────── ►│           │          │            │             │              │
    │                │──proxy to results svc──── ►          │            │             │              │
    │                │               │           │          │◄─query─── ►│             │              │
    │  ◄─────────────│◄──grade + feedback────── ►│          │            │             │              │
```

---

## 6. Database Schema & Design

### Entity Relationship Diagram

```
┌──────────────────────┐       ┌──────────────────────────┐
│       users          │       │     refresh_tokens       │
├──────────────────────┤       ├──────────────────────────┤
│ id         UUID PK   │◄──┐  │ id          UUID PK      │
│ email      VARCHAR   │   │  │ user_id     UUID FK ──────┘
│ password_hash VARCHAR│   │  │ token_hash  VARCHAR       │
│ full_name  VARCHAR   │   │  │ expires_at  TIMESTAMP     │
│ created_at TIMESTAMP │   │  │ revoked     BOOLEAN       │
│ updated_at TIMESTAMP │   │  │ created_at  TIMESTAMP     │
└──────────┬───────────┘   │  └──────────────────────────┘
           │               │
           │ 1:N            │
           ▼               │
┌──────────────────────────┐
│      submissions         │
├──────────────────────────┤
│ id            UUID PK    │
│ user_id       UUID FK ───┘
│ roll_no       VARCHAR
│ course        VARCHAR
│ max_score     INTEGER
│ answer_scheme TEXT        │  ← rubric / expected answers
│ s3_key        VARCHAR    │  ← path in S3 bucket
│ file_size     INTEGER
│ status        VARCHAR    │  ← uploaded | processing | graded | failed
│ error_message TEXT
│ created_at    TIMESTAMP
│ updated_at    TIMESTAMP
└──────────┬───────────────┘
           │
     ┌─────┴──────┐
     │ 1:1        │ 1:N
     ▼            ▼
┌────────────┐  ┌──────────────────────┐
│   grades   │  │   outbox_events      │
├────────────┤  ├──────────────────────┤
│ id     PK  │  │ id           UUID PK │
│ sub_id FK  │  │ submission_id UUID FK│
│ score  INT │  │ event_type   VARCHAR │
│ feedback   │  │ payload      JSONB   │
│   TEXT     │  │ status       VARCHAR │
│ graded_at  │  │ attempt_no   INT     │
│ created_at │  │ error        TEXT    │
└────────────┘  │ published_at TIMESTAMP│
                │ created_at  TIMESTAMP│
                └──────────────────────┘
```

### Key Design Decisions

| Decision | Reasoning |
|---|---|
| UUID primary keys | Globally unique, safe for distributed systems, no auto-increment conflicts across services |
| `pgcrypto` extension | Generates UUIDs at the database level (`gen_random_uuid()`) — no application-level UUID generation race conditions |
| `outbox_events` table | Implements the Transactional Outbox Pattern (explained in §8) |
| `status` as VARCHAR | Simple state machine: uploaded → processing → graded/failed |
| `feedback` as TEXT (JSON) | Flexible schema — each LLM can return slightly different structures |
| `answer_scheme` as TEXT | The rubric can be any length and format |
| `UNIQUE(submission_id)` on grades | One grade per submission — prevents duplicate grading |
| `updated_at` trigger | Auto-updates timestamp on any row modification |
| Foreign keys with `ON DELETE CASCADE` | Deleting a user removes all their submissions, grades, etc. |

---

## 7. Authentication System Deep Dive

### Flow Diagram

```
┌──────────┐                    ┌──────────────┐              ┌────────────┐
│  Browser │                    │  API Gateway │              │ PostgreSQL │
└────┬─────┘                    └──────┬───────┘              └─────┬──────┘
     │                                 │                            │
     │── POST /auth/register ─────────►│                            │
     │   { email, password, name }     │── Check email exists ─────►│
     │                                 │◄── null (not found) ──────│
     │                                 │                            │
     │                                 │── bcrypt.Hash(password) ──►│ (CPU)
     │                                 │                            │
     │                                 │── INSERT user ────────────►│
     │                                 │◄── user with id ──────────│
     │                                 │                            │
     │                                 │── Generate JWT (15 min) ──►│ (in memory)
     │                                 │── Generate refresh token ──│ (crypto/rand)
     │                                 │── SHA256(refresh_token) ──►│
     │                                 │── INSERT refresh_token ───►│
     │                                 │                            │
     │◄── { access_token, refresh_token, user } ──────────────────│
     │                                 │                            │
     │    [stores tokens in localStorage]                           │
     │                                 │                            │
     │── GET /results ────────────────►│                            │
     │   Authorization: Bearer <jwt>   │                            │
     │                                 │── Validate JWT ──────────►│ (in memory)
     │                                 │── Extract user_id          │
     │                                 │── Set X-User-ID header     │
     │                                 │── Proxy to Results Service │
```

### Token Strategy

```
Access Token (JWT):
  ├── Algorithm: HS256 (HMAC with SHA-256)
  ├── Lifetime: 15 minutes
  ├── Contains: user_id, email, issuer, expiry
  ├── Stored: localStorage on client
  └── Purpose: Authenticate API requests

Refresh Token:
  ├── Format: 32 random bytes → hex string (64 chars)
  ├── Lifetime: 30 days
  ├── Stored in DB as: SHA-256 hash (never store raw token in DB)
  ├── Stored: localStorage on client
  └── Purpose: Get new access token without re-login

Why SHA-256 hash in DB?
  If an attacker gets the database, they can't use the
  hashed refresh tokens. They'd need the original token.
```

### Why JWT + Refresh Token (not sessions)?

| JWT + Refresh | Server Sessions |
|---|---|
| Stateless — no session store needed | Requires Redis or DB lookup on every request |
| Gateway can validate without DB call | Must hit DB/Redis on every request |
| Works naturally with microservices | Session ID must be shared across services |
| Short access token = damage window is small | Session can persist until explicit logout |

---

## 8. Transactional Outbox Pattern

This is the **most important architectural pattern** in this project.

### The Problem It Solves

```
❌ NAIVE APPROACH (can lose events):

  BEGIN TRANSACTION
    INSERT submission
  COMMIT
                          ← What if the app crashes HERE?
  kafka.Publish("paper-uploaded")   ← This never happens!

Result: Submission is in DB but Kafka never got the event.
        The paper will NEVER be graded. Data is inconsistent.
```

```
❌ ALSO BAD (can create orphan events):

  kafka.Publish("paper-uploaded")   ← Message sent OK
                          ← What if the app crashes HERE?
  BEGIN TRANSACTION
    INSERT submission
  COMMIT                 ← This never happens!

Result: Kafka got the event, but no submission exists in DB.
        The grader will fail when it tries to load the submission.
```

### The Solution: Transactional Outbox

```
✅ OUTBOX PATTERN:

  BEGIN TRANSACTION
    INSERT submission              ← row 1
    INSERT outbox_events           ← row 2 (same transaction!)
  COMMIT
  ← Both rows are saved atomically. Either BOTH exist or NEITHER.

  [Later, in a background goroutine]
  Outbox Publisher:
    1. SELECT * FROM outbox_events WHERE status = 'pending'
    2. For each event:
       a. kafka.Publish(event.payload)
       b. If success: UPDATE outbox_events SET status = 'published'
       c. If failure: Increment attempt_no, retry next poll cycle
       d. After 5 failures: Mark as 'failed'
```

### Visual Diagram

```
┌────────────────────────────────────────────────────────────┐
│                   Upload Service                            │
│                                                            │
│  handleUpload()                                            │
│  ┌─────────────────────────────────────────────┐           │
│  │  Database Transaction                       │           │
│  │  ┌─────────────────────────────────────────┐│           │
│  │  │ INSERT INTO submissions (...)           ││           │
│  │  │ INSERT INTO outbox_events (             ││           │
│  │  │   event_type='paper-uploaded',          ││           │
│  │  │   payload='{"submission_id":"...",      ││           │
│  │  │            "s3_key":"..."}'             ││           │
│  │  │   status='pending'                     ││           │
│  │  │ )                                       ││           │
│  │  └─────────────────────────────────────────┘│           │
│  │  COMMIT ✓                                   │           │
│  └─────────────────────────────────────────────┘           │
│                                                            │
│  ┌─────────────────────────────────────────────┐           │
│  │  Outbox Publisher (background goroutine)    │           │
│  │                                             │           │
│  │  Every 5 seconds:                           │           │
│  │  ┌───────────────────────────────────────┐  │           │
│  │  │ SELECT * FROM outbox_events           │  │           │
│  │  │ WHERE status = 'pending'              │  │           │
│  │  │ ORDER BY created_at LIMIT 100         │  │           │
│  │  └──────────────┬────────────────────────┘  │           │
│  │                 │                            │           │
│  │                 ▼                            │           │
│  │  ┌───────────────────────────────────────┐  │           │
│  │  │ kafka.Publish(topic, key, payload)    │──┼──►  Kafka │
│  │  └──────────────┬────────────────────────┘  │           │
│  │                 │                            │           │
│  │                 ▼                            │           │
│  │  ┌───────────────────────────────────────┐  │           │
│  │  │ UPDATE outbox_events                  │  │           │
│  │  │ SET status='published',               │  │           │
│  │  │     published_at=NOW()                │  │           │
│  │  └───────────────────────────────────────┘  │           │
│  └─────────────────────────────────────────────┘           │
└────────────────────────────────────────────────────────────┘
```

### Why Not Just Use Kafka Transactions?

| Outbox Pattern | Kafka Transactions |
|---|---|
| Works with any message broker | Kafka-specific |
| Database is the source of truth | Two sources of truth (DB + Kafka) |
| Easy to debug (query outbox table) | Hard to inspect Kafka state |
| Retry logic is simple SQL | Complex transaction coordinator |
| No distributed transaction needed | Requires two-phase commit analogy |

---

## 9. Kafka Event-Driven Pipeline

### Topic Design

```
Topic: paper-uploaded
  ├── Producer: Upload Service (via Outbox Publisher)
  ├── Consumer: Grader Service (consumer group: grader-consumer-group)
  ├── Key: submission_id (ensures ordering per submission)
  └── Payload: { "submission_id": "...", "s3_key": "...", "user_id": "..." }

Topic: paper-graded
  ├── Producer: Grader Service (via Outbox Publisher)
  ├── Consumer: (currently unused — ready for future services like notifications)
  ├── Key: submission_id
  └── Payload: { "submission_id": "...", "grade_id": "...", "score": 85, "feedback": {...} }
```

### Kafka Configuration

```
Producer Config:
  ├── RequiredAcks: ALL (wait for all replicas — strongest durability)
  ├── Async: false (synchronous writes for reliability)
  ├── Compression: Snappy (fast compression, reduces network I/O)
  ├── Balancer: Hash (same key → same partition → ordering guarantee)
  └── AutoTopicCreation: true

Consumer Config:
  ├── GroupID: "grader-consumer-group"
  ├── StartOffset: FirstOffset (process all unread messages on startup)
  ├── CommitInterval: 1 second
  └── MaxBytes: 10 MB per fetch
```

### Why Kafka Over RabbitMQ or Redis Streams?

| Feature | Kafka | RabbitMQ | Redis Streams |
|---|---|---|---|
| Message durability | Disk-based log, replicated | Memory + optional disk | Memory + optional AOF |
| Replay old messages | ✅ Yes (consumer resets offset) | ❌ No (message is gone after ack) | ✅ Limited |
| Consumer groups | ✅ Native | ✅ (but different model) | ✅ (simpler) |
| Throughput | Very high (millions/sec) | Medium | High |
| Ordering guarantee | Per-partition | Per-queue | Per-stream |
| Best for | Event sourcing, audit logs | Task queues, RPC | Caching + lightweight streams |

**For this project**: Kafka's replay capability means if the grader crashes, it picks up from the last committed offset — no events are lost.

---

## 10. AI/LLM Grading Pipeline

### How the Vision-LLM Grading Works

```
                    PDF (from S3)
                         │
                         ▼
              ┌─────────────────────┐
              │   PDF Renderer      │
              │   (go-fitz/MuPDF)   │
              │                     │
              │   150 DPI rendering │
              │   JPEG quality: 85  │
              │   Concurrent pages  │
              └────────┬────────────┘
                       │
                       ▼
              Page 1.jpg, Page 2.jpg, ... Page N.jpg
              (max 10 pages per submission)
                       │
                       ▼
              ┌─────────────────────────────────────┐
              │        OpenRouter API Call            │
              │                                     │
              │  Model: x-ai/grok-4.1-fast          │
              │  Temperature: 0 (deterministic)      │
              │                                     │
              │  System Prompt:                      │
              │  "You are an academic examiner..."   │
              │                                     │
              │  User Message:                       │
              │  ├── Text: rubric + instructions     │
              │  └── Images: base64-encoded JPEGs    │
              │                                     │
              │  Expected Response: JSON             │
              │  {                                   │
              │    "overall_score": 85,              │
              │    "summary": "Good work...",        │
              │    "criteria": [                     │
              │      {                               │
              │        "name": "Q1",                 │
              │        "score": 18,                  │
              │        "max_score": 20,              │
              │        "comment": "Correct approach" │
              │      }                               │
              │    ]                                 │
              │  }                                   │
              └─────────────────────────────────────┘
```

### Why Vision-LLM Instead of OCR + Text-LLM?

```
OCR Approach (❌ Fragile):
  PDF → OCR (Tesseract) → Noisy text → Text LLM → Grade
  Problems:
  • OCR errors on handwriting (especially math, diagrams)
  • Loses spatial context (tables, circled answers, crossed-out text)
  • Two-step pipeline = more failure points

Vision Approach (✅ Robust):
  PDF → Render to images → Vision LLM → Grade
  Advantages:
  • LLM actually "sees" the answer sheet like a human examiner
  • Handles handwriting, diagrams, equations, annotations
  • Single-step AI call — fewer things to break
  • Works with any language or script
```

### Retry & Error Handling

```
LLM Call Retry Logic:
  ├── Max retries: 2 (total 3 attempts)
  ├── Retry on: malformed JSON, empty criteria, server errors (5xx), rate limit (429)
  ├── No retry on: client errors (4xx except 429)
  ├── Timeout: 4 minutes per call
  ├── Score clamping: 0 ≤ score ≤ max_score
  └── JSON cleanup: Strip markdown fences (```json ... ```)
```

---

## 11. Frontend Architecture

### Component Tree

```
<App>
  <AuthProvider>          ← Context: user state, login/logout/register functions
    <BrowserRouter>
      <Routes>
        <Route "/" → <LandingPage />                              ← Public
        <Route "/login" → <PublicRoute><LoginPage /></PublicRoute> ← Redirects to /dashboard if logged in
        <Route "/register" → <PublicRoute><RegisterPage /></PublicRoute>
        <Route "/dashboard" → <ProtectedRoute><DashboardPage /></ProtectedRoute>
        <Route "/upload" → <ProtectedRoute><UploadPage /></ProtectedRoute>
        <Route "/results/:id" → <ProtectedRoute><ResultPage /></ProtectedRoute>
      </Routes>
    </BrowserRouter>
  </AuthProvider>
</App>
```

### Route Protection Pattern

```jsx
// ProtectedRoute: Redirects to /login if no user
function ProtectedRoute({ children }) {
  const { user } = useAuth()
  return user ? children : <Navigate to="/login" replace />
}

// PublicRoute: Redirects to /dashboard if already logged in
function PublicRoute({ children }) {
  const { user } = useAuth()
  return user ? <Navigate to="/dashboard" replace /> : children
}
```

### Auth State Management

```
On App Load:
  └── AuthProvider initializes
      └── Reads user from localStorage (synchronous)
          ├── If user exists → set as authenticated (no flash)
          └── If null → user is logged out

On Login/Register:
  └── API call → success
      ├── Save access_token to localStorage
      ├── Save refresh_token to localStorage
      ├── Save user JSON to localStorage
      └── Update React state → triggers re-render → ProtectedRoute allows access

On Logout:
  └── Clear localStorage
      └── Set user to null → ProtectedRoute redirects to /login
```

### API Client Design

```
client.js
  ├── BASE = '' (same-origin, Vite proxies to API Gateway)
  ├── getToken() → reads access_token from localStorage
  ├── request(path, options) → generic fetch wrapper
  │   ├── Auto-adds Content-Type: application/json
  │   ├── Auto-adds Authorization: Bearer <token>
  │   ├── Parses JSON response
  │   └── Throws structured error with status + code
  ├── authApi
  │   ├── .register(body)
  │   ├── .login(body)
  │   └── .refresh(body)
  ├── uploadApi
  │   └── .upload(formData) ← uses raw fetch (no JSON Content-Type for multipart)
  └── resultsApi
      ├── .list(limit, offset)
      └── .get(id)
```

### Real-Time Polling (Result Page)

```
ResultPage loads:
  1. fetch /results/:id
  2. If status is NOT "graded" or "failed":
     └── setInterval(fetch, 5000) ← poll every 5 seconds
  3. When status becomes "graded" or "failed":
     └── clearInterval() ← stop polling
  4. On unmount:
     └── clearInterval() ← cleanup
```

---

## 12. Docker & Deployment

### Docker Compose Architecture

```
docker-compose.yml spins up:

  ┌─────────────────────────────────────────────────────────────┐
  │                 Docker Network (bridge)                     │
  │                                                             │
  │  ┌──────────┐  ┌──────────┐  ┌──────────┐  ┌───────────┐ │
  │  │ postgres │  │zookeeper │  │  kafka   │  │kafka-init │ │
  │  │ :5432    │  │ :2181    │  │ :9092    │  │ (one-shot)│ │
  │  └──────────┘  └──────────┘  └──────────┘  └───────────┘ │
  │                                                             │
  │  ┌──────────────┐  ┌──────────────┐  ┌──────────────────┐ │
  │  │ api-gateway  │  │upload-service│  │ results-service  │ │
  │  │ :8080        │  │ :8081        │  │ :8083            │ │
  │  └──────────────┘  └──────────────┘  └──────────────────┘ │
  │                                                             │
  │  ┌──────────────────────────────────────┐                  │
  │  │ grader-service (custom Dockerfile)   │                  │
  │  │ Requires MuPDF C libraries (CGO)     │                  │
  │  └──────────────────────────────────────┘                  │
  └─────────────────────────────────────────────────────────────┘
```

### Multi-Stage Dockerfile (Grader)

```dockerfile
# Stage 1: BUILD — full Go + C toolchain
FROM golang:1.25-bookworm AS builder
  ├── Install libmupdf-dev, libfreetype-dev, etc.
  ├── COPY go.mod go.sum → go mod download (layer cached!)
  ├── COPY . . → build binary
  └── Output: /grader (single binary)

# Stage 2: RUNTIME — minimal Debian
FROM debian:bookworm-slim
  ├── Install only runtime libs (no dev headers)
  ├── COPY --from=builder /grader /grader
  └── ENTRYPOINT ["/grader"]

Result: ~100 MB image vs ~1.5 GB if we kept the builder stage
```

### Why Multi-Stage Build?

```
Without multi-stage:
  Image size: ~1.5 GB (includes Go compiler, all dev headers, source code)

With multi-stage:
  Image size: ~100 MB (only binary + shared libraries)

Benefits:
  • Faster deploys (smaller image to push/pull)
  • Smaller attack surface (no compiler in production)
  • No source code leaked in production image
```

---

## 13. Folder Structure Explained

```
ai-grader/
├── cmd/                        ← Entry points (main.go files)
│   ├── api/                    ← API Gateway service
│   │   ├── main.go             ← Server setup, routing, proxy config
│   │   └── handlers.go         ← Auth handlers (register, login, refresh)
│   ├── grader/                 ← Grader Service
│   │   └── main.go             ← Kafka consumer, LLM orchestration
│   ├── results/                ← Results Service
│   │   └── main.go             ← HTTP server for results queries
│   └── upload/                 ← Upload Service
│       └── main.go             ← HTTP server for file uploads
│
├── internal/                   ← Shared internal packages (not importable externally)
│   ├── auth/                   ← Authentication module
│   │   ├── jwt.go              ← Token generation & validation
│   │   ├── middleware.go       ← HTTP auth middleware
│   │   ├── password.go         ← bcrypt hashing
│   │   └── repository.go      ← User & refresh token DB operations
│   ├── database/               ← PostgreSQL connection pool
│   │   └── db.go               ← Connect, Close, global Pool
│   ├── grading/                ← Grading domain logic
│   │   ├── openrouter.go       ← Vision-LLM API client
│   │   ├── repository.go       ← Grade DB operations + outbox
│   │   └── service.go          ← Grading orchestration (download → render → grade → save)
│   ├── kafka/                  ← Kafka infrastructure
│   │   ├── consumer.go         ← Generic Kafka consumer
│   │   ├── producer.go         ← Generic Kafka producer
│   │   ├── outbox.go           ← Outbox repository (CRUD on outbox_events)
│   │   └── outbox_publisher.go ← Background outbox → Kafka publisher
│   ├── models/                 ← Shared data models / DTOs
│   │   └── models.go           ← User, Submission, Grade, OutboxEvent, etc.
│   ├── pdf/                    ← PDF utilities
│   │   ├── renderer.go         ← PDF → JPEG page rendering (MuPDF)
│   │   └── validator.go        ← Magic byte validation, size check
│   ├── results/                ← Results domain logic
│   │   ├── handler.go          ← HTTP handlers for results
│   │   ├── repository.go       ← Results DB queries (JOIN submissions + grades)
│   │   └── service.go          ← Business logic layer
│   ├── s3/                     ← AWS S3 client
│   │   └── client.go           ← Upload, Download, Delete, Pre-signed URLs
│   └── upload/                 ← Upload domain logic
│       ├── repository.go       ← Submission CRUD
│       └── service.go          ← Upload orchestration (submission + outbox in TX)
│
├── migrations/                 ← SQL migration files
│   ├── 001_schema.sql          ← CREATE tables
│   └── 001_schema.down.sql     ← DROP tables
│
├── web/                        ← React frontend
│   ├── src/
│   │   ├── App.jsx             ← Root component with routing
│   │   ├── api/client.js       ← API client functions
│   │   ├── components/         ← Reusable UI components
│   │   ├── context/            ← React Context providers
│   │   └── pages/              ← Page-level components
│   ├── package.json
│   ├── vite.config.js
│   └── tailwind.config.js
│
├── docker-compose.yml          ← Full stack orchestration
├── Dockerfile.grader           ← Multi-stage build for grader
├── go.mod                      ← Go dependencies
└── Makefile                    ← Build commands
```

### Why `cmd/` and `internal/`?

This is **standard Go project layout** (recommended by the Go community):

- `cmd/` — Each subfolder is a separate binary. `go build ./cmd/...` builds all four services.
- `internal/` — Go enforces that packages under `internal/` cannot be imported by code outside this module. This prevents other projects from depending on your internal implementation details.

---

## 14. Design Patterns Used

### 1. Repository Pattern

```
Handler → Service → Repository → Database

Each layer has a clear responsibility:
  • Handler: HTTP request/response parsing
  • Service: Business logic, orchestration
  • Repository: Pure database operations

Why?
  • Testable: Mock the repository to test service logic
  • Swappable: Change from PostgreSQL to MongoDB by replacing the repository
  • Clean: No SQL in handlers, no HTTP in repositories
```

### 2. API Gateway Pattern

```
All clients → API Gateway → Internal Services

Why?
  • Single entry point (one URL for clients)
  • Centralized authentication
  • Service discovery is internal only
  • Can add rate limiting, logging, CORS in one place
```

### 3. Transactional Outbox Pattern

*(Covered in detail in §8)*

Ensures data consistency between the database and the message broker without distributed transactions.

### 4. Event-Driven Architecture

```
Services communicate via events, not direct HTTP calls:

  Upload Service ──event──► Kafka ──event──► Grader Service

Why not direct HTTP call from Upload to Grader?
  • Upload would block until grading completes (30-60 sec)
  • If Grader is down, Upload fails too (tight coupling)
  • Can't scale Grader independently
  • No retry/replay capability
```

### 5. Reverse Proxy Pattern

```
The API Gateway uses Go's httputil.ReverseProxy:
  • Client sends request to Gateway
  • Gateway forwards the request to the correct backend
  • Response flows back through the Gateway

Extra functionality added:
  • Injects X-User-ID header from JWT claims
  • Custom error handler (returns JSON instead of plain text)
  • FlushInterval: -1 for streaming uploads
```

### 6. Consumer Group Pattern (Kafka)

```
Multiple Grader instances can run in the same consumer group:

  Kafka partition 0 ──► Grader instance A
  Kafka partition 1 ──► Grader instance B
  Kafka partition 2 ──► Grader instance C

Each message is processed by exactly ONE consumer in the group.
Scaling = add more Grader instances + more partitions.
```

### 7. Graceful Shutdown Pattern

```go
// Every service follows this pattern:
quit := make(chan os.Signal, 1)
signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)
<-quit  // Block until signal received

// Start graceful shutdown
ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
server.Shutdown(ctx)  // Finish in-flight requests, then stop
```

### 8. Middleware Chain Pattern

```
Request → CORS → Auth Middleware → Route Handler

Auth Middleware:
  1. Extract "Authorization: Bearer <token>" header
  2. Validate JWT
  3. Store claims in request context
  4. Call next handler
```

---

## 15. Interview Questions & Answers

### Architecture & System Design

---

**Q1: Why did you choose microservices over a monolith?**

The grading step takes 20-60 seconds (LLM API call + PDF rendering). If this ran inside the same process as the upload endpoint, users would experience timeouts. By splitting into separate services:
- Upload responds instantly (< 2 sec)
- Grading runs asynchronously in the background
- Results service stays fast for read queries
- Each service can be scaled independently (e.g., run 5 grader instances during peak exam season)

---

**Q2: Why an API Gateway instead of letting clients call services directly?**

Three reasons:
1. **Single endpoint**: Clients know only one URL. No CORS complexity with multiple backends.
2. **Centralized auth**: JWT validation happens once at the gateway. Backend services trust the `X-User-ID` header — they don't need to know about JWT at all.
3. **Flexibility**: I can add/remove/rewrite backend services without changing the client. The gateway is the stable interface.

---

**Q3: How does the system handle failures during paper grading?**

Multiple layers of resilience:
1. **Transactional Outbox**: Even if the app crashes after inserting the submission, the outbox event persists. The publisher will retry.
2. **Kafka durability**: Messages are persisted to disk with `RequiredAcks: ALL`. If the grader crashes, the message stays in Kafka (consumer offset not committed) and will be reprocessed on restart.
3. **LLM retries**: The OpenRouter client retries up to 2 times on transient errors (malformed JSON, 5xx, rate limit).
4. **Status tracking**: If grading fails permanently, the submission is marked as `failed` with an error message, so the user sees what went wrong.
5. **Outbox retry limit**: After 5 failed publish attempts, the outbox event is marked `failed` to prevent infinite loops.

---

**Q4: What happens if the same message is processed twice (idempotency)?**

The `grades` table has a `UNIQUE(submission_id)` constraint. If the grader processes the same submission twice (e.g., due to Kafka rebalancing before offset commit), the second `INSERT INTO grades` will fail with a unique constraint violation, and the error is logged. The previously saved grade remains intact.

---

**Q5: Why PostgreSQL over MongoDB?**

1. **Transactions**: The outbox pattern requires ACID transactions (insert submission + outbox event atomically). MongoDB transactions work but are more complex and have more caveats.
2. **Relational integrity**: Users → Submissions → Grades is a natural relational model with foreign keys. MongoDB can't enforce referential integrity.
3. **JSONB**: PostgreSQL supports JSON storage (`outbox_events.payload` is JSONB) for the parts that need flexibility, giving us the best of both worlds.

---

**Q6: Why S3 for file storage instead of saving PDFs in PostgreSQL?**

1. **Size**: PDFs can be up to 25 MB. Storing large blobs in PostgreSQL bloats the database, slows backups, and hurts query performance.
2. **Cost**: S3 costs ~$0.023/GB/month. PostgreSQL storage (on RDS) costs ~$0.115/GB/month — 5x more.
3. **Scalability**: S3 handles unlimited files with 99.999999999% durability. No need to shard the database for storage.
4. **Access control**: S3 pre-signed URLs can grant temporary download access without hitting our API.

---

**Q7: Why use the Transactional Outbox Pattern instead of publishing directly to Kafka?**

Direct Kafka publish creates a **dual-write problem**: you need to write to both the database and Kafka, but you can't guarantee both succeed atomically.

- If you write to DB first, then publish to Kafka — the app might crash between the two, and the Kafka message is lost.
- If you publish to Kafka first, then write to DB — the app might crash after Kafka publish, and the DB row is missing.

The Outbox pattern solves this by writing both the business data and the event into the **same database transaction**. A background process then reads pending events from the outbox table and publishes them to Kafka. If the publisher fails, it retries on the next poll cycle. The database is the single source of truth.

---

### Backend / Go

---

**Q8: Why Go for the backend?**

1. **Performance**: Go compiles to native binaries with ~10 MB memory footprint per service. Compared to Java (200+ MB JVM heap) or Node.js (50+ MB V8 runtime), it's significantly lighter for microservices.
2. **Concurrency**: Goroutines are lightweight (2 KB stack) vs OS threads (1 MB stack). Perfect for handling I/O-bound work (HTTP, Kafka, S3, database) concurrently.
3. **Single binary**: `go build` produces one static binary. No dependency management at deployment time. Just copy the binary and run.
4. **Fast compilation**: Full rebuild takes < 5 seconds. Rapid iteration.
5. **Standard library**: Go's `net/http` is production-grade. No need for Express/Spring Boot equivalent.

---

**Q9: How does the API Gateway reverse proxy work?**

The gateway uses `httputil.ReverseProxy` from Go's standard library:

1. Client sends request to `http://gateway:8080/upload`
2. The auth middleware validates the JWT token and stores claims in the request context
3. The reverse proxy's `Director` function modifies the request:
   - Changes the host to the upstream service URL
   - Adds `X-User-ID` header extracted from JWT claims
4. The request is forwarded to the upstream service (e.g., `http://upload-service:8081/upload`)
5. The response flows back through the proxy to the client

Key configuration:
- `FlushInterval: -1` — flushes response data immediately (important for streaming uploads)
- `ResponseHeaderTimeout: 5 min` — allows large file uploads to complete
- Custom `ErrorHandler` returns JSON error responses instead of Go's default plain-text "Bad Gateway"

---

**Q10: How do you handle database transactions in Go?**

Using `pgx/v5`'s transaction API:

```go
tx, err := db.Begin(ctx)       // Start transaction
if err != nil { return err }
defer tx.Rollback(ctx)         // Rollback if not committed (safe to call on committed tx)

// Execute queries within the transaction
tx.QueryRow(ctx, "INSERT ...", ...).Scan(&id)
tx.Exec(ctx, "INSERT ...", ...)

err = tx.Commit(ctx)           // Commit atomically
```

The `defer tx.Rollback(ctx)` is a safety net — if any error causes an early return before `Commit()`, the transaction is automatically rolled back. If `Commit()` was already called, `Rollback()` is a no-op.

---

**Q11: Why `pgx/v5` instead of `database/sql`?**

1. **Performance**: `pgx` is 2-3x faster than `database/sql` with the `lib/pq` driver because it uses PostgreSQL's native binary protocol instead of text encoding.
2. **Connection pooling**: Built-in `pgxpool.Pool` with configurable min/max connections.
3. **Context support**: Every operation accepts `context.Context` for timeout and cancellation.
4. **Type safety**: Native support for PostgreSQL types (UUID, JSONB, arrays) without custom scanners.

---

**Q12: How does the PDF rendering work (CGO)?**

The `go-fitz` library wraps MuPDF (a C library) via CGO:

1. `fitz.NewFromMemory(pdfData)` — opens the PDF from a byte slice
2. `doc.ImageDPI(pageNum, 150)` — renders page at 150 DPI to a Go `image.Image`
3. `jpeg.Encode(buf, img, quality:85)` — encodes to JPEG

Since MuPDF is not thread-safe, each goroutine creates its own document instance. Pages are rendered concurrently using a `sync.WaitGroup`:

```go
for i := 0; i < limit; i++ {
    wg.Add(1)
    go func(pageNum int) {
        defer wg.Done()
        localDoc, _ := fitz.NewFromMemory(pdfData)  // Each goroutine gets its own doc
        defer localDoc.Close()
        img, _ := localDoc.ImageDPI(pageNum, r.DPI)
        // ... encode to JPEG
    }(i)
}
wg.Wait()
```

This requires CGO (C compiler) at build time, which is why the grader needs a special Dockerfile with MuPDF dev headers.

---

**Q13: How does graceful shutdown work in your services?**

Every service follows this pattern:

1. Start the HTTP server in a goroutine
2. Main goroutine blocks on `signal.Notify(quit, SIGINT, SIGTERM)`
3. When signal received:
   - Create a timeout context (e.g., 30 seconds)
   - Call `server.Shutdown(ctx)` — stops accepting new connections, waits for in-flight requests to finish
   - Close database connections, Kafka consumers/producers
   - Exit cleanly

This ensures no request is dropped during deployments.

---

**Q14: How do you validate the uploaded PDF?**

Three layers of validation:

1. **Content-Type header**: Must start with `multipart/form-data`
2. **File size**: `pdf.ValidateFileSize(size)` — rejects files > 25 MB or empty files
3. **Magic bytes**: `pdf.ValidatePDF(reader)` — reads the first 5 bytes and checks for `%PDF-` signature. This prevents users from uploading a `.exe` renamed to `.pdf`.

---

**Q15: Explain the middleware pattern in your auth system.**

The auth middleware is a higher-order function:

```go
func AuthMiddleware(jwtSecret string) func(http.Handler) http.Handler {
    return func(next http.Handler) http.Handler {
        return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
            // 1. Extract "Authorization: Bearer <token>"
            // 2. Validate JWT signature and expiry
            // 3. Store claims in request context
            ctx := context.WithValue(r.Context(), UserContextKey, claims)
            // 4. Call the next handler with enriched context
            next.ServeHTTP(w, r.WithContext(ctx))
        })
    }
}
```

This follows the **decorator pattern** — it wraps a handler with additional behavior (auth check) without modifying the handler itself. The Gateway applies it as a subrouter middleware using `gorilla/mux`:

```go
protected := router.PathPrefix("").Subrouter()
protected.Use(auth.AuthMiddleware(jwtSecret))
```

All routes on `protected` automatically go through JWT validation.

---

### Frontend / React

---

**Q16: Why React with Vite instead of Next.js?**

This is a **Single Page Application (SPA)** — all rendering happens in the browser. We don't need Server-Side Rendering (SSR), which is Next.js's main selling point. Vite gives us:
- Faster dev server (native ES modules, no bundling during development)
- Simpler setup (no file-based routing magic, no API routes confusion)
- Smaller learning curve for the frontend

If we needed SEO (e.g., a public blog), Next.js would make sense. For a dashboard app behind a login, SPA is fine.

---

**Q17: Why Tailwind CSS instead of styled-components or CSS modules?**

1. **Speed**: Writing `className="text-sm text-stone-700"` is faster than creating a styled component with CSS-in-JS.
2. **Consistency**: Design tokens (colors, spacing, fonts) are defined in `tailwind.config.js` — every developer uses the same scale.
3. **Bundle size**: Tailwind purges unused classes in production. Typical output is ~10 KB gzipped. CSS-in-JS includes a runtime (~15 KB+ just for the library).
4. **No naming**: No need to think of class names or worry about name collisions.

---

**Q18: How does the auth state persist across page refreshes?**

The `AuthProvider` uses a **synchronous initializer** for `useState`:

```jsx
const [user, setUser] = useState(readUserFromStorage)
```

`readUserFromStorage` reads from `localStorage` **synchronously on the first render**. This means:
- No async "loading" state needed
- No flash of "unauthenticated" content
- The user appears logged in immediately if they have a valid token stored

On login/register, three things are saved to localStorage: `access_token`, `refresh_token`, and `user` (JSON). On logout, all three are removed.

---

**Q19: How does the result polling work?**

The `ResultPage` component uses `setInterval` to poll the API:

```jsx
useEffect(() => {
    load()                                           // Fetch immediately
    pollRef.current = setInterval(load, 5000)        // Then every 5 seconds
    return () => clearInterval(pollRef.current)       // Cleanup on unmount
}, [id])
```

Inside `load()`, when the status becomes `"graded"` or `"failed"`, polling is stopped: `clearInterval(pollRef.current)`.

**Why polling instead of WebSockets?**
- Simpler implementation (no WebSocket server, no connection management)
- Works behind any proxy/CDN (WebSockets can be problematic with corporate firewalls)
- 5-second interval is acceptable UX for a grading task that takes 30-60 seconds
- Less infrastructure to maintain

---

**Q20: How does the upload form handle file validation on the client?**

The `UploadPage` validates on the client side *before* sending to the server:

1. **File type**: `file.type === 'application/pdf'` — only accepts PDFs
2. **Drag & drop support**: `onDrop` event extracts the file from `e.dataTransfer.files`
3. **File input**: Hidden `<input type="file" accept="application/pdf">` triggered by clicking the drop zone

The server validates *again* (defense in depth) — magic bytes and file size.

---

### Security

---

**Q21: How do you secure passwords?**

Passwords are hashed using **bcrypt** with a cost factor of 10:
- Bcrypt is specifically designed for password hashing (unlike SHA-256)
- It's intentionally slow (prevents brute-force attacks)
- Each hash includes a unique salt (no rainbow table attacks)
- Cost factor 10 means 2^10 = 1,024 iterations

The raw password is NEVER stored. The `PasswordHash` field has `json:"-"` so it's never included in API responses.

---

**Q22: How do you prevent common API attacks?**

| Attack | Prevention |
|---|---|
| SQL injection | Parameterized queries (`$1, $2, $3`) — never string concatenation |
| XSS | React auto-escapes all rendered content |
| CSRF | Token-based auth (Bearer token in header, not cookies) |
| Brute force | bcrypt's slow hashing, short JWT lifetime (15 min) |
| File upload attacks | PDF magic byte validation, 25 MB size limit, S3 storage (not filesystem) |
| Token theft | Access tokens expire in 15 min, refresh tokens are hashed in DB |
| Path traversal | `filepath.Base()` sanitization in filename handling |

---

**Q23: Why is the refresh token stored as a SHA-256 hash in the database?**

If an attacker gains read access to the database (SQL injection, backup leak), they see only the hash, not the actual token. Since SHA-256 is a one-way function, they can't reverse it to get the original refresh token. The original token only exists in the client's localStorage.

---

### Kafka & Event-Driven

---

**Q24: Why did you choose `RequiredAcks: ALL` for the Kafka producer?**

`RequiredAcks: ALL` means the producer waits until **all in-sync replicas** have acknowledged the message before returning success. This provides the **strongest durability guarantee**:

| Setting | Meaning | Risk |
|---|---|---|
| `RequireNone` (0) | Fire and forget | Message may be lost |
| `RequireOne` (1) | Leader acknowledged | Lost if leader crashes before replication |
| `RequireAll` (-1) | All replicas acknowledged | Safest — message survives any single broker crash |

For exam papers, losing a message means a paper never gets graded. The extra latency (~5-10 ms) is worth the durability.

---

**Q25: What happens if Kafka is down when the Outbox Publisher tries to publish?**

The Outbox Publisher is designed for this:

1. It tries to publish the event to Kafka
2. If Kafka is down, the `PublishEvent` call returns an error
3. The publisher increments `attempt_no` and stores the error in the outbox row
4. On the next poll cycle (5 seconds later), it tries again
5. After 5 failed attempts, both the event is marked as `failed`

The submission still exists in the database — an admin can manually retry or investigate.

---

**Q26: How does the Kafka consumer handle poison messages (malformed events)?**

The `parsePaperUploadedEvent` function is very defensive:
1. It first tries direct JSON unmarshalling
2. If that fails, it tries extracting nested fields
3. If the `payload` field contains embedded JSON, it recursively parses that
4. If none of these work, it returns `false`, and the consumer logs a warning and **skips the message** (commits the offset)

This prevents one bad message from blocking the entire consumer group.

---

### Database

---

**Q27: Why use UUID instead of auto-incrementing integers for primary keys?**

1. **No collisions across services**: UUIDs can be generated independently by any service without coordination. Auto-increment IDs require a central database sequence.
2. **Security**: UUIDs don't reveal how many records exist. An auto-increment ID of `42` tells an attacker you have ≤42 submissions.
3. **Merge-friendly**: If you ever merge databases (e.g., from staging to production), UUIDs won't conflict.
4. **Database-generated**: Using `gen_random_uuid()` from PostgreSQL's `pgcrypto` extension is fast and eliminates application-level concerns about uniqueness.

---

**Q28: Explain the database indexing strategy.**

```sql
-- Users: Fast email lookup for login
CREATE INDEX idx_users_email ON users(email);

-- Submissions: Fast lookup by user, by status, and by date
CREATE INDEX idx_submissions_user_id ON submissions(user_id);
CREATE INDEX idx_submissions_status ON submissions(status);
CREATE INDEX idx_submissions_created_at ON submissions(created_at DESC);

-- Grades: Fast lookup by submission
CREATE INDEX idx_grades_submission_id ON grades(submission_id);

-- Outbox: Fast polling for pending events
CREATE INDEX idx_outbox_events_status ON outbox_events(status, created_at);

-- Refresh tokens: Fast token lookup
CREATE INDEX idx_refresh_tokens_token_hash ON refresh_tokens(token_hash);
```

The `outbox_events(status, created_at)` is a **composite index** — it covers the exact query the Outbox Publisher runs: `WHERE status = 'pending' ORDER BY created_at ASC`. PostgreSQL can satisfy this query entirely from the index without touching the table.

---

**Q29: Why use a trigger for `updated_at` instead of setting it in application code?**

```sql
CREATE TRIGGER update_submissions_updated_at
  BEFORE UPDATE ON submissions
  FOR EACH ROW EXECUTE FUNCTION update_updated_at_column();
```

1. **Consistency**: Every UPDATE on the table automatically sets `updated_at = NOW()`, regardless of which service or query made the change.
2. **Can't forget**: Application code might miss setting `updated_at` in some code path. The trigger ensures it's always set.
3. **Direct SQL updates**: If an admin runs a manual `UPDATE` in psql, the timestamp is still correct.

---

### DevOps

---

**Q30: Why does the grader need a special Dockerfile while other services don't?**

The grader uses `go-fitz`, which wraps the **MuPDF C library** via CGO. This requires:
- A C compiler (`gcc`) at build time
- MuPDF development headers (`libmupdf-dev`) at build time
- MuPDF shared libraries at runtime

The other services are pure Go (no CGO) and can run with a simple `go run ./cmd/...` command inside a generic `golang` Docker image. The grader needs a multi-stage Dockerfile to install the C dependencies.

---

**Q31: How would you deploy this in production?**

1. **Container orchestration**: Kubernetes or AWS ECS to manage the 4 services
2. **Managed services**: Replace Docker Compose's Postgres/Kafka with Amazon RDS (PostgreSQL) and Amazon MSK (Kafka)
3. **CI/CD**: GitHub Actions → build Docker images → push to ECR → deploy to ECS/K8s
4. **HTTPS**: Put an ALB (Application Load Balancer) with TLS termination in front of the API Gateway
5. **Secrets**: Use AWS Secrets Manager for `JWT_SECRET`, `OPENROUTER_API_KEY`, database credentials
6. **Monitoring**: Prometheus + Grafana for metrics, CloudWatch/Loki for logs
7. **Auto-scaling**: Scale grader instances based on Kafka consumer lag

---

**Q32: How do environment variables manage configuration?**

Every service reads configuration from environment variables using `os.Getenv()`:
- `godotenv.Load()` loads a `.env` file in development (optional, won't error if missing)
- In production (Docker/K8s), real environment variables take precedence
- Sensible defaults are provided (`port := os.Getenv("GATEWAY_PORT"); if port == "" { port = "8080" }`)

This follows the **12-Factor App** principle (config in environment).

---

### Performance

---

**Q33: How does the connection pool work?**

```go
config.MaxConns = 20    // Maximum 20 concurrent database connections
config.MinConns = 5     // Keep 5 connections always open (warm)
```

When a query needs a connection:
1. Check if a free connection exists in the pool → use it
2. If not, and pool is below `MaxConns` → create a new connection
3. If pool is at `MaxConns` → wait until one is returned

This avoids the overhead of creating a new TCP connection + TLS handshake + PostgreSQL authentication for every query (~50-100ms saved per request).

---

**Q34: How do you control the LLM API cost?**

1. **Page cap**: Maximum 10 pages per submission (`MaxPagesPerSubmission = 10`). More pages = more tokens = more cost.
2. **Image compression**: JPEG at quality 85 (not raw PNG). Reduces payload size by ~80%.
3. **Resolution control**: 150 DPI instead of 300 DPI. Halves image dimensions (and token count for vision models).
4. **Temperature: 0**: Deterministic output. No random token sampling = slightly fewer tokens generated.
5. **Model selection via env var**: Can switch to a cheaper model without code change (`OPENROUTER_MODEL`).

---

**Q35: How does the reverse proxy handle large file uploads without running out of memory?**

Three techniques:

1. **FlushInterval: -1**: The proxy streams bytes to the backend as they arrive, instead of buffering the entire upload in memory.
2. **ReadHeaderTimeout (not ReadTimeout)**: Only the headers have a timeout. The body is allowed to stream in at whatever speed the client sends.
3. **ParseMultipartForm(25 << 20)**: The upload service limits in-memory buffering to 25 MB. Files larger than this are rejected.

---

### Error Handling

---

**Q36: What happens when S3 upload succeeds but the database insert fails?**

The upload handler explicitly handles this case:

```go
if err := uploadService.CreateSubmissionWithEvent(ctx, submission); err != nil {
    // Attempt to clean up S3 file
    if delErr := s3Client.DeleteFile(ctx, s3Key); delErr != nil {
        log.Printf("Failed to clean up S3 file %s: %v", s3Key, delErr)
    }
    respondError(w, ...)
    return
}
```

It tries to delete the orphaned S3 file. If that also fails, the file remains in S3 (orphaned), but at least the user gets a clear error and can retry. An operational alert should watch for orphaned S3 files.

---

**Q37: How does the grader mark submissions as failed?**

The `failSubmission` helper updates the database:

```go
func (s *Service) failSubmission(ctx context.Context, submissionID, errorMessage string) {
    _ = s.repo.MarkSubmissionFailed(ctx, submissionID, errorMessage)
}
```

This sets `status = 'failed'` and `error_message = '<details>'`. The frontend displays this to the user on the results page with a red "Grading failed" card and the error description.

---

**Q38: Why does the upload handler have both `withTimeout` and `withRecovery` middleware?**

```go
http.HandleFunc("POST /upload", withTimeout(withRecovery(handleUpload)))
```

- `withRecovery`: Catches panics (e.g., nil pointer dereference) and returns a 500 JSON error instead of crashing the entire process.
- `withTimeout`: Adds a 4-minute deadline to the request context. If the upload + S3 write takes longer than 4 minutes, the context is cancelled and the handler returns gracefully.

They're applied in this order: `withTimeout(withRecovery(handler))` — the timeout wraps the recovery, so both the recovery logic and the handler are subject to the deadline.

---

### Scalability

---

**Q39: How would you scale this system to handle 10,000 submissions per hour?**

1. **Upload Service**: Run 3-5 instances behind a load balancer. Stateless, so horizontal scaling is trivial.
2. **Grader Service**: The bottleneck. Run 10-20 instances, each consuming from Kafka. Increase Kafka partitions to match.
3. **Results Service**: Run 2-3 instances. Add Redis caching for frequently accessed results.
4. **PostgreSQL**: Add read replicas for the Results Service. Primary handles writes only.
5. **S3**: Already infinitely scalable. No changes needed.
6. **Kafka**: Increase partitions for `paper-uploaded` topic. Each partition is consumed by exactly one grader instance.

---

**Q40: What would you change if this system needed real-time result updates?**

Replace the 5-second polling with **Server-Sent Events (SSE)** or **WebSockets**:

1. The grader publishes a `paper-graded` event to Kafka (already implemented)
2. Add a new **Notification Service** that consumes `paper-graded` events
3. This service maintains SSE connections to browsers
4. When a grade is ready, push the event to the connected browser instantly

This reduces latency from 0-5 seconds (polling interval) to near-instant, and eliminates unnecessary API calls while papers are processing.

---

*End of guide. This document should serve as both a learning reference and an interview preparation resource for explaining every aspect of the AI Grader system.*
