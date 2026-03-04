# AI Grader — Complete AWS Deployment Plan

> **Application**: AI Grader (Go microservices + React SPA)  
> **Date**: March 2026  
> **Estimated timeline**: 2–3 weeks for full production setup

---

## Table of Contents

1. [Architecture Overview](#1-architecture-overview)
2. [AWS Services Map](#2-aws-services-map)
3. [Phase 1 — Foundation (VPC, IAM, Secrets)](#3-phase-1--foundation)
4. [Phase 2 — Data Layer (RDS, MSK, S3)](#4-phase-2--data-layer)
5. [Phase 3 — Container Platform (ECR, ECS Fargate)](#5-phase-3--container-platform)
6. [Phase 4 — Networking & Load Balancer (ALB, Route 53, ACM)](#6-phase-4--networking--load-balancer)
7. [Phase 5 — Frontend Deployment (S3, CloudFront)](#7-phase-5--frontend-deployment)
8. [Phase 6 — CI/CD Pipeline (GitHub Actions)](#8-phase-6--cicd-pipeline)
9. [Phase 7 — Monitoring & Logging](#9-phase-7--monitoring--logging)
10. [Phase 8 — Security Hardening](#10-phase-8--security-hardening)
11. [Dockerfiles to Create](#11-dockerfiles-to-create)
12. [Environment Variables Reference](#12-environment-variables-reference)
13. [Cost Estimate](#13-cost-estimate)
14. [Budget Alternative (Single EC2)](#14-budget-alternative)
15. [Pre-Deployment Checklist](#15-pre-deployment-checklist)
16. [Runbook: Step-by-Step Commands](#16-runbook)

---

## 1. Architecture Overview

### Current Services (from docker-compose.yml)

| Service | Binary | Port | Role |
|---------|--------|------|------|
| **API Gateway** | `cmd/api` | 8080 | Auth (JWT), reverse proxy to upload + results services |
| **Upload Service** | `cmd/upload` | 8081 | Receives PDF, stores in S3, inserts outbox event, publishes to Kafka |
| **Grader Service** | `cmd/grader` | — (no HTTP) | Kafka consumer, downloads PDF from S3, extracts text (MuPDF/CGO), calls OpenRouter AI, saves grade |
| **Results Service** | `cmd/results` | 8083 | Serves grading results (GET /results, GET /results/:id) |
| **React Frontend** | `web/` | — (static) | Vite + Tailwind SPA, talks to API Gateway |

### Dependencies

| Dependency | Current (docker-compose) | Production Target |
|------------|--------------------------|-------------------|
| PostgreSQL 16 | Container | **Amazon RDS** |
| Kafka + Zookeeper | Confluent containers | **Amazon MSK** |
| S3 | AWS S3 (already) | AWS S3 (no change) |
| OpenRouter API | External HTTP | External HTTP (no change) |

### Request Flow

```
Browser → CloudFront (React SPA)
              ↓ API calls
          ALB (HTTPS:443)
              ↓ path routing
          API Gateway (:8080)
            ├── /auth/*        → handles directly (JWT + PostgreSQL)
            ├── /upload/*      → reverse proxy → Upload Service (:8081)
            │                      ├── S3.PutObject
            │                      ├── INSERT outbox_events
            │                      └── Kafka.Produce("paper-uploaded")
            └── /results/*     → reverse proxy → Results Service (:8083)
                                    └── SELECT grades/submissions

          Grader Service (background worker)
              ├── Kafka.Consume("paper-uploaded")
              ├── S3.GetObject → PDF text extraction (MuPDF)
              ├── OpenRouter AI → grade + feedback
              ├── INSERT grades
              └── Kafka.Produce("paper-graded")
```

---

## 2. AWS Services Map

```
┌────────────────────────────────────────────────────────────────┐
│                       AWS Account                              │
│                                                                │
│  ┌──────────────┐  ┌──────────────┐  ┌──────────────────────┐  │
│  │  Route 53    │  │  ACM         │  │  Secrets Manager     │  │
│  │  DNS zone    │  │  SSL cert    │  │  DB pass, JWT, API   │  │
│  └──────┬───────┘  └──────┬───────┘  └──────────────────────┘  │
│         │                 │                                     │
│  ┌──────▼─────────────────▼────────────────────────────────┐   │
│  │              VPC  (10.0.0.0/16)                         │   │
│  │                                                         │   │
│  │  ┌───────────── Public Subnets (2 AZs) ──────────────┐  │   │
│  │  │  ALB (internet-facing)     NAT Gateway (×2)       │  │   │
│  │  └───────────────────┬───────────────────────────────┘  │   │
│  │                      │                                  │   │
│  │  ┌───────────── Private Subnets (2 AZs) ─────────────┐  │   │
│  │  │                                                    │  │   │
│  │  │  ECS Fargate Tasks:                                │  │   │
│  │  │  ┌─────────────┐ ┌────────────┐ ┌──────────────┐   │  │   │
│  │  │  │ API Gateway │ │ Upload Svc │ │ Results Svc  │   │  │   │
│  │  │  │ (2 tasks)   │ │ (2 tasks)  │ │ (2 tasks)    │   │  │   │
│  │  │  └─────────────┘ └────────────┘ └──────────────┘   │  │   │
│  │  │  ┌──────────────┐                                  │  │   │
│  │  │  │ Grader Svc   │ ← Kafka consumer, no ALB target  │  │   │
│  │  │  │ (2 tasks)    │                                  │  │   │
│  │  │  └──────────────┘                                  │  │   │
│  │  │                                                    │  │   │
│  │  │  ┌──────────────┐ ┌──────────────┐                 │  │   │
│  │  │  │ RDS Postgres │ │ Amazon MSK   │                 │  │   │
│  │  │  │ (Multi-AZ)   │ │ (2 brokers)  │                 │  │   │
│  │  │  └──────────────┘ └──────────────┘                 │  │   │
│  │  └────────────────────────────────────────────────────┘  │   │
│  └─────────────────────────────────────────────────────────┘   │
│                                                                │
│  ┌──────────────┐ ┌───────────────┐ ┌────────────────────────┐  │
│  │ S3 (PDFs)    │ │ S3 (Frontend) │ │ ECR (Docker images)    │  │
│  └──────────────┘ │ + CloudFront  │ └────────────────────────┘  │
│                   └───────────────┘                             │
└────────────────────────────────────────────────────────────────┘
```

---

## 3. Phase 1 — Foundation

### 3.1 VPC

```
VPC CIDR: 10.0.0.0/16

Subnets:
  Public  Subnet A: 10.0.1.0/24   (us-east-1a)  — ALB, NAT GW
  Public  Subnet B: 10.0.2.0/24   (us-east-1b)  — ALB, NAT GW
  Private Subnet A: 10.0.10.0/24  (us-east-1a)  — ECS, RDS, MSK
  Private Subnet B: 10.0.20.0/24  (us-east-1b)  — ECS, RDS, MSK

Internet Gateway: 1
NAT Gateways:     2 (one per AZ for HA — or 1 to save cost)
Route Tables:
  Public  → 0.0.0.0/0 → IGW
  Private → 0.0.0.0/0 → NAT GW (same AZ)
```

### 3.2 IAM Roles

| Role | Attached To | Policies |
|------|-------------|----------|
| `ecsTaskExecutionRole` | All ECS tasks | `AmazonECSTaskExecutionRolePolicy`, Secrets Manager read |
| `apiGatewayTaskRole` | API Gateway task | Secrets Manager read |
| `uploadTaskRole` | Upload Service task | `s3:PutObject`, `s3:GetObject` on PDF bucket, Secrets Manager read |
| `graderTaskRole` | Grader Service task | `s3:GetObject` on PDF bucket, Secrets Manager read |
| `resultsTaskRole` | Results Service task | Secrets Manager read |
| `githubActionsRole` | GitHub OIDC | ECR push, ECS deploy, S3 sync, CloudFront invalidate |

> **Key point**: Use IAM Task Roles instead of `AWS_ACCESS_KEY_ID`/`AWS_SECRET_ACCESS_KEY` environment variables. ECS tasks automatically get credentials via the task metadata endpoint. This eliminates static credentials.

### 3.3 Secrets Manager

Store these secrets (referenced by ECS task definitions):

| Secret Name | Value |
|-------------|-------|
| `ai-grader/db-url` | `postgresql://aigrader:<password>@<rds-endpoint>:5432/ai_grader?sslmode=require` |
| `ai-grader/jwt-secret` | Random 64-char string |
| `ai-grader/openrouter-api-key` | Your OpenRouter key |

---

## 4. Phase 2 — Data Layer

### 4.1 Amazon RDS (PostgreSQL)

```
Engine:              PostgreSQL 16.x
Instance Class:      db.t3.medium  (2 vCPU, 4 GB RAM)
Storage:             20 GB gp3, auto-scaling up to 100 GB
Multi-AZ:            YES
Encryption:          YES (aws/rds KMS key)
Public Access:       NO
Subnet Group:        Private subnets A + B
Security Group:      Allow TCP 5432 from ECS task security group ONLY
Backup:              7-day retention, daily snapshots
Parameter Group:     Default (pgcrypto extension enabled)
```

**Schema migration**: Run migrations on deploy using a one-off ECS task or a CI/CD step:
```bash
# From CI/CD or a bastion:
psql "$DATABASE_URL" -f migrations/001_schema.sql
```

### 4.2 Amazon MSK (Managed Kafka)

```
Cluster Name:        ai-grader-kafka
Kafka Version:       3.6.x
Broker Instance:     kafka.t3.small (dev) / kafka.m5.large (prod)
Number of Brokers:   2 (one per AZ)
Storage per Broker:  50 GB
Encryption:
  In-transit:        TLS
  At-rest:           YES
Auth:                IAM auth (or SASL/SCRAM)
Security Group:      Allow TCP 9092/9094 from ECS task SG
```

**Topics to create** (via MSK topic auto-creation or CLI):
```
paper-uploaded   — partitions: 3, replication-factor: 2
paper-graded     — partitions: 3, replication-factor: 2
```

> **Code change needed**: Update Kafka broker addresses in env vars to use MSK bootstrap servers (e.g., `b-1.ai-grader.xxxxx.kafka.us-east-1.amazonaws.com:9092,b-2...`). If using IAM auth, update the `kafka-go` dialer to use SASL/IAM mechanism.

### 4.3 Amazon S3

You already use S3. Ensure the bucket is configured:

```
Bucket Name:         ai-grader-papers-<account-id>
Region:              Same as ECS cluster
Versioning:          Enabled
Encryption:          SSE-S3 (AES-256)
Lifecycle Rule:      Transition to IA after 90 days, delete after 365 days
Block Public Access: ALL blocked
CORS:                Not needed (uploads go through Upload Service, not browser)
```

---

## 5. Phase 3 — Container Platform

### 5.1 Amazon ECR — Repositories

Create 4 ECR repositories:
```
ai-grader/api-gateway
ai-grader/upload-service
ai-grader/grader-service
ai-grader/results-service
```

Enable image scanning on push. Set lifecycle policy to keep last 10 images.

### 5.2 ECS Cluster

```
Cluster Name:     ai-grader
Capacity Provider: FARGATE (default), FARGATE_SPOT (for grader to save cost)
```

### 5.3 Task Definitions

#### API Gateway Task

```json
{
  "family": "api-gateway",
  "networkMode": "awsvpc",
  "requiresCompatibilities": ["FARGATE"],
  "cpu": "256",
  "memory": "512",
  "executionRoleArn": "arn:aws:iam::role/ecsTaskExecutionRole",
  "taskRoleArn": "arn:aws:iam::role/apiGatewayTaskRole",
  "containerDefinitions": [{
    "name": "api-gateway",
    "image": "<account>.dkr.ecr.<region>.amazonaws.com/ai-grader/api-gateway:latest",
    "portMappings": [{ "containerPort": 8080, "protocol": "tcp" }],
    "environment": [
      { "name": "GATEWAY_PORT", "value": "8080" },
      { "name": "UPLOAD_SERVICE_URL", "value": "http://upload-service.ai-grader.local:8081" },
      { "name": "RESULTS_SERVICE_URL", "value": "http://results-service.ai-grader.local:8083" }
    ],
    "secrets": [
      { "name": "DATABASE_URL", "valueFrom": "arn:aws:secretsmanager:...:ai-grader/db-url" },
      { "name": "JWT_SECRET", "valueFrom": "arn:aws:secretsmanager:...:ai-grader/jwt-secret" }
    ],
    "healthCheck": {
      "command": ["CMD-SHELL", "curl -f http://localhost:8080/health || exit 1"],
      "interval": 30,
      "timeout": 5,
      "retries": 3,
      "startPeriod": 10
    },
    "logConfiguration": {
      "logDriver": "awslogs",
      "options": {
        "awslogs-group": "/ecs/ai-grader/api-gateway",
        "awslogs-region": "us-east-1",
        "awslogs-stream-prefix": "ecs"
      }
    }
  }]
}
```

#### Upload Service Task

```json
{
  "family": "upload-service",
  "cpu": "512",
  "memory": "1024",
  "taskRoleArn": "arn:aws:iam::role/uploadTaskRole",
  "containerDefinitions": [{
    "name": "upload-service",
    "image": "<account>.dkr.ecr.<region>.amazonaws.com/ai-grader/upload-service:latest",
    "portMappings": [{ "containerPort": 8081 }],
    "environment": [
      { "name": "UPLOAD_SERVICE_PORT", "value": "8081" },
      { "name": "S3_BUCKET_NAME", "value": "ai-grader-papers-<account-id>" },
      { "name": "AWS_REGION", "value": "us-east-1" },
      { "name": "KAFKA_BROKERS", "value": "<msk-bootstrap-servers>" },
      { "name": "KAFKA_TOPIC", "value": "paper-uploaded" }
    ],
    "secrets": [
      { "name": "DATABASE_URL", "valueFrom": "arn:aws:secretsmanager:...:ai-grader/db-url" }
    ]
  }]
}
```

> **Note**: Remove `AWS_ACCESS_KEY_ID` and `AWS_SECRET_ACCESS_KEY` from env vars. The `uploadTaskRole` IAM role provides S3 access automatically. The `s3.NewClient()` code should be updated to use default credential chain instead of explicit keys.

#### Grader Service Task

```json
{
  "family": "grader-service",
  "cpu": "1024",
  "memory": "2048",
  "taskRoleArn": "arn:aws:iam::role/graderTaskRole",
  "containerDefinitions": [{
    "name": "grader-service",
    "image": "<account>.dkr.ecr.<region>.amazonaws.com/ai-grader/grader-service:latest",
    "environment": [
      { "name": "KAFKA_BROKERS", "value": "<msk-bootstrap-servers>" },
      { "name": "KAFKA_TOPIC", "value": "paper-uploaded" },
      { "name": "KAFKA_GRADED_TOPIC", "value": "paper-graded" },
      { "name": "KAFKA_CONSUMER_GROUP_ID", "value": "grader-consumer-group" },
      { "name": "S3_BUCKET_NAME", "value": "ai-grader-papers-<account-id>" },
      { "name": "AWS_REGION", "value": "us-east-1" },
      { "name": "OPENROUTER_MODEL", "value": "x-ai/grok-4.1-fast" }
    ],
    "secrets": [
      { "name": "DATABASE_URL", "valueFrom": "arn:aws:secretsmanager:...:ai-grader/db-url" },
      { "name": "OPENROUTER_API_KEY", "valueFrom": "arn:aws:secretsmanager:...:ai-grader/openrouter-api-key" }
    ]
  }]
}
```

> The grader uses CGO (MuPDF). The existing `Dockerfile.grader` multi-stage build handles this correctly — it builds with `libmupdf-dev` and runs on `debian:bookworm-slim` with shared libraries.

#### Results Service Task

```json
{
  "family": "results-service",
  "cpu": "256",
  "memory": "512",
  "containerDefinitions": [{
    "name": "results-service",
    "image": "<account>.dkr.ecr.<region>.amazonaws.com/ai-grader/results-service:latest",
    "portMappings": [{ "containerPort": 8083 }],
    "environment": [
      { "name": "RESULTS_SERVICE_PORT", "value": "8083" }
    ],
    "secrets": [
      { "name": "DATABASE_URL", "valueFrom": "arn:aws:secretsmanager:...:ai-grader/db-url" }
    ]
  }]
}
```

### 5.4 ECS Services

| Service | Desired Count | Min | Max | Capacity Provider | Service Discovery |
|---------|---------------|-----|-----|-------------------|-------------------|
| api-gateway | 2 | 2 | 6 | FARGATE | `api-gateway.ai-grader.local` |
| upload-service | 2 | 2 | 6 | FARGATE | `upload-service.ai-grader.local` |
| grader-service | 2 | 1 | 10 | FARGATE_SPOT | `grader-service.ai-grader.local` |
| results-service | 2 | 2 | 4 | FARGATE | `results-service.ai-grader.local` |

**Service Discovery**: Use AWS Cloud Map to create a private DNS namespace `ai-grader.local` so services can reach each other by name (e.g., the API Gateway proxies to `http://upload-service.ai-grader.local:8081`).

**Auto Scaling Policies**:
- **api-gateway**: Scale on ALB request count (target: 1000 req/min/task)
- **upload-service**: Scale on ALB request count (target: 200 req/min/task)
- **grader-service**: Scale on Kafka consumer lag (custom CloudWatch metric)
- **results-service**: Scale on ALB request count (target: 2000 req/min/task)

### 5.5 Security Groups

```
SG: alb-sg
  Inbound:  TCP 443 from 0.0.0.0/0, TCP 80 from 0.0.0.0/0
  Outbound: TCP 8080 to ecs-sg

SG: ecs-sg
  Inbound:  TCP 8080,8081,8083 from alb-sg
  Outbound: TCP 5432 to rds-sg, TCP 9092 to msk-sg, TCP 443 to 0.0.0.0/0 (S3/OpenRouter)

SG: rds-sg
  Inbound:  TCP 5432 from ecs-sg
  Outbound: None

SG: msk-sg
  Inbound:  TCP 9092,9094 from ecs-sg
  Outbound: None
```

---

## 6. Phase 4 — Networking & Load Balancer

### 6.1 ACM Certificate

```
Domain:        grader.yourdomain.com
SANs:          *.grader.yourdomain.com (optional)
Validation:    DNS (add CNAME to Route 53)
```

### 6.2 Application Load Balancer

```
Name:           ai-grader-alb
Scheme:         Internet-facing
Subnets:        Public Subnet A + B
Security Group: alb-sg

Listeners:
  HTTP  :80  → Redirect to HTTPS :443
  HTTPS :443 → Forward (rules below), certificate from ACM

Target Groups:
  tg-api-gateway:
    Port:         8080
    Protocol:     HTTP
    Target Type:  IP (Fargate)
    Health Check: GET /health → 200, interval 30s, threshold 2
    Deregistration Delay: 30s

Listener Rules (HTTPS:443):
  Priority 1: Path /api/*     → tg-api-gateway
  Priority 2: Path /auth/*    → tg-api-gateway
  Priority 3: Path /health    → tg-api-gateway
  Default:    Fixed 404 (or redirect to CloudFront)
```

> **Important architectural note**: Your API Gateway already reverse-proxies to upload-service and results-service. So the ALB only needs ONE target group pointing at the API Gateway. Internal service-to-service traffic uses Cloud Map DNS (`upload-service.ai-grader.local`), not the ALB.

### 6.3 Route 53

```
Hosted Zone:   yourdomain.com

Records:
  api.grader.yourdomain.com    → ALIAS → ALB DNS name
  grader.yourdomain.com        → ALIAS → CloudFront distribution
```

---

## 7. Phase 5 — Frontend Deployment

### 7.1 Build Configuration

Update `web/vite.config.js` for production:
```js
export default defineConfig({
  plugins: [react()],
  server: {
    // dev only
    proxy: { '/auth': 'http://localhost:8080', ... },
  },
})
```

The frontend uses `const BASE = ''` in `client.js`, meaning API calls go to the same origin. In production, configure CloudFront to route `/auth/*`, `/upload/*`, `/results/*`, `/health` to the ALB origin, and everything else to the S3 origin.

### 7.2 S3 Bucket (Frontend)

```
Bucket Name:         ai-grader-frontend-<account-id>
Static Hosting:      Not needed (CloudFront handles it)
Block Public Access: ALL blocked (CloudFront OAC provides access)
```

### 7.3 CloudFront Distribution

```
Origins:
  1. S3 Origin (Default):
     Domain: ai-grader-frontend-<account-id>.s3.us-east-1.amazonaws.com
     Access: Origin Access Control (OAC)
     
  2. ALB Origin (API):
     Domain: ai-grader-alb-xxxxx.us-east-1.elb.amazonaws.com
     Protocol: HTTPS only
     Custom Headers: (none)

Behaviors:
  /auth/*      → ALB origin, Allowed Methods: GET,HEAD,OPTIONS,PUT,POST,PATCH,DELETE
                  Cache Policy: CachingDisabled
                  Origin Request Policy: AllViewer

  /upload/*    → ALB origin, same as above

  /results/*   → ALB origin, same as above

  /health      → ALB origin, same as above

  /* (Default) → S3 origin, Allowed Methods: GET,HEAD
                  Cache Policy: CachingOptimized
                  Custom Error Response: 403/404 → /index.html (200) ← SPA routing

Alternate Domain:  grader.yourdomain.com
SSL Certificate:   ACM cert (must be in us-east-1 for CloudFront)
HTTP/2:            Enabled
Price Class:       PriceClass_100 (US/EU) to save cost
```

---

## 8. Phase 6 — CI/CD Pipeline

### 8.1 GitHub Actions Workflow

```yaml
# .github/workflows/deploy.yml
name: Build & Deploy

on:
  push:
    branches: [main]
  pull_request:
    branches: [main]

permissions:
  id-token: write   # for OIDC
  contents: read

env:
  AWS_REGION: us-east-1
  ECR_REGISTRY: ${{ secrets.AWS_ACCOUNT_ID }}.dkr.ecr.us-east-1.amazonaws.com
  ECS_CLUSTER: ai-grader

jobs:
  # ────────────────────────── TEST ──────────────────────────
  test:
    runs-on: ubuntu-latest
    services:
      postgres:
        image: postgres:16.6
        env:
          POSTGRES_USER: test
          POSTGRES_PASSWORD: test
          POSTGRES_DB: ai_grader_test
        ports: ["5432:5432"]
        options: >-
          --health-cmd pg_isready
          --health-interval 10s
          --health-timeout 5s
          --health-retries 5
    steps:
      - uses: actions/checkout@v4
      - uses: actions/setup-go@v5
        with:
          go-version: '1.25'

      - name: Run Go tests
        env:
          DATABASE_URL: postgresql://test:test@localhost:5432/ai_grader_test?sslmode=disable
        run: go test ./... -v -race -count=1

      - name: Lint frontend
        working-directory: web
        run: |
          npm ci
          npm run build

  # ────────────────────── BUILD IMAGES ──────────────────────
  build:
    needs: test
    if: github.ref == 'refs/heads/main' && github.event_name == 'push'
    runs-on: ubuntu-latest
    strategy:
      matrix:
        include:
          - service: api-gateway
            dockerfile: Dockerfile.api
          - service: upload-service
            dockerfile: Dockerfile.upload
          - service: grader-service
            dockerfile: Dockerfile.grader
          - service: results-service
            dockerfile: Dockerfile.results
    steps:
      - uses: actions/checkout@v4

      - name: Configure AWS credentials (OIDC)
        uses: aws-actions/configure-aws-credentials@v4
        with:
          role-to-assume: arn:aws:iam::${{ secrets.AWS_ACCOUNT_ID }}:role/github-actions-role
          aws-region: ${{ env.AWS_REGION }}

      - name: Login to ECR
        uses: aws-actions/amazon-ecr-login@v2

      - name: Build and push
        run: |
          IMAGE=${{ env.ECR_REGISTRY }}/ai-grader/${{ matrix.service }}
          docker build -t $IMAGE:${{ github.sha }} -t $IMAGE:latest \
            -f ${{ matrix.dockerfile }} .
          docker push $IMAGE --all-tags

  # ───────────────────── DEPLOY BACKEND ─────────────────────
  deploy-backend:
    needs: build
    runs-on: ubuntu-latest
    strategy:
      max-parallel: 2
      matrix:
        service: [api-gateway, upload-service, grader-service, results-service]
    steps:
      - name: Configure AWS credentials
        uses: aws-actions/configure-aws-credentials@v4
        with:
          role-to-assume: arn:aws:iam::${{ secrets.AWS_ACCOUNT_ID }}:role/github-actions-role
          aws-region: ${{ env.AWS_REGION }}

      - name: Force new ECS deployment
        run: |
          aws ecs update-service \
            --cluster ${{ env.ECS_CLUSTER }} \
            --service ${{ matrix.service }} \
            --force-new-deployment

      - name: Wait for service stability
        run: |
          aws ecs wait services-stable \
            --cluster ${{ env.ECS_CLUSTER }} \
            --services ${{ matrix.service }}

  # ──────────────────── DEPLOY FRONTEND ─────────────────────
  deploy-frontend:
    needs: test
    if: github.ref == 'refs/heads/main' && github.event_name == 'push'
    runs-on: ubuntu-latest
    steps:
      - uses: actions/checkout@v4

      - uses: actions/setup-node@v4
        with:
          node-version: '20'
          cache: 'npm'
          cache-dependency-path: web/package-lock.json

      - name: Build
        working-directory: web
        run: |
          npm ci
          npm run build

      - name: Configure AWS credentials
        uses: aws-actions/configure-aws-credentials@v4
        with:
          role-to-assume: arn:aws:iam::${{ secrets.AWS_ACCOUNT_ID }}:role/github-actions-role
          aws-region: ${{ env.AWS_REGION }}

      - name: Sync to S3 & invalidate CloudFront
        run: |
          aws s3 sync web/dist/ s3://ai-grader-frontend-${{ secrets.AWS_ACCOUNT_ID }}/ --delete
          aws cloudfront create-invalidation \
            --distribution-id ${{ secrets.CLOUDFRONT_DIST_ID }} \
            --paths "/*"

  # ─────────────────── DB MIGRATION ─────────────────────────
  migrate:
    needs: build
    runs-on: ubuntu-latest
    steps:
      - uses: actions/checkout@v4

      - name: Configure AWS credentials
        uses: aws-actions/configure-aws-credentials@v4
        with:
          role-to-assume: arn:aws:iam::${{ secrets.AWS_ACCOUNT_ID }}:role/github-actions-role
          aws-region: ${{ env.AWS_REGION }}

      - name: Run migration via ECS RunTask
        run: |
          aws ecs run-task \
            --cluster ${{ env.ECS_CLUSTER }} \
            --task-definition db-migrate \
            --launch-type FARGATE \
            --network-configuration '{
              "awsvpcConfiguration": {
                "subnets": ["subnet-private-a","subnet-private-b"],
                "securityGroups": ["sg-ecs"],
                "assignPublicIp": "DISABLED"
              }
            }'
```

### 8.2 GitHub Secrets to Configure

| Secret | Value |
|--------|-------|
| `AWS_ACCOUNT_ID` | Your 12-digit AWS account ID |
| `CLOUDFRONT_DIST_ID` | CloudFront distribution ID |

> Use GitHub OIDC + IAM role (no static AWS keys in GitHub).

### 8.3 Branch Strategy

```
main          → auto-deploy to production
staging       → auto-deploy to staging environment (optional)
feature/*     → run tests only (PR checks)
```

---

## 9. Phase 7 — Monitoring & Logging

### 9.1 CloudWatch

| Resource | Metric | Alarm Threshold |
|----------|--------|-----------------|
| ALB | `HTTPCode_Target_5XX_Count` | > 10 in 5 min |
| ALB | `TargetResponseTime` | p99 > 5s |
| ECS (each service) | `CPUUtilization` | > 80% sustained 5 min |
| ECS (each service) | `MemoryUtilization` | > 85% sustained 5 min |
| RDS | `CPUUtilization` | > 80% |
| RDS | `FreeStorageSpace` | < 5 GB |
| RDS | `DatabaseConnections` | > 80% of max |
| MSK | `OffsetLag` (grader group) | > 100 for 5 min |

### 9.2 Log Groups

```
/ecs/ai-grader/api-gateway       ← API Gateway container logs
/ecs/ai-grader/upload-service    ← Upload Service logs
/ecs/ai-grader/grader-service    ← Grader Service logs (most important)
/ecs/ai-grader/results-service   ← Results Service logs
```

Retention: 30 days. Set up Log Insights queries for common patterns:
```
# Failed grading attempts
fields @timestamp, @message
| filter @message like /error|Error|FATAL/
| sort @timestamp desc
| limit 50
```

### 9.3 Alerts

- **SNS Topic**: `ai-grader-alerts`
- **Subscribers**: Email (your address), optionally Slack via AWS Chatbot
- All CloudWatch alarms → SNS → you get notified

---

## 10. Phase 8 — Security Hardening

### Checklist

- [ ] **No static AWS keys in code or env vars** — use IAM Task Roles
- [ ] **Secrets Manager** for DB password, JWT secret, OpenRouter API key
- [ ] **RDS encryption at rest** (KMS)
- [ ] **RDS SSL connections** (`sslmode=require` in DATABASE_URL)
- [ ] **S3 bucket policy**: deny non-SSL, block public access
- [ ] **MSK encryption** in-transit (TLS) and at-rest
- [ ] **ALB → HTTPS only** (redirect HTTP to HTTPS)
- [ ] **Security groups**: minimally permissive (documented above)
- [ ] **VPC Flow Logs** → CloudWatch (detect suspicious traffic)
- [ ] **ECS exec disabled** in production (or audit-logged)
- [ ] **ECR image scanning** on push
- [ ] **Dependabot** or `govulncheck` in CI for dependency vulnerabilities
- [ ] **WAF on ALB** (optional): rate limiting, SQL injection protection
- [ ] **CloudFront geo-restriction** (optional): serve only needed regions

### Code Changes Required for Production

1. **Remove hardcoded AWS credentials** from Upload & Grader services:
   ```go
   // BEFORE (current code in cmd/upload/main.go)
   s3Client, err = s3.NewClient(awsRegion, awsAccessKey, awsSecretKey, s3BucketName)
   
   // AFTER (use default credential chain — picks up IAM Task Role)
   s3Client, err = s3.NewClientWithDefaultCredentials(awsRegion, s3BucketName)
   ```

2. **Add graceful shutdown to API Gateway** (currently missing `signal.Notify`):
   ```go
   // cmd/api/main.go needs a graceful shutdown handler
   // like cmd/results/main.go already has
   ```

3. **Add health endpoints** — already present:
   - API Gateway: `GET /health` ✅
   - Results Service: `GET /health` ✅
   - Upload Service: needs a `/health` endpoint
   - Grader Service: no HTTP server (Kafka consumer only — ECS health check via process liveness)

---

## 11. Dockerfiles to Create

You already have `Dockerfile.grader`. Create 3 more:

### Dockerfile.api
```dockerfile
FROM golang:1.25-bookworm AS builder
WORKDIR /app
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 GOOS=linux go build -o /api-gateway ./cmd/api

FROM gcr.io/distroless/static-debian12
COPY --from=builder /api-gateway /api-gateway
EXPOSE 8080
ENTRYPOINT ["/api-gateway"]
```

### Dockerfile.upload
```dockerfile
FROM golang:1.25-bookworm AS builder
WORKDIR /app
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 GOOS=linux go build -o /upload-service ./cmd/upload

FROM gcr.io/distroless/static-debian12
COPY --from=builder /upload-service /upload-service
EXPOSE 8081
ENTRYPOINT ["/upload-service"]
```

### Dockerfile.results
```dockerfile
FROM golang:1.25-bookworm AS builder
WORKDIR /app
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 GOOS=linux go build -o /results-service ./cmd/results

FROM gcr.io/distroless/static-debian12
COPY --from=builder /results-service /results-service
EXPOSE 8083
ENTRYPOINT ["/results-service"]
```

> **Note**: API Gateway, Upload, and Results services use `CGO_ENABLED=0` (pure Go) so they can run on `distroless/static`. The Grader requires CGO for MuPDF and uses `debian:bookworm-slim`.

---

## 12. Environment Variables Reference

### API Gateway
| Variable | Source | Example |
|----------|--------|---------|
| `DATABASE_URL` | Secrets Manager | `postgresql://...` |
| `JWT_SECRET` | Secrets Manager | `<random-64-chars>` |
| `GATEWAY_PORT` | Task Definition | `8080` |
| `UPLOAD_SERVICE_URL` | Task Definition | `http://upload-service.ai-grader.local:8081` |
| `RESULTS_SERVICE_URL` | Task Definition | `http://results-service.ai-grader.local:8083` |

### Upload Service
| Variable | Source | Example |
|----------|--------|---------|
| `DATABASE_URL` | Secrets Manager | `postgresql://...` |
| `UPLOAD_SERVICE_PORT` | Task Definition | `8081` |
| `AWS_REGION` | Task Definition | `us-east-1` |
| `S3_BUCKET_NAME` | Task Definition | `ai-grader-papers-123456789012` |
| `KAFKA_BROKERS` | Task Definition | `b-1.xxx.kafka.us-east-1.amazonaws.com:9092,...` |
| `KAFKA_TOPIC` | Task Definition | `paper-uploaded` |

### Grader Service
| Variable | Source | Example |
|----------|--------|---------|
| `DATABASE_URL` | Secrets Manager | `postgresql://...` |
| `OPENROUTER_API_KEY` | Secrets Manager | `sk-or-...` |
| `OPENROUTER_MODEL` | Task Definition | `x-ai/grok-4.1-fast` |
| `GLOBAL_GRADING_RUBRIC` | Task Definition | *(optional rubric text)* |
| `AWS_REGION` | Task Definition | `us-east-1` |
| `S3_BUCKET_NAME` | Task Definition | `ai-grader-papers-123456789012` |
| `KAFKA_BROKERS` | Task Definition | MSK bootstrap servers |
| `KAFKA_TOPIC` | Task Definition | `paper-uploaded` |
| `KAFKA_GRADED_TOPIC` | Task Definition | `paper-graded` |
| `KAFKA_CONSUMER_GROUP_ID` | Task Definition | `grader-consumer-group` |

### Results Service
| Variable | Source | Example |
|----------|--------|---------|
| `DATABASE_URL` | Secrets Manager | `postgresql://...` |
| `RESULTS_SERVICE_PORT` | Task Definition | `8083` |

---

## 13. Cost Estimate

### Production (ECS Fargate + Managed Services)

| Service | Spec | Monthly Cost |
|---------|------|-------------|
| ECS Fargate (8 tasks total) | Mixed vCPU/memory | $80–$160 |
| RDS PostgreSQL | db.t3.medium, Multi-AZ | $70–$140 |
| Amazon MSK | kafka.t3.small × 2 brokers | $70–$130 |
| ALB | 1 ALB + LCU hours | $20–$40 |
| S3 (PDFs) | <10 GB | $1–$5 |
| S3 (Frontend) | <100 MB | <$1 |
| CloudFront | Low traffic | $1–$10 |
| ECR | <5 GB images | $1–$3 |
| NAT Gateway | 2 (HA) | $65–$70 |
| Secrets Manager | 3 secrets | ~$2 |
| CloudWatch | Logs + metrics | $5–$20 |
| Route 53 | 1 hosted zone | $0.50 |
| **Total** | | **$315–$580/mo** |

### Dev/Staging (cost optimized)

| Optimization | Savings |
|--------------|---------|
| 1 NAT Gateway (not 2) | -$33/mo |
| RDS Single-AZ, db.t3.micro | -$55/mo |
| MSK → self-managed Kafka on Fargate | -$80/mo |
| FARGATE_SPOT for grader | -$20/mo |
| Fewer tasks (1 each) | -$40/mo |
| **Dev Total** | **~$90–$150/mo** |

---

## 14. Budget Alternative (Single EC2)

If this is for learning, demos, or very low traffic:

```
1× EC2 t3.medium         $30/mo     All services via docker-compose
1× RDS db.t3.micro       $15/mo     Single-AZ PostgreSQL
   Kafka on EC2           $0         Self-managed in Docker
   S3 for PDFs            $1/mo
   No ALB — Nginx on EC2  $0         Nginx reverse proxy + Let's Encrypt
   Elastic IP              $0         (free when attached)
   GitHub Actions           $0         Free tier
   ─────────────────────────────
   Total: ~$50/mo
```

Setup:
1. Launch EC2 t3.medium with Amazon Linux 2023
2. Install Docker + Docker Compose
3. Clone repo, create `.env`, run `docker compose up -d`
4. Install Nginx, configure reverse proxy + certbot for SSL
5. Point domain A record to Elastic IP

---

## 15. Pre-Deployment Checklist

### Code changes before deploy

- [ ] Create `Dockerfile.api`, `Dockerfile.upload`, `Dockerfile.results`
- [ ] Add `/health` endpoint to Upload Service
- [ ] Add graceful shutdown to API Gateway (`cmd/api/main.go`)
- [ ] Update `s3.NewClient()` to support default credential chain (IAM roles)
- [ ] Remove hardcoded `godotenv.Load()` calls (or make them non-fatal — most already are)
- [ ] Set `VITE_API_URL` or keep `BASE = ''` in frontend (current setup works with CloudFront)
- [ ] Add `.dockerignore` to exclude `web/node_modules`, `.git`, `bin/`

### AWS setup

- [ ] Register a domain (or use existing) in Route 53
- [ ] Request ACM certificate (us-east-1 for CloudFront + app region for ALB)
- [ ] Create VPC with public/private subnets (use VPC wizard)
- [ ] Create RDS instance and run `001_schema.sql`
- [ ] Create MSK cluster (or self-managed Kafka)
- [ ] Create S3 buckets (PDFs + frontend)
- [ ] Create ECR repositories
- [ ] Create ECS cluster + task definitions + services
- [ ] Create ALB + target groups + listener rules
- [ ] Create CloudFront distribution
- [ ] Configure DNS records
- [ ] Store secrets in Secrets Manager
- [ ] Set up GitHub OIDC + IAM role for CI/CD
- [ ] Configure CloudWatch alarms + SNS alerts

---

## 16. Runbook

### Order of operations

```
Step  1:  Create VPC (subnets, IGW, NAT, route tables)
Step  2:  Create security groups (alb-sg, ecs-sg, rds-sg, msk-sg)
Step  3:  Create RDS PostgreSQL instance
Step  4:  Run database migration (001_schema.sql)
Step  5:  Create MSK cluster (or skip for budget path)
Step  6:  Create Kafka topics (paper-uploaded, paper-graded)
Step  7:  Create S3 buckets (PDFs + frontend)
Step  8:  Create ECR repositories (×4)
Step  9:  Build & push Docker images (locally first)
Step 10:  Create Secrets Manager secrets
Step 11:  Create IAM roles (execution role, 4 task roles)
Step 12:  Create ECS cluster
Step 13:  Create Cloud Map namespace (ai-grader.local)
Step 14:  Create ECS task definitions (×4)
Step 15:  Create ALB + target group + listener
Step 16:  Create ECS services (×4) with service discovery
Step 17:  Verify: curl https://api.grader.yourdomain.com/health
Step 18:  Request ACM cert (us-east-1 for CloudFront)
Step 19:  Build frontend (npm run build)
Step 20:  Upload to S3 frontend bucket
Step 21:  Create CloudFront distribution
Step 22:  Configure Route 53 DNS
Step 23:  Set up GitHub OIDC + Actions workflow
Step 24:  Set up CloudWatch alarms
Step 25:  End-to-end test: register → login → upload PDF → wait → view results
```

### Rollback procedure

```bash
# Roll back to previous ECS task definition revision
aws ecs update-service \
  --cluster ai-grader \
  --service api-gateway \
  --task-definition api-gateway:<previous-revision>

# Roll back frontend
aws s3 sync s3://ai-grader-frontend-backup/ s3://ai-grader-frontend-<account>/
aws cloudfront create-invalidation --distribution-id EXXX --paths "/*"

# Roll back database (if migration was destructive)
# Restore from RDS automated snapshot
aws rds restore-db-instance-to-point-in-time \
  --source-db-instance-identifier ai-grader-db \
  --target-db-instance-identifier ai-grader-db-restored \
  --restore-time <timestamp>
```

---

**This plan covers the complete path from your current docker-compose local setup to a production-grade AWS deployment.** Start with Phase 1–3 (foundation + data), then Phase 4–5 (networking + frontend), and finally Phase 6–8 (CI/CD + monitoring + security).
