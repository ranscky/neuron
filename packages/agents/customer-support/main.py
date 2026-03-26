import sys
import json
import urllib.request
import urllib.error
from pathlib import Path


def run(inputs: dict) -> dict:
    """Handle customer support queries using Notion knowledge base"""
    try:
        # Extract inputs
        question = inputs.get('question')
        context = inputs.get('context', '')
        tone = inputs.get('tone', 'friendly')
        
        if not question:
            return {'error': 'Question is required', 'error_type': 'invalid_input'}
        
        # Load knowledge base context
        knowledge_context = load_knowledge_base()
        if not knowledge_context:
            knowledge_context = context
        
        # Generate response using Ollama
        response = generate_response(question, knowledge_context, tone)
        return response
        
    except Exception as e:
        return {'error': f'Processing failed: {str(e)}', 'error_type': 'processing_error'}


def load_knowledge_base() -> str:
    """Load cached Notion content if available"""
    try:
        cache_file = Path.home() / '.neuron' / 'notion-cache.json'
        
        if cache_file.exists():
            with open(cache_file, 'r') as f:
                cache_data = json.load(f)
            
            # Extract relevant information from cache
            context_parts = []
            for page in cache_data.get('pages', []):
                context_parts.append(f"Title: {page.get('title')}\nURL: {page.get('url')}")
            
            return "\n\n".join(context_parts)
        else:
            return ""
    except Exception:
        return ""


def generate_response(question: str, context: str, tone: str) -> dict:
    """Generate customer support response using Ollama"""
    try:
        # Prepare context information
        if context:
            full_context = f"Knowledge Base Context:\n{context}\n\nQuestion: {question}"
        else:
            full_context = question
        
        # Map tone to descriptive text
        tone_descriptions = {
            "friendly": "Be friendly and approachable",
            "professional": "Be professional and business-appropriate", 
            "formal": "Be formal and highly professional"
        }
        tone_desc = tone_descriptions.get(tone, "Be friendly and approachable")
        
        # Prepare the prompt for Ollama
        prompt = f"""Question: {question}

Context: {full_context}

Tone: {tone_desc}

You are a helpful customer support agent. Be concise, empathetic and solution-focused.
Provide a clear answer to the question based on the context provided.
If the context doesn't contain relevant information, provide general helpful guidance."""

        # Call Ollama API
        url = "http://localhost:11434/api/generate"
        data = {
            "model": "qwen3-coder:480b-cloud",
            "prompt": prompt,
            "system": "You are a helpful customer support agent. Be concise, empathetic and solution-focused.",
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
        
        # Generate suggested follow-ups (simple approach)
        suggested_followups = [
            f"How do I {question.lower().split()[0] if question.split() else 'do this'}?",
            f"What if I {question.lower().split()[0] if question.split() else 'do this'} incorrectly?",
            f"Are there alternatives to {question.lower().split()[-1] if question.split() else 'this'}?"
        ]
        
        return {
            'answer': answer,
            'confidence': 0.85,  # Default confidence
            'suggested_followups': suggested_followups
        }
            
    except urllib.error.URLError as e:
        raise Exception(f'Ollama API error: {str(e)}')
    except Exception as e:
        raise Exception(f'Response generation error: {str(e)}')


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