import sys
import json
import urllib.request
import urllib.error
import os
from pathlib import Path


def run(inputs: dict) -> dict:
    """Code reviewer that analyzes code for bugs, improvements, and security issues"""
    try:
        # Extract inputs
        path = inputs.get('path')
        focus = inputs.get('focus', 'general')
        
        if not path:
            return {'error': 'Path is required', 'error_type': 'invalid_input'}
        
        # Check if path exists
        target_path = Path(path)
        if not target_path.exists():
            return {'error': f'Path not found: {path}', 'error_type': 'file_not_found'}
        
        # Read file content(s)
        files_content = {}
        files_reviewed = []
        
        if target_path.is_file():
            # Single file
            try:
                with open(target_path, 'r', encoding='utf-8') as f:
                    files_content[str(target_path)] = f.read()
                files_reviewed = [str(target_path)]
            except Exception as e:
                return {'error': f'Failed to read file {path}: {str(e)}', 'error_type': 'file_read_error'}
        else:
            # Directory - read up to 5 files recursively
            file_count = 0
            for root, dirs, files in os.walk(target_path):
                # Sort to ensure consistent ordering
                dirs.sort()
                files.sort()
                
                for file in files:
                    if file_count >= 5:
                        break
                    
                    # Skip hidden files and common non-code files
                    if file.startswith('.') or file.endswith(('.png', '.jpg', '.jpeg', '.gif', '.ico', '.pdf', '.zip', '.tar', '.gz')):
                        continue
                    
                    file_path = Path(root) / file
                    try:
                        with open(file_path, 'r', encoding='utf-8') as f:
                            files_content[str(file_path)] = f.read()
                        files_reviewed.append(str(file_path))
                        file_count += 1
                    except UnicodeDecodeError:
                        # Skip binary files
                        continue
                    except Exception as e:
                        return {'error': f'Failed to read file {file_path}: {str(e)}', 'error_type': 'file_read_error'}
                
                if file_count >= 5:
                    break
            
            if file_count == 0:
                return {'error': 'No readable files found in directory', 'error_type': 'file_not_found'}
        
        # Prepare code content for review
        code_context = ""
        for file_path, content in files_content.items():
            code_context += f"\n\n--- File: {file_path} ---\n{content}"
        
        # Prepare system prompt based on focus
        focus_prompts = {
            "security": "You are an expert code reviewer focused on security. Analyze the code and identify: security vulnerabilities, potential exploits, insecure practices, and compliance issues.",
            "performance": "You are an expert code reviewer focused on performance. Analyze the code and identify: performance bottlenecks, optimization opportunities, memory leaks, and efficiency improvements.",
            "readability": "You are an expert code reviewer focused on code quality and readability. Analyze the code and identify: code structure issues, naming conventions, documentation gaps, and maintainability concerns.",
            "general": "You are an expert code reviewer. Analyze the code and provide: bugs found, improvements, security issues, and a quality score out of 10."
        }
        
        system_prompt = focus_prompts.get(focus, focus_prompts["general"])
        
        # Prepare the prompt for Ollama
        prompt = f"""Analyze the following code and provide a detailed review:

Focus: {focus}

Code to review:
{code_context}

Please provide your analysis in JSON format with these exact keys:
- bugs: array of strings describing bugs found
- improvements: array of strings describing improvements suggested
- security_issues: array of strings describing security issues found
- quality_score: integer from 1-10 representing overall code quality

Be specific and actionable in your feedback."""

        # Call Ollama API using urllib.request
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
        try:
            with urllib.request.urlopen(req) as response:
                response_data = json.loads(response.read().decode('utf-8'))
        except urllib.error.URLError as e:
            return {'error': f'Ollama API error: {str(e)}', 'error_type': 'api_error'}
        except Exception as e:
            return {'error': f'API call failed: {str(e)}', 'error_type': 'api_error'}
        
        # Extract and parse the response
        if 'response' in response_data:
            try:
                # Try to parse the response as JSON
                review_result = json.loads(response_data['response'])
                
                # Ensure all required fields are present
                result = {
                    'bugs': review_result.get('bugs', []),
                    'improvements': review_result.get('improvements', []),
                    'security_issues': review_result.get('security_issues', []),
                    'quality_score': review_result.get('quality_score', 5),
                    'files_reviewed': files_reviewed
                }
                
                return result
            except json.JSONDecodeError:
                # If not valid JSON, create a fallback structure
                return {
                    'bugs': ['Unable to parse detailed review'],
                    'improvements': [response_data['response']],
                    'security_issues': [],
                    'quality_score': 5,
                    'files_reviewed': files_reviewed
                }
        else:
            # Fallback response structure
            return {
                'bugs': [],
                'improvements': ['Review completed but no detailed response available'],
                'security_issues': [],
                'quality_score': 5,
                'files_reviewed': files_reviewed
            }
            
    except Exception as e:
        return {'error': f'Code review failed: {str(e)}', 'error_type': 'review_failed'}


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