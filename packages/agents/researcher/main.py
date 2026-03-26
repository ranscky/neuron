import sys
import json
import subprocess
import urllib.request
import urllib.error
from pathlib import Path


def run(inputs: dict) -> dict:
    """Research assistant that searches the web and synthesizes findings"""
    try:
        # Extract inputs
        query = inputs.get('query')
        depth = inputs.get('depth', 3)
        
        if not query:
            return {'error': 'Query is required', 'error_type': 'invalid_query'}
        
        # Call tools/web-search using subprocess
        search_inputs = {
            'query': query,
            'max_results': depth
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
        
        # Synthesize results using Ollama
        synthesized_result = synthesize_research(query, search_results)
        
        return {
            'summary': synthesized_result,
            'sources': search_results,
            'query_used': query
        }
        
    except Exception as e:
        return {'error': f'Research failed: {str(e)}', 'error_type': 'synthesis_failed'}


def synthesize_research(query: str, search_results: list) -> dict:
    """Synthesize research results using Ollama"""
    try:
        # Prepare context from search results
        context_parts = []
        for i, result in enumerate(search_results[:5]):  # Limit to top 5 results
            context_parts.append(f"Source {i+1}:\nTitle: {result.get('title', '')}\nURL: {result.get('url', '')}\nContent: {result.get('content', '')}")
        
        context = "\n\n".join(context_parts)
        
        # Prepare the prompt for Ollama
        prompt = f"""Research Query: {query}

Web Search Results:
{context}

Please synthesize a structured research summary with these sections:
1. Overview - Brief summary of the topic
2. Key Findings - Main insights and discoveries
3. Sources - List of sources with titles and URLs

Format the response as JSON with keys: overview, key_findings, sources"""

        # Call Ollama API
        url = "http://localhost:11434/api/generate"
        data = {
            "model": "qwen3-coder:480b-cloud",
            "prompt": prompt,
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
                synthesized = json.loads(response_data['response'])
                return synthesized
            except json.JSONDecodeError:
                # If not valid JSON, return as plain text in a structured format
                return {
                    'overview': 'Research summary',
                    'key_findings': response_data['response'],
                    'sources': [f"Source {i+1}: {result.get('title', '')} - {result.get('url', '')}" 
                               for i, result in enumerate(search_results[:3])]
                }
        else:
            # Fallback response structure
            return {
                'overview': f'Research on "{query}"',
                'key_findings': 'Synthesized findings from web search results',
                'sources': [f"Source {i+1}: {result.get('title', '')} - {result.get('url', '')}" 
                           for i, result in enumerate(search_results[:3])]
            }
            
    except urllib.error.URLError as e:
        raise Exception(f'Ollama API error: {str(e)}')
    except Exception as e:
        raise Exception(f'Synthesis error: {str(e)}')


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
        error_result = {'error': f'Invalid JSON input: {str(e)}', 'error_type': 'invalid_query'}
        print(json.dumps(error_result))
        sys.exit(1)
    except Exception as e:
        error_result = {'error': f'Unexpected error: {str(e)}', 'error_type': 'processing_error'}
        print(json.dumps(error_result))
        sys.exit(1)