import sys
import json
import subprocess
import urllib.request
import urllib.error
from pathlib import Path


def run(inputs: dict) -> dict:
    """Content writer that creates engaging content using web research and Ollama"""
    try:
        # Extract inputs
        topic = inputs.get('topic')
        content_type = inputs.get('type', 'blog')
        tone = inputs.get('tone', 'professional')
        length = inputs.get('length', 'medium')
        
        if not topic:
            return {'error': 'Topic is required', 'error_type': 'invalid_input'}
        
        # Validate enum values
        valid_types = ['blog', 'summary', 'rewrite']
        valid_tones = ['professional', 'casual', 'technical']
        valid_lengths = ['short', 'medium', 'long']
        
        if content_type not in valid_types:
            return {'error': f'Invalid type. Must be one of: {valid_types}', 'error_type': 'invalid_input'}
        
        if tone not in valid_tones:
            return {'error': f'Invalid tone. Must be one of: {valid_tones}', 'error_type': 'invalid_input'}
        
        if length not in valid_lengths:
            return {'error': f'Invalid length. Must be one of: {valid_lengths}', 'error_type': 'invalid_input'}
        
        # Call tools/web-search using subprocess
        search_inputs = {
            'query': topic,
            'max_results': 3
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
        
        # Generate content using Ollama
        generated_content = generate_content(topic, content_type, tone, length, search_results)
        
        return generated_content
        
    except Exception as e:
        return {'error': f'Content generation failed: {str(e)}', 'error_type': 'generation_failed'}


def generate_content(topic: str, content_type: str, tone: str, length: str, search_results: list) -> dict:
    """Generate content using Ollama"""
    try:
        # Prepare context from search results
        context_parts = []
        for i, result in enumerate(search_results[:3]):  # Limit to top 3 results
            context_parts.append(f"Source {i+1}:\nTitle: {result.get('title', '')}\nURL: {result.get('url', '')}\nContent: {result.get('content', '')}")
        
        context = "\n\n".join(context_parts)
        
        # Map length to word count guidance
        length_guidance = {
            'short': 'around 300-500 words',
            'medium': 'around 800-1200 words', 
            'long': 'around 1500-2000 words'
        }
        
        # Prepare the prompt for Ollama
        prompt = f"""You are an expert content writer. Write engaging, well-structured content.

Content Requirements:
- Topic: {topic}
- Type: {content_type}
- Tone: {tone}
- Length: {length_guidance.get(length, 'medium')} ({length})

Web Research Context:
{context}

Instructions:
Create well-structured content based on the research context provided. 
For blog posts: Include an engaging introduction, well-organized body sections, and a conclusion.
For summaries: Provide a concise overview with key points highlighted.
For rewrites: Rephrase and improve the content while maintaining the core message.

Return ONLY valid JSON with these fields:
- title: A compelling title for the content
- content: The main content text
- word_count: Integer count of words in the content
- sources_used: Array of source URLs used

Format the response as strict JSON without any markdown formatting or additional text."""

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
                generated = json.loads(response_data['response'])
                return generated
            except json.JSONDecodeError:
                # If not valid JSON, return as plain text in a structured format
                word_count = len(response_data['response'].split())
                return {
                    'title': f'Content on {topic}',
                    'content': response_data['response'],
                    'word_count': word_count,
                    'sources_used': [result.get('url', '') for result in search_results[:3] if result.get('url')]
                }
        else:
            # Fallback response structure
            return {
                'title': f'{content_type.title()} on {topic}',
                'content': f'Generated {content_type} content about {topic} with {tone} tone.',
                'word_count': 50,
                'sources_used': [result.get('url', '') for result in search_results[:3] if result.get('url')]
            }
            
    except urllib.error.URLError as e:
        raise Exception(f'Ollama API error: {str(e)}')
    except Exception as e:
        raise Exception(f'Generation error: {str(e)}')


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