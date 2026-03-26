import sys
import json
import subprocess
import urllib.request
import urllib.error
from pathlib import Path


def run(inputs: dict) -> dict:
    """Analyze startup ideas by researching market and competitors"""
    try:
        # Extract inputs
        idea = inputs.get('idea')
        market = inputs.get('market', 'global')
        
        if not idea:
            return {'error': 'Idea is required', 'error_type': 'invalid_input'}
        
        # Call tools/web-search using subprocess to research market and competitors
        search_query = f"startup market analysis {idea} {market}"
        search_inputs = {
            'query': search_query,
            'max_results': 5
        }
        
        # Get the path to tools/web-search main.py
        web_search_path = Path.home() / '.neuron' / 'packages' / 'tools' / 'web-search' / '1.0.0' / 'main.py'
        
        # If the packaged version doesn't exist, try the local development version
        if not web_search_path.exists():
            web_search_path = Path(__file__).parent.parent.parent / 'tools' / 'web-search' / 'main.py'
        
        if not web_search_path.exists():
            return {'error': 'tools/web-search package not found', 'error_type': 'search_failed'}
        
        # Run the web search tool as subprocess
        search_process = subprocess.run([
            sys.executable, str(web_search_path)
        ], input=json.dumps(search_inputs), text=True, capture_output=True, encoding='utf-8')
        
        if search_process.returncode != 0:
            return {'error': f'Web search failed: {search_process.stderr}', 'error_type': 'search_failed'}
        
        # Parse search results
        search_results = json.loads(search_process.stdout)
        
        if 'error' in search_results:
            return {'error': f'Web search error: {search_results["error"]}', 'error_type': 'search_failed'}
        
        # Analyze results using Ollama
        analysis_result = analyze_startup(idea, market, search_results)
        
        return analysis_result
        
    except Exception as e:
        return {'error': f'Analysis failed: {str(e)}', 'error_type': 'analysis_failed'}


def analyze_startup(idea: str, market: str, search_results: list) -> dict:
    """Analyze startup idea using Ollama"""
    try:
        # Prepare context from search results
        context_parts = []
        for i, result in enumerate(search_results[:5]):  # Limit to top 5 results
            context_parts.append(f"Source {i+1}:\nTitle: {result.get('title', '')}\nURL: {result.get('url', '')}\nContent: {result.get('content', '')}")
        
        context = "\n\n".join(context_parts)
        
        # Prepare the prompt for Ollama
        prompt = f"""Startup Idea: {idea}
Target Market: {market}

Market Research Results:
{context}

You are an expert startup analyst. Analyze this idea and provide a structured breakdown.

Return a JSON object with exactly these fields:
- verdict: "Promising" or "Risky" or "Avoid" - Your overall assessment
- market_size: A string describing the market size (e.g., "$10B annually", "Emerging market")
- competitors: Array of strings - Key competitors in this space
- risks: Array of strings - Main risks and challenges
- monetization: Array of strings - Potential monetization strategies
- next_steps: Array of strings - Concrete next steps for validation
- score: Integer from 1-10 - Overall viability score

Provide concise, actionable insights."""

        # Call Ollama API
        url = "http://localhost:11434/api/generate"
        data = {
            "model": "qwen3-coder:480b-cloud",
            "prompt": prompt,
            "system": "You are an expert startup analyst. Analyze this idea and provide a structured breakdown.",
            "stream": False,
            "format": "json"
        }
        
        # Convert to JSON and encode
        json_data = json.dumps(data).encode('utf-8')
        
        # Create request
        req = urllib.request.Request(
            url,
            data=json_data,
            headers={'Content-Type': 'application/json'}
        )
        
        # Make the request
        with urllib.request.urlopen(req) as response:
            response_data = json.loads(response.read().decode('utf-8'))
            
        # Extract and parse the response
        if 'response' in response_data:
            try:
                # Try to parse the response as JSON
                analysis = json.loads(response_data['response'])
                return analysis
            except json.JSONDecodeError:
                # If not valid JSON, return a fallback structured response
                return {
                    'verdict': 'Risky',
                    'market_size': 'Unknown',
                    'competitors': ['Research needed'],
                    'risks': ['Analysis parsing failed'],
                    'monetization': ['Further research needed'],
                    'next_steps': ['Validate idea with customers'],
                    'score': 5
                }
        else:
            # Fallback response structure
            return {
                'verdict': 'Risky',
                'market_size': 'Unknown',
                'competitors': ['Research needed'],
                'risks': ['Limited data available'],
                'monetization': ['Further research needed'],
                'next_steps': ['Validate idea with customers'],
                'score': 5
            }
            
    except urllib.error.URLError as e:
        raise Exception(f'Ollama API error: {str(e)}')
    except Exception as e:
        raise Exception(f'Analysis error: {str(e)}')


if __name__ == "__main__":
    try:
        # Read JSON from stdin
        raw = sys.stdin.read()
        inputs = json.loads(raw)
        result = run(inputs)
        
        # Check if result contains an error
        if isinstance(result, dict) and 'error' in result:
            print(json.dumps(result))
            sys.exit(1)
        
        # Output results as JSON
        print(json.dumps(result))
        
    except json.JSONDecodeError as e:
        error_result = {'error': f'Invalid JSON input: {str(e)}', 'error_type': 'invalid_input'}
        print(json.dumps(error_result))
        sys.exit(1)
    except Exception as e:
        error_result = {'error': f'Unexpected error: {str(e)}', 'error_type': 'processing_error'}
        print(json.dumps(error_result))
        sys.exit(1)