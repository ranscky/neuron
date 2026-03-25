# GitHub Tool

This Neuron package provides GitHub repository interaction capabilities using the GitHub API.

## Capability

```json
{
  "name": "tools/github",
  "version": "1.0.0",
  "description": "Interact with GitHub repositories using the GitHub API",
  "capability": {
    "input": [
      { "name": "action", "type": "string", "required": true, "description": "Action to perform: get_repo, list_issues, get_file, open_issue" },
      { "name": "repo", "type": "string", "required": true, "description": "Repository in format 'owner/repo'" },
      { "name": "path", "type": "string", "required": false, "description": "File path (required for get_file)" },
      { "name": "title", "type": "string", "required": false, "description": "Issue title (required for open_issue)" },
      { "name": "body", "type": "string", "required": false, "description": "Issue body (required for open_issue)" }
    ],
    "output": {
      "type": "object",
      "format": "json"
    },
    "errors": ["invalid_action", "repo_not_found", "file_not_found", "issue_creation_failed", "authentication_error"]
  },
  "performance": {
    "avg_latency_ms": 1000,
    "p99_latency_ms": 3000,
    "success_rate": 0.95,
    "cost_per_call_usd": 0.000
  },
  "permissions": [
    "http",
    "env:GITHUB_TOKEN"
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
neuron install tools/github
```

## Usage

```bash
neuron run tools/github
```

The tool reads JSON from stdin and outputs JSON to stdout.

### Actions

#### get_repo - Get repository information

Input (stdin):
```json
{
  "action": "get_repo",
  "repo": "octocat/Hello-World"
}
```

Output (stdout):
```json
{
  "name": "Hello-World",
  "full_name": "octocat/Hello-World",
  "description": "A repository for testing purposes",
  "stars": 1234,
  "language": "JavaScript",
  "url": "https://github.com/octocat/Hello-World",
  "forks": 567,
  "open_issues": 8
}
```

#### list_issues - List open issues

Input (stdin):
```json
{
  "action": "list_issues",
  "repo": "octocat/Hello-World"
}
```

Output (stdout):
```json
[
  {
    "number": 42,
    "title": "Bug in authentication flow",
    "url": "https://github.com/octocat/Hello-World/issues/42",
    "state": "open",
    "created_at": "2023-01-15T10:30:00Z"
  },
  {
    "number": 43,
    "title": "Feature request: Add dark mode",
    "url": "https://github.com/octocat/Hello-World/issues/43",
    "state": "open",
    "created_at": "2023-01-16T14:22:00Z"
  }
]
```

#### get_file - Get file content

Input (stdin):
```json
{
  "action": "get_file",
  "repo": "octocat/Hello-World",
  "path": "README.md"
}
```

Output (stdout):
```json
{
  "path": "README.md",
  "content": "# Hello World\n\nThis is a sample repository...",
  "sha": "a1b2c3d4e5f6g7h8i9j0k1l2m3n4o5p6q7r8s9t0",
  "size": 1024
}
```

#### open_issue - Create a new issue

Input (stdin):
```json
{
  "action": "open_issue",
  "repo": "octocat/Hello-World",
  "title": "Found a bug in the login page",
  "body": "When clicking the login button, the page redirects to an incorrect URL."
}
```

Output (stdout):
```json
{
  "number": 123,
  "title": "Found a bug in the login page",
  "url": "https://github.com/octocat/Hello-World/issues/123",
  "state": "open"
}
```

### Environment Variables

Set your GitHub personal access token:
```bash
export GITHUB_TOKEN="your-github-token-here"
```

The token should have appropriate permissions for the repositories you want to access.