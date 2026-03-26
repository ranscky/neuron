# Notion Sync - RAG Tool

Sync Notion databases and query content using Ollama.

## Description

This tool provides two main functionalities:
1. **Sync**: Fetch all pages from a Notion database and cache them locally
2. **Query**: Search cached Notion content and get answers using Ollama

The sync action stores data in `~/.neuron/notion-cache.json` for fast querying without hitting the Notion API.

## Inputs

### For Sync Action
- `action` (string, required): Must be "sync"
- `database_id` (string, required): The Notion database ID to sync

### For Query Action
- `action` (string, required): Must be "query"
- `question` (string, required): The question to answer based on Notion content

## Output

Returns a JSON object with:
- `result`: The result message (sync confirmation or answer to question)
- `pages_found`: Number of pages processed or found

## Usage Examples

### Sync Database

```bash
export NOTION_API_KEY="your_notion_api_key"
echo '{"action": "sync", "database_id": "abc123..."}' | neuron run rag/notion-sync
```

### Query Content

```bash
echo '{"action": "query", "question": "What are our product requirements?"}' | neuron run rag/notion-sync
```

### In a Script

```bash
#!/bin/bash
# First sync the database
neuron run rag/notion-sync << EOF
{
  "action": "sync",
  "database_id": "abc123..."
}
EOF

# Then query the content
neuron run rag/notion-sync << EOF
{
  "action": "query",
  "question": "What are the key features of our product?"
}
EOF
```

## Requirements

- Python 3.8+
- Ollama service running locally on port 11434
- Notion API key with database access permissions
- Notion database ID

## Dependencies

None - Uses only Python standard library

## Error Handling

The tool returns structured error messages:
- `sync_failed`: Issues with Notion API sync
- `query_failed`: Issues with query processing
- `api_key_missing`: NOTION_API_KEY environment variable not set
- `invalid_action`: Invalid action parameter
- `processing_error`: General processing issues

All errors are returned as JSON with exit code 1.