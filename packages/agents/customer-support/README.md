# Customer Support Agent

An AI-powered customer support agent that answers questions using Notion knowledge base or provided context.

## Usage

```bash
neuron run agents/customer-support "How do I reset my password?"
neuron run agents/customer-support "refund policy question"
```

## Advanced Usage

```bash
# With specific tone
neuron run agents/customer-support '{"question": "How do I cancel my subscription?", "tone": "professional"}'

# With custom context
neuron run agents/customer-support '{"question": "How does this work?", "context": "Product is a payment processing system", "tone": "formal"}'
```

## Capabilities

- Answers customer questions using Notion knowledge base
- Falls back to provided context when no cache available
- Supports different tones: friendly (default), professional, formal
- Provides confidence score and suggested follow-up questions

## Input Parameters

- `question` (string, required): The customer question to answer
- `context` (string, optional): Product or company context when no Notion cache available
- `tone` (string, optional, default: "friendly"): Response tone - "friendly", "professional", or "formal"

## Output Format

```json
{
  "answer": "string",
  "confidence": 0.85,
  "suggested_followups": ["array", "of", "strings"]
}
```

## Dependencies

- Requires `rag/notion-sync` package for knowledge base integration
- Uses Ollama with `qwen3-coder:480b-cloud` model locally

## Setup

1. Ensure Ollama is running locally
2. Sync Notion database using `neuron run rag/notion-sync` if using knowledge base
3. Run customer support queries as shown in usage examples