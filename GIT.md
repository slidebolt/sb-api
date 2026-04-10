# Git Workflow for sb-api

This repository contains the Slidebolt API service, which provides the primary REST and WebSocket interfaces for external clients and MCP tools. It produces a standalone binary.

## Dependencies
- **Internal:**
  - `sb-contract`: Core interfaces and shared structures.
  - `sb-domain`: Shared domain models for entities and commands.
  - `sb-logging`: Central logging implementation.
  - `sb-logging-sdk`: Client interfaces for logging.
  - `sb-messenger-sdk`: Shared messaging interfaces.
  - `sb-runtime`: Core execution environment.
  - `sb-storage-sdk`: Shared storage interfaces.
- **External:** 
  - `github.com/danielgtaylor/huma/v2`: REST API framework.
  - `github.com/go-chi/chi/v5`: HTTP router.
  - `github.com/gorilla/websocket`: WebSocket support.
  - `github.com/mark3labs/mcp-go`: Model Context Protocol (MCP) support.

## Build Process
- **Type:** Go Application (Service).
- **Consumption:** Run as the primary API gateway for Slidebolt.
- **Artifacts:** Produces a binary named `sb-api`.
- **Command:** `go build -o sb-api ./cmd/sb-api`
- **Validation:** 
  - Validated through unit tests: `go test -v ./...`
  - Validated by successful compilation of the binary.

## Pre-requisites & Publishing
As the primary API gateway, `sb-api` must be updated whenever any of the core domain, messaging, storage, or logging SDKs are changed.

**Before publishing:**
1. Determine current tag: `git tag | sort -V | tail -n 1`
2. Ensure all local tests pass: `go test -v ./...`
3. Ensure the binary builds: `go build -o sb-api ./cmd/sb-api`

**Publishing Order:**
1. Ensure all internal dependencies are tagged and pushed.
2. Update `sb-api/go.mod` to reference the latest tags.
3. Determine next semantic version for `sb-api` (e.g., `v1.0.5`).
4. Commit and push the changes to `main`.
5. Tag the repository: `git tag v1.0.5`.
6. Push the tag: `git push origin main v1.0.5`.

## Update Workflow & Verification
1. **Modify:** Update API routes in `internal/routes/` or service logic in `app/`.
2. **Verify Local:**
   - Run `go mod tidy`.
   - Run `go test ./...`.
   - Run `go build -o sb-api ./cmd/sb-api`.
3. **Commit:** Ensure the commit message clearly describes the API change.
4. **Tag & Push:** (Follow the Publishing Order above).
