# Researcher Agent

A research assistant that searches the web and synthesizes findings using Ollama.

## Usage

```bash
neuron run agents/researcher "latest AI trends"
```

## Capabilities

- Performs web searches using Tavily API
- Synthesizes research findings using Ollama (qwen3-coder:480b-cloud)
- Returns structured summaries with overview, key findings, and sources

## Input Parameters

- `query` (string, required): Research topic or question
- `depth` (integer, optional, default: 3): Number of sources to search

## Output Format

```json
{
  "summary": {
    "overview": "...",
    "key_findings": "...",
    "sources": [...]
  },
  "sources": [...],
  "query_used": "..."
}