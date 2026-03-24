# Web Search Tool

This Neuron package provides real-time web search capabilities using the Tavily API.

## Capability

```json
{
  "name": "tools/web-search",
  "version": "1.0.0",
  "description": "Fetch real-time information from the web using Tavily",
  "capability": {
    "input": [
      { "name": "query", "type": "string", "required": true },
      { "name": "max_results", "type": "integer", "required": false, "default": 5 }
    ],
    "output": {
      "type": "array",
      "format": "json"
    },
    "errors": ["search_failed", "invalid_query", "api_key_missing"]
  },
  "performance": {
    "avg_latency_ms": 800,
    "p99_latency_ms": 2000,
    "success_rate": 0.96,
    "cost_per_call_usd": 0.001
  },
  "permissions": [
    "http",
    "env:TAVILY_API_KEY"
  ],
  "models": [],
  "runtime": "python",
  "entry": "main.py",
  "dependencies": {},
  "neuron": "1"
}
```

## Installation

```bash
neuron install tools/web-search
```

## Usage

```bash
neuron run tools/web-search
```

The tool reads JSON from stdin and outputs JSON to stdout.

### Example

Input (stdin):
```json
{
  "query": "latest developments in AI",
  "max_results": 3
}
```

Output (stdout):
```json
[
  {
    "title": "Breakthrough AI Research Published",
    "url": "https://example.com/ai-research",
    "content": "Scientists have made significant progress in neural network architectures...",
    "score": 0.95
  },
  {
    "title": "New AI Model Surpasses Previous Benchmarks",
    "url": "https://example.com/ai-model",
    "content": "The latest transformer model demonstrates unprecedented performance...",
    "score": 0.87
  }
]
```

### Environment Variables

Set your Tavily API key:
```bash
export TAVILY_API_KEY="your-api-key-here"