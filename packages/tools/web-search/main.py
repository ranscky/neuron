import sys
import json
from tavily import TavilyClient
import os

def run(inputs: dict) -> dict:
    """
    Perform web search using Tavily API
    
    Args:
        inputs: dict with 'query' (str, required) and 'max_results' (int, optional)
    
    Returns:
        dict: Array of search results with title, url, content, score
    """
    try:
        query = inputs.get('query')
        if not query:
            return {'error': 'Query is required'}
        
        max_results = inputs.get('max_results', 5)
        
        # Get API key from environment
        api_key = os.environ.get('TAVILY_API_KEY')
        if not api_key:
            return {'error': 'TAVILY_API_KEY environment variable not set'}
        
        # Initialize Tavily client
        client = TavilyClient(api_key=api_key)
        
        # Perform search
        response = client.search(
            query=query,
            max_results=max_results,
            include_answer=False,
            include_raw_content=True
        )
        
        # Format results
        results = []
        if 'results' in response:
            for item in response['results']:
                results.append({
                    'title': item.get('title', ''),
                    'url': item.get('url', ''),
                    'content': item.get('content', ''),
                    'score': item.get('score', 0)
                })
        
        return results
        
    except Exception as e:
        return {'error': f'Search failed: {str(e)}'}

if __name__ == '__main__':
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
        print(json.dumps({'error': f'Invalid JSON input: {str(e)}'}))
        sys.exit(1)
    except Exception as e:
        print(json.dumps({'error': f'Unexpected error: {str(e)}'}))
        sys.exit(1)