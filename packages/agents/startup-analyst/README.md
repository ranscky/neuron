# Startup Analyst Agent

An AI-powered startup idea analyzer that researches market viability and provides strategic recommendations.

## Usage

```bash
neuron run agents/startup-analyst "AI for farmers in Africa"
neuron run agents/startup-analyst "npm for AI tools"
```

## Capabilities

- Performs market research using Tavily API
- Analyzes startup ideas using Ollama (qwen3-coder:480b-cloud)
- Provides structured analysis with viability score and recommendations

## Input Parameters

- `idea` (string, required): The startup idea to analyze
- `market` (string, optional, default: "global"): Target market for the idea

## Output Format

```json
{
  "verdict": "Promising|Risky|Avoid",
  "market_size": "string",
  "competitors": ["array", "of", "strings"],
  "risks": ["array", "of", "strings"],
  "monetization": ["array", "of", "strings"],
  "next_steps": ["array", "of", "strings"],
  "score": 1-10
}
```

## Example Output

```json
{
  "verdict": "Promising",
  "market_size": "$5B annually",
  "competitors": ["John Deere", "FarmWise", "Blue River Technology"],
  "risks": ["High competition", "Regulatory compliance", "Weather dependency"],
  "monetization": ["SaaS subscriptions", "Hardware sales", "Consulting services"],
  "next_steps": ["Conduct customer interviews", "Build MVP prototype", "Identify early adopters"],
  "score": 8
}