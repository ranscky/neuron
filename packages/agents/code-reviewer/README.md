# Code Reviewer Agent

An AI-powered code reviewer that analyzes code for bugs, improvements, and security issues using the Qwen3 Coder model.

## Features

- Reviews individual files or entire directories (up to 5 files)
- Multiple review focuses: security, performance, readability, or general
- Provides structured feedback with bugs, improvements, and security issues
- Quality scoring from 1-10
- Works with any programming language

## Usage

```bash
# Review a directory
neuron run agents/code-reviewer "./myproject"

# Review a single file with security focus
neuron run agents/code-reviewer "./app.py" --focus security

# Review with performance focus
neuron run agents/code-reviewer "./src" --focus performance
```

## Focus Options

- `security` - Security vulnerabilities and compliance issues
- `performance` - Performance bottlenecks and optimizations
- `readability` - Code structure and maintainability
- `general` - Comprehensive review (default)

## Output

The agent returns a JSON object with:

```json
{
  "bugs": ["array of bug descriptions"],
  "improvements": ["array of improvement suggestions"],
  "security_issues": ["array of security issues found"],
  "quality_score": 8,
  "files_reviewed": ["list of files that were reviewed"]
}
```

## Requirements

- Ollama with qwen3-coder:480b-cloud model running locally
- Python 3.7+
- Standard library only (no external dependencies)