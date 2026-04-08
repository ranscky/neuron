import os
import json
import urllib.request
import urllib.error


def call_model(prompt: str, system: str = "") -> str:
    """
    Call the configured AI model with the given prompt and optional system message.
    
    Args:
        prompt (str): The user prompt to send to the model
        system (str, optional): System message to guide the model behavior
        
    Returns:
        str: The model's response text
        
    Raises:
        Exception: If there's an error calling the model or processing the response
    """
    provider = os.environ.get("NEURON_PROVIDER", "ollama")
    
    if provider == "ollama":
        return _call_ollama(prompt, system)
    elif provider == "openai":
        return _call_openai(prompt, system)
    elif provider == "anthropic":
        return _call_anthropic(prompt, system)
    elif provider == "groq":
        return _call_groq(prompt, system)
    else:
        raise Exception(f"Unsupported provider: {provider}")


def _call_ollama(prompt: str, system: str = "") -> str:
    """Call Ollama model via HTTP API"""
    base_url = os.environ.get("NEURON_OLLAMA_BASE_URL", "http://localhost:11434")
    model = os.environ.get("NEURON_MODEL", "llama3")
    
    url = f"{base_url}/api/generate"
    
    # Prepare request data
    data = {
        "model": model,
        "prompt": prompt,
        "stream": False
    }
    
    if system:
        data["system"] = system
    
    # Convert to JSON
    json_data = json.dumps(data).encode('utf-8')
    
    # Create request
    req = urllib.request.Request(
        url,
        data=json_data,
        headers={'Content-Type': 'application/json'}
    )
    
    try:
        # Make request
        with urllib.request.urlopen(req) as response:
            response_data = json.loads(response.read().decode('utf-8'))
            return response_data.get("response", "")
    except urllib.error.HTTPError as e:
        error_body = e.read().decode('utf-8')
        raise Exception(f"Ollama API error: {e.code} - {error_body}")
    except urllib.error.URLError as e:
        raise Exception(f"Network error when calling Ollama: {str(e)}")
    except json.JSONDecodeError as e:
        raise Exception(f"Failed to parse Ollama response: {str(e)}")


def _call_openai(prompt: str, system: str = "") -> str:
    """Call OpenAI model via HTTP API"""
    api_key = os.environ.get("NEURON_OPENAI_API_KEY")
    if not api_key:
        raise Exception("NEURON_OPENAI_API_KEY environment variable not set")
    
    model = os.environ.get("NEURON_MODEL", "gpt-4o")
    url = "https://api.openai.com/v1/chat/completions"
    
    # Prepare messages
    messages = []
    if system:
        messages.append({"role": "system", "content": system})
    messages.append({"role": "user", "content": prompt})
    
    # Prepare request data
    data = {
        "model": model,
        "messages": messages,
        "temperature": 0.7
    }
    
    # Convert to JSON
    json_data = json.dumps(data).encode('utf-8')
    
    # Create request
    req = urllib.request.Request(
        url,
        data=json_data,
        headers={
            'Content-Type': 'application/json',
            'Authorization': f'Bearer {api_key}'
        }
    )
    
    try:
        # Make request
        with urllib.request.urlopen(req) as response:
            response_data = json.loads(response.read().decode('utf-8'))
            return response_data["choices"][0]["message"]["content"]
    except urllib.error.HTTPError as e:
        error_body = e.read().decode('utf-8')
        raise Exception(f"OpenAI API error: {e.code} - {error_body}")
    except urllib.error.URLError as e:
        raise Exception(f"Network error when calling OpenAI: {str(e)}")
    except (KeyError, IndexError, json.JSONDecodeError) as e:
        raise Exception(f"Failed to parse OpenAI response: {str(e)}")


def _call_anthropic(prompt: str, system: str = "") -> str:
    """Call Anthropic model via HTTP API"""
    api_key = os.environ.get("NEURON_ANTHROPIC_API_KEY")
    if not api_key:
        raise Exception("NEURON_ANTHROPIC_API_KEY environment variable not set")
    
    model = os.environ.get("NEURON_MODEL", "claude-sonnet-4-20250514")
    url = "https://api.anthropic.com/v1/messages"
    
    # Prepare messages
    messages = [{"role": "user", "content": prompt}]
    
    # Prepare request data
    data = {
        "model": model,
        "messages": messages,
        "max_tokens": 1024
    }
    
    if system:
        data["system"] = system
    
    # Convert to JSON
    json_data = json.dumps(data).encode('utf-8')
    
    # Create request
    req = urllib.request.Request(
        url,
        data=json_data,
        headers={
            'Content-Type': 'application/json',
            'x-api-key': api_key,
            'anthropic-version': '2023-06-01'
        }
    )
    
    try:
        # Make request
        with urllib.request.urlopen(req) as response:
            response_data = json.loads(response.read().decode('utf-8'))
            return response_data["content"][0]["text"]
    except urllib.error.HTTPError as e:
        error_body = e.read().decode('utf-8')
        raise Exception(f"Anthropic API error: {e.code} - {error_body}")
    except urllib.error.URLError as e:
        raise Exception(f"Network error when calling Anthropic: {str(e)}")
    except (KeyError, IndexError, json.JSONDecodeError) as e:
        raise Exception(f"Failed to parse Anthropic response: {str(e)}")


def _call_groq(prompt: str, system: str = "") -> str:
    """Call Groq model via HTTP API"""
    api_key = os.environ.get("NEURON_GROQ_API_KEY")
    if not api_key:
        raise Exception("NEURON_GROQ_API_KEY environment variable not set")
    
    model = os.environ.get("NEURON_MODEL", "llama3-70b-8192")
    url = "https://api.groq.com/openai/v1/chat/completions"
    
    # Prepare messages
    messages = []
    if system:
        messages.append({"role": "system", "content": system})
    messages.append({"role": "user", "content": prompt})
    
    # Prepare request data
    data = {
        "model": model,
        "messages": messages,
        "temperature": 0.7
    }
    
    # Convert to JSON
    json_data = json.dumps(data).encode('utf-8')
    
    # Create request
    req = urllib.request.Request(
        url,
        data=json_data,
        headers={
            'Content-Type': 'application/json',
            'Authorization': f'Bearer {api_key}'
        }
    )
    
    try:
        # Make request
        with urllib.request.urlopen(req) as response:
            response_data = json.loads(response.read().decode('utf-8'))
            return response_data["choices"][0]["message"]["content"]
    except urllib.error.HTTPError as e:
        error_body = e.read().decode('utf-8')
        raise Exception(f"Groq API error: {e.code} - {error_body}")
    except urllib.error.URLError as e:
        raise Exception(f"Network error when calling Groq: {str(e)}")
    except (KeyError, IndexError, json.JSONDecodeError) as e:
        raise Exception(f"Failed to parse Groq response: {str(e)}")