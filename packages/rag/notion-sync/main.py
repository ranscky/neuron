import sys
import json
import os
import urllib.request
import urllib.error
from pathlib import Path
import hashlib


def run(inputs: dict) -> dict:
    """Sync Notion database or query cached content using Ollama"""
    try:
        # Extract inputs
        action = inputs.get('action')
        database_id = inputs.get('database_id')
        question = inputs.get('question')
        
        # Validate action
        if action not in ['sync', 'query']:
            return {'error': 'Invalid action. Must be "sync" or "query"', 'error_type': 'invalid_action'}
        
        # Handle sync action
        if action == 'sync':
            if not database_id:
                return {'error': 'database_id is required for sync action', 'error_type': 'sync_failed'}
            return sync_notion_database(database_id)
        
        # Handle query action
        elif action == 'query':
            if not question:
                return {'error': 'question is required for query action', 'error_type': 'query_failed'}
            return query_notion_content(question)
        else:
            return {'error': 'Invalid action. Must be "sync" or "query"', 'error_type': 'invalid_action'}
            
    except Exception as e:
        return {'error': f'Processing error: {str(e)}', 'error_type': 'processing_error'}


def sync_notion_database(database_id: str) -> dict:
    """Fetch all pages from Notion database and save to cache"""
    try:
        # Get API key from environment
        api_key = os.environ.get('NOTION_API_KEY')
        if not api_key:
            return {'error': 'NOTION_API_KEY environment variable not set', 'error_type': 'api_key_missing'}
        
        # Get cache directory and file path
        cache_dir = Path.home() / '.neuron'
        cache_dir.mkdir(exist_ok=True)
        cache_file = cache_dir / 'notion-cache.json'
        
        # Fetch database pages using Notion API v1
        url = f"https://api.notion.com/v1/databases/{database_id}/query"
        
        # Prepare request data to fetch all pages
        data = {
            "page_size": 100  # Fetch up to 100 pages per request
        }
        
        # Convert to JSON and encode
        json_data = json.dumps(data).encode('utf-8')
        
        # Create request
        req = urllib.request.Request(
            url,
            data=json_data,
            headers={
                'Content-Type': 'application/json',
                'Authorization': f'Bearer {api_key}',
                'Notion-Version': '2022-06-28'
            }
        )
        
        # Make the request
        try:
            with urllib.request.urlopen(req) as response:
                response_data = json.loads(response.read().decode('utf-8'))
        except urllib.error.HTTPError as e:
            error_response = e.read().decode('utf-8')
            return {'error': f'Notion API error: {e.code} - {error_response}', 'error_type': 'sync_failed'}
        except Exception as e:
            return {'error': f'Network error: {str(e)}', 'error_type': 'sync_failed'}
        
        # Process pages and extract content
        pages = []
        if 'results' in response_data:
            for page in response_data['results']:
                if page.get('object') == 'page':
                    # Extract page properties
                    properties = page.get('properties', {})
                    title = ""
                    
                    # Find title property (usually called "Name" or "Title")
                    for prop_name, prop_value in properties.items():
                        if prop_value.get('type') == 'title':
                            title_parts = prop_value.get('title', [])
                            if title_parts:
                                title = "".join([part.get('plain_text', '') for part in title_parts])
                            break
                    
                    # For now, we'll store basic page info
                    # In a real implementation, you might fetch page content too
                    pages.append({
                        'id': page.get('id'),
                        'title': title,
                        'created_time': page.get('created_time'),
                        'last_edited_time': page.get('last_edited_time'),
                        'url': page.get('url')
                    })
        
        # Save to cache file
        cache_data = {
            'database_id': database_id,
            'last_sync': response_data.get('next_cursor', None),
            'pages': pages
        }
        
        with open(cache_file, 'w') as f:
            json.dump(cache_data, f, indent=2)
        
        return {
            'result': f'Successfully synced {len(pages)} pages from database {database_id}',
            'pages_found': len(pages)
        }
        
    except Exception as e:
        return {'error': f'Sync failed: {str(e)}', 'error_type': 'sync_failed'}


def query_notion_content(question: str) -> dict:
    """Load cached content and find relevant pages using keyword matching"""
    try:
        # Get cache file path
        cache_file = Path.home() / '.neuron' / 'notion-cache.json'
        
        # Check if cache exists
        if not cache_file.exists():
            return {'error': 'No cached data found. Run sync action first.', 'error_type': 'query_failed'}
        
        # Load cached data
        with open(cache_file, 'r') as f:
            cache_data = json.load(f)
        
        # Simple keyword matching to find relevant pages
        question_lower = question.lower()
        relevant_pages = []
        
        for page in cache_data.get('pages', []):
            # Create searchable text from page properties
            searchable_text = f"{page.get('title', '')} {page.get('url', '')}".lower()
            
            # Count keyword matches
            score = sum(1 for word in question_lower.split() if word in searchable_text)
            
            if score > 0:
                relevant_pages.append((score, page))
        
        # Sort by relevance
        relevant_pages.sort(reverse=True)
        top_pages = [page for score, page in relevant_pages[:5]]  # Take top 5 pages
        
        if not top_pages:
            # If no relevant pages found, use first few pages
            top_pages = cache_data.get('pages', [])[:3]
        
        # Prepare context for Ollama
        context_parts = []
        for page in top_pages:
            context_parts.append(f"Title: {page.get('title')}\nURL: {page.get('url')}")
        
        context = "\n\n".join(context_parts)
        
        # Prepare the prompt for Ollama
        prompt = f"Context from Notion pages:\n{context}\n\nQuestion: {question}"
        
        # Call Ollama API
        try:
            url = "http://localhost:11434/api/generate"
            data = {
                "model": "qwen3-coder:480b-cloud",
                "prompt": prompt,
                "stream": False
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
                
            # Extract answer from response
            answer = response_data.get('response', 'No response generated')
            
            return {
                'result': answer,
                'pages_found': len(top_pages)
            }
        except urllib.error.URLError as e:
            return {'error': f'Ollama API error: {str(e)}', 'error_type': 'query_failed'}
        except Exception as e:
            return {'error': f'Ollama API error: {str(e)}', 'error_type': 'query_failed'}
            
    except Exception as e:
        return {'error': f'Query failed: {str(e)}', 'error_type': 'query_failed'}


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
        error_result = {'error': f'Invalid JSON input: {str(e)}', 'error_type': 'invalid_action'}
        print(json.dumps(error_result))
        sys.exit(1)
    except Exception as e:
        error_result = {'error': f'Unexpected error: {str(e)}', 'error_type': 'processing_error'}
        print(json.dumps(error_result))
        sys.exit(1)