# Content Writer Agent

A content writer agent that creates engaging content using web research and Ollama.

## Usage

```bash
neuron run agents/content-writer "AI in healthcare"
neuron run agents/content-writer "blockchain" --type summary
```

## Capabilities

- Performs web searches using Tavily API to gather context
- Generates various types of content using Ollama (qwen3-coder:480b-cloud)
- Supports different content types, tones, and lengths

## Input Parameters

- `topic` (string, required): The main subject for content creation
- `type` (string, optional, default: "blog"): Content type - one of: blog, summary, rewrite
- `tone` (string, optional, default: "professional"): Writing tone - one of: professional, casual, technical
- `length` (string, optional, default: "medium"): Content length - one of: short, medium, long

## Output Format

```json
{
  "title": "Compelling Title",
  "content": "Generated content text...",
  "word_count": 1200,
  "sources_used": ["https://example.com/source1", "https://example.com/source2"]
}