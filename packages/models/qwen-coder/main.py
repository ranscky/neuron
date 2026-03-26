import sys
import json
import urllib.request
import urllib.error


def run(inputs: dict) -> dict:
    """Send request to Ollama Qwen3 Coder model and return response"""
    try:
        # Extract inputs
        prompt = inputs.get('prompt')
        system = inputs.get('system', 'You are an expert programmer')
        temperature = inputs.get('temperature', 0.7)
        max_tokens = inputs.get('max_tokens', 2048)
        
        if not prompt:
            return {
                'error': 'Prompt is required',
                'error_type': 'invalid_request'
            }
        
        # Combine system prompt and user prompt
        full_prompt = f"{system}\n\n{prompt}" if system else prompt
        
        # Prepare request data
        url = "http://localhost:11434/api/generate"
        data = {
            "model": "qwen3-coder:480b-cloud",
            "prompt": full_prompt,
            "stream": False,
            "options": {
                "temperature": temperature,
                "num_predict": max_tokens
            }
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
        
        # Return structured response
        return {
            'response': response_data.get('response', ''),
            'model': response_data.get('model', 'qwen3-coder:480b-cloud'),
            'tokens_used': response_data.get('prompt_eval_count', 0) + response_data.get('eval_count', 0)
        }
        
    except urllib.error.URLError as e:
        return {
            'error': f'Ollama API error: {str(e)}',
            'error_type': 'api_error'
        }
    except Exception as e:
        return {
            'error': f'Processing error: {str(e)}',
            'error_type': 'invalid_request'
        }


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
        error_result = {
            'error': f'Invalid JSON input: {str(e)}',
            'error_type': 'invalid_request'
        }
        print(json.dumps(error_result))
        sys.exit(1)
    except Exception as e:
        error_result = {
            'error': f'Unexpected error: {str(e)}',
            'error_type': 'invalid_request'
        }
        print(json.dumps(error_result))
        sys.exit(1)