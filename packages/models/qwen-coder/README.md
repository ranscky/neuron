# Qwen3 Coder Model Abstraction

This package provides a standardized interface to the Qwen3 Coder model running locally via Ollama. It's designed as a reusable model abstraction that other Neuron agents can depend on for code generation capabilities.

## Capabilities

- **Model**: `qwen3-coder:480b-cloud`
- **Purpose**: Code generation and programming assistance
- **Input Format**: JSON with configurable parameters
- **Output Format**: Structured JSON response

## Usage

Other agents can use this model abstraction by declaring it as a dependency in their `neuron.json`:

```json
{
  "dependencies": {
    "models/qwen-coder": "^1.0.0"
  }
}
```

### Input Parameters

- `prompt` (string, required): The coding prompt or question
- `system` (string, optional): System prompt (default: "You are an expert programmer")
- `temperature` (float, optional): Sampling temperature (default: 0.7)
- `max_tokens` (integer, optional): Maximum tokens to generate (default: 2048)

### Example Usage

```bash
# Direct usage
neuron run models/qwen-coder '{"prompt": "Write a Python function to reverse a string"}'

# Usage from another agent
neuron run agents/my-agent '{"task": "generate auth module"}'
```

### Programmatic Usage

Agents can invoke this model programmatically:

```python
import subprocess
import json

# Prepare inputs
inputs = {
    "prompt": "Create a REST API endpoint for user authentication",
    "system": "You are a senior backend engineer specializing in Python Flask",
    "temperature": 0.5,
    "max_tokens": 1024
}

# Call the model
process = subprocess.run([
    'python', 'models/qwen-coder/main.py'
], input=json.dumps(inputs), text=True, capture_output=True, encoding='utf-8')

# Parse response
response = json.loads(process.stdout)
print(response['response'])
```

## Response Format

The model returns a structured JSON response:

```json
{
  "response": "Generated code or explanation...",
  "model": "qwen3-coder:480b-cloud",
  "tokens_used": 1250
}
```

## Error Handling

Errors are returned as JSON with error details:

```json
{
  "error": "Ollama API error: Connection refused",
  "error_type": "api_error"
}
```

## Requirements

- Ollama running locally at `http://localhost:11434`
- Qwen3 Coder model installed (`ollama pull qwen3-coder:480b-cloud`)
- No external Python dependencies (uses standard library only)

## Permissions

This package requires the `local:ollama` permission to communicate with the local Ollama service.