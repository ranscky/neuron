import sys
import json
import subprocess
import urllib.request
import urllib.error
import os
from pathlib import Path


def call_model(prompt: str, system: str = "You are a helpful assistant") -> str:
    """
    Call the configured model with prompt and system message.
    Supports Ollama, OpenAI, Anthropic, and Groq providers.
    """
    # Determine which provider to use based on environment or configuration
    provider = os.getenv('NEURON_MODEL_PROVIDER', 'ollama').lower()
    
    if provider == 'ollama':
        return _call_ollama(prompt, system)
    elif provider == 'openai':
        return _call_openai(prompt, system)
    elif provider == 'anthropic':
        return _call_anthropic(prompt, system)
    elif provider == 'groq':
        return _call_groq(prompt, system)
    else:
        # Default to Ollama if unknown provider
        return _call_ollama(prompt, system)


def _call_ollama(prompt: str, system: str) -> str:
    """Call Ollama model"""
    try:
        full_prompt = f"{system}\n\n{prompt}" if system else prompt
        url = "http://localhost:11434/api/generate"
        data = {
            "model": "qwen3-coder:480b-cloud",
            "prompt": full_prompt,
            "stream": False
        }
        
        json_data = json.dumps(data).encode('utf-8')
        req = urllib.request.Request(
            url,
            data=json_data,
            headers={'Content-Type': 'application/json'}
        )
        
        with urllib.request.urlopen(req) as response:
            response_data = json.loads(response.read().decode('utf-8'))
            return response_data.get('response', '')
            
    except Exception as e:
        raise Exception(f'Ollama API error: {str(e)}')


def _call_openai(prompt: str, system: str) -> str:
    """Call OpenAI model"""
    try:
        import openai
        client = openai.OpenAI(api_key=os.getenv('OPENAI_API_KEY'))
        
        messages = []
        if system:
            messages.append({"role": "system", "content": system})
        messages.append({"role": "user", "content": prompt})
        
        response = client.chat.completions.create(
            model="gpt-4-turbo",
            messages=messages,
            temperature=0.7,
            max_tokens=2048
        )
        
        return response.choices[0].message.content
        
    except ImportError:
        raise Exception("OpenAI library not installed. Please install with: pip install openai")
    except Exception as e:
        raise Exception(f'OpenAI API error: {str(e)}')


def _call_anthropic(prompt: str, system: str) -> str:
    """Call Anthropic model"""
    try:
        import anthropic
        client = anthropic.Anthropic(api_key=os.getenv('ANTHROPIC_API_KEY'))
        
        full_prompt = f"{system}\n\n{prompt}" if system else prompt
        response = client.completions.create(
            model="claude-3-opus-20240229",
            prompt=full_prompt,
            max_tokens_to_sample=2048,
            temperature=0.7
        )
        
        return response.completion
        
    except ImportError:
        raise Exception("Anthropic library not installed. Please install with: pip install anthropic")
    except Exception as e:
        raise Exception(f'Anthropic API error: {str(e)}')


def _call_groq(prompt: str, system: str) -> str:
    """Call Groq model"""
    try:
        import groq
        client = groq.Groq(api_key=os.getenv('GROQ_API_KEY'))
        
        messages = []
        if system:
            messages.append({"role": "system", "content": system})
        messages.append({"role": "user", "content": prompt})
        
        response = client.chat.completions.create(
            model="mixtral-8x7b-32768",
            messages=messages,
            temperature=0.7,
            max_tokens=2048
        )
        
        return response.choices[0].message.content
        
    except ImportError:
        raise Exception("Groq library not installed. Please install with: pip install groq")
    except Exception as e:
        raise Exception(f'Groq API error: {str(e)}')


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
        
        # Get the path to tools/web-search main.py with dynamic version detection
        web_search_base_path = Path.home() / '.neuron' / 'packages' / 'tools' / 'web-search'
        
        # Dynamically detect the installed version
        web_search_version = None
        if web_search_base_path.exists():
            try:
                versions = [d.name for d in web_search_base_path.iterdir() if d.is_dir()]
                if versions:
                    web_search_version = sorted(versions)[-1]  # Get the latest version
            except Exception:
                pass
        
        if web_search_version:
            web_search_path = web_search_base_path / web_search_version / 'main.py'
        else:
            # Fallback to hardcoded path if dynamic detection fails
            web_search_path = web_search_base_path / '1.0.0' / 'main.py'
        
        # If the packaged version doesn't exist, try the local development version
        if not web_search_path.exists():
            web_search_path = Path(__file__).parent.parent.parent / 'tools' / 'web-search' / 'main.py'
        
        if not web_search_path.exists():
            return {'error': 'tools/web-search package not found', 'error_type': 'search_failed'}
        
        # Construct venv Python path
        venv_python_path = None
        venv_path = Path.home() / '.neuron' / 'venv' / 'tools' / 'web-search' / 'bin' / 'python3'
        if venv_path.exists():
            venv_python_path = venv_path
        
        # Use venv Python if it exists, otherwise fall back to sys.executable
        python_executable = str(venv_python_path) if venv_python_path else sys.executable
        
        # Run the web search tool as subprocess
        search_process = subprocess.run([
            python_executable, str(web_search_path)
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
    """Synthesize research results using the shared model utility"""
    try:
        # Prepare context from search results
        context_parts = []
        for i, result in enumerate(search_results[:5]):  # Limit to top 5 results
            context_parts.append(f"Source {i+1}:\nTitle: {result.get('title', '')}\nURL: {result.get('url', '')}\nContent: {result.get('content', '')}")
        
        context = "\n\n".join(context_parts)
        
        # Prepare the prompt
        prompt = f"""Research Query: {query}

Web Search Results:
{context}

Please synthesize a structured research summary with these sections:
1. Overview - Brief summary of the topic
2. Key Findings - Main insights and discoveries
3. Sources - List of sources with titles and URLs

Format the response as JSON with keys: overview, key_findings, sources"""

        # Use the shared model utility with the specified system prompt
        system_prompt = "You are a research analyst. Synthesize findings into a structured summary with overview, key findings and sources."
        response_text = call_model(prompt, system_prompt)
        
        # Try to parse the response as JSON
        try:
            synthesized = json.loads(response_text)
            return synthesized
        except json.JSONDecodeError:
            # If not valid JSON, return as plain text in a structured format
            return {
                'overview': 'Research summary',
                'key_findings': response_text,
                'sources': [f"Source {i+1}: {result.get('title', '')} - {result.get('url', '')}" 
                           for i, result in enumerate(search_results[:3])]
            }
            
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