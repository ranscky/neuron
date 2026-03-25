# PDF Reader - RAG Tool

Extract information from PDF files and answer questions using Ollama.

## Description

This tool reads PDF documents, extracts relevant text chunks based on your question using simple keyword matching, and uses Ollama to generate accurate answers from the extracted content.

## Inputs

- `file_path` (string, required): Path to the PDF file
- `question` (string, required): Question to answer based on the PDF content
- `max_pages` (integer, optional, default 50): Maximum number of pages to read from the PDF

## Output

Returns a JSON object with:
- `answer`: The answer to your question based on the PDF content
- `pages_read`: Number of pages processed from the PDF
- `chunks_used`: Number of text chunks used for context

## Usage Examples

### Basic Usage

```bash
echo '{"file_path": "/path/to/document.pdf", "question": "What is the main topic of this document?"}' | neuron run rag/pdf-reader
```

### With Custom Page Limit

```bash
echo '{"file_path": "/path/to/document.pdf", "question": "Summarize the key points", "max_pages": 10}' | neuron run rag/pdf-reader
```

### In a Script

```bash
#!/bin/bash
neuron run rag/pdf-reader << EOF
{
  "file_path": "./research-paper.pdf",
  "question": "What are the conclusions of this study?",
  "max_pages": 25
}
EOF
```

## Requirements

- Python 3.8+
- Ollama service running locally on port 11434
- PDF file accessible from the file system

## Dependencies

- pypdf - For PDF text extraction

## Error Handling

The tool returns structured error messages:
- `file_not_found`: PDF file doesn't exist
- `invalid_pdf`: File is not a valid PDF
- `ollama_error`: Issues with Ollama API call
- `processing_error`: General processing issues

All errors are returned as JSON with exit code 1.