import sys
import json
from tavily import TavilyClient
import os

def run(inputs: dict) -> dict | list:
    """Perform web search using Tavily API"""
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
        query=str(query),
        max_results=int(max_results),
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

if __name__ == "__main__":
    # Neuron passes inputs as a JSON string via stdin
    raw = sys.stdin.read()
    inputs = json.loads(raw)
    result = run(inputs)
    print(json.dumps(result))