# 🧠 Neuron Roadmap: The Path to the AI Operating System

Neuron is not just a package manager; it is a distribution and orchestration layer for AI tools, agents, and models. The goal is to transition from a "Distribution Layer" (v1) to an "Orchestration Layer" (v2) and finally a "Governance Protocol" (v3).

## 🏁 Phase 1: Infrastructure Bedrock (Hardening the MVP)
*Goal: Eliminate friction. Ensure "One command to install, one command to run" works 100% of the time.*

### 🛠️ CLI & Runtime (`cli/`)
- [ ] **Secrets Management**
    - [ ] Implement `neuron secrets set <key> <value>` using `zalando/go-keyring`.
    - [ ] Add "Secret Scoping" (Global vs. Package-Specific: `agents/researcher:KEY`).
- [ ] **Dependency Resolution**
    - [ ] Implement "Pre-flight Check" in `installer.go` to verify runtime environment.
    - [ ] Implement auto-venv creation and `pip install -r requirements.txt` for Python packages.
    - [ ] Implement `neuron.lock` (lockfile) for deterministic environments.
- [ ] **Node.js Runtime**
    - [ ] Create `NodeRuntime` struct in `pkg/runtime/node.go`.
    - [ ] Implement `npm install` and `node entry.js` with JSON `stdin`/`stdout` piping.
- [ ] **DX Improvements**
    - [ ] Implement `neuron update` to upgrade installed packages.
    - [ ] Implement `neuron list` to show installed versions and capabilities.

### 🌐 Registry (`registry/`)
- [ ] **Versioning Logic**
    - [ ] Upgrade `pkg/store` to support SemVer ranges (`^1.0.0`, `~1.2.0`).
    - [ ] Implement version-specific resolution in the API: `GET /v1/packages/:name?version=...`.
- [ ] **Search & Discovery**
    - [ ] Implement an inverted index in `pkg/store/index.go` for faster searching.
    - [ ] Add weighting (Name match > Description match).
    - [ ] Add metadata: Tags, Author, and PublishedDate to the manifest.
- [ ] **Registry Guardrails**
    - [ ] Implement a "Manifest Validator" on `/v1/publish` to reject invalid `neuron.json` files.
    - [ ] Implement basic API Key authentication for publishers.

---

## ⚡ Phase 2: Orchestration Layer (The Intelligence Phase)
*Goal: Solve the "Glue Code" problem. Enable agents to compose other agents.*

### 🧩 Composition Engine (Neuron v2)
- [ ] **`workflow.json` Specification**
    - [ ] Define the manifest for Composite Agents (DAG-based execution).
    - [ ] Implement a workflow runner that pipes outputs from Step A to inputs of Step B.
- [ ] **The "Planner" (Dynamic Composition)**
    - [ ] Implement `neuron solve "<query>"`.
    - [ ] Integrate LLM to analyze local `capability` blocks and generate a temporary execution DAG.
- [ ] **Context Memory Store (Session Bus)**
    - [ ] Implement a lightweight state store (SQLite/Redis) for shared session memory.
    - [ ] Add `neuron_context` permission to allow packages to read/write to the bus.

### 🔌 MCP Integration
- [ ] **Universal MCP Bridge**
    - [ ] Implement `neuron run <package> --mcp`.
    - [ ] Create a JSON-RPC wrapper in `pkg/mcp` to expose Neuron packages as MCP servers.
- [ ] **Auto-Registration**
    - [ ] Implement a scanner to auto-register local Neuron MCP packages with Claude Desktop/Cursor.

---

## 🛡️ Phase 3: Trust & Governance (The Industrial Phase)
*Goal: Enterprise-grade security and verified quality. Shift from "Trust me" to "Verify me."*

### 📉 The Evaluation Layer
- [ ] **`evals` Manifest Block**
    - [ ] Add `evals` section to `neuron.json` (Benchmark name, Golden Set path).
- [ ] **`neuron test` Command**
    - [ ] Implement an execution loop that runs a package against its Golden Set.
    - [ ] Implement "LLM-as-a-judge" to grade the output quality.
- [ ] **Registry Benchmarks**
    - [ ] Display "Verified Performance Scores" on the registry search results.

### 🔐 Zero-Trust Sandbox
- [ ] **Strict Network Proxy**
    - [ ] Implement a runtime proxy to enforce domain-specific `http` permissions (e.g., `http:tavily.com`).
- [ ] **Package Attestation**
    - [ ] Integrate **Sigstore/Cosign** for cryptographic signing of packages.
    - [ ] Implement signature verification during `neuron install`.

---

## 🚀 Phase 4: The AI OS (The Platform Phase)
*Goal: Pervasive environment for AI capabilities.*

- [ ] **`neuron deploy`**: Transform a local package into a serverless API endpoint.
- [ ] **Private Registries**: Enterprise-specific registry instances for proprietary agents.
- [ ] **Outcome-Based Ranking**: Use real-world success rates to rank tools in the registry.
- [ ] **Monetization Layer**: Implement a pay-per-execution billing system for package authors.

---

## 🎯 Immediate Priorities (The "Quick Wins")
1. [ ] **`neuron secrets set`** (Fix the biggest user friction point).
2. [ ] **Auto-Dependency Install** (Enable "One command to run").
3. [ ] **`workflow.json` Prototype** (Prove the composition vision).
