import sys
import json
import os
from pypdf import PdfReader
import urllib.request
import urllib.error


def run(inputs: dict) -> dict:
    """Extract information from PDF and answer questions using Ollama"""
    try:
        # Extract inputs
        file_path = inputs.get('file_path')
        question = inputs.get('question')
        max_pages = inputs.get('max_pages', 50)
        
        if not file_path:
            return {'error': 'file_path is required'}
        
        if not question:
            return {'error': 'question is required'}
        
        # Check if file exists
        if not os.path.exists(file_path):
            return {'error': 'File not found', 'file_path': file_path}
        
        # Read PDF file
        try:
            reader = PdfReader(file_path)
            pages_read = min(len(reader.pages), max_pages)
            
            # Extract text from PDF
            text = ""
            for i in range(pages_read):
                page = reader.pages[i]
                text += page.extract_text() + "\n"
        except Exception as e:
            return {'error': f'Invalid PDF file: {str(e)}'}
        
        # Split text into chunks of 1000 characters
        chunk_size = 1000
        chunks = []
        for i in range(0, len(text), chunk_size):
            chunks.append(text[i:i + chunk_size])
        
        # Simple keyword matching to find relevant chunks
        question_lower = question.lower()
        relevant_chunks = []
        
        for chunk in chunks:
            # Count keyword matches
            score = sum(1 for word in question_lower.split() if word in chunk.lower())
            if score > 0:
                relevant_chunks.append((score, chunk))
        
        # Sort by relevance and take top chunks
        relevant_chunks.sort(reverse=True)
        top_chunks = [chunk for score, chunk in relevant_chunks[:5]]  # Take top 5 chunks
        
        if not top_chunks:
            # If no relevant chunks found, use first few chunks
            top_chunks = chunks[:3]
        
        # Prepare context for Ollama
        context = "\n\n".join(top_chunks)
        
        # Prepare the prompt for Ollama
        prompt = f"Context from PDF:\n{context}\n\nQuestion: {question}"
        
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
                'answer': answer,
                'pages_read': pages_read,
                'chunks_used': len(top_chunks)
            }
        except urllib.error.URLError as e:
            return {'error': f'Ollama API error: {str(e)}'}
        except Exception as e:
            return {'error': f'Ollama API error: {str(e)}'}
            
    except Exception as e:
        return {'error': f'Processing error: {str(e)}'}


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
        print(json.dumps({'error': f'Invalid JSON input: {str(e)}'}))
        sys.exit(1)
    except Exception as e:
        print(json.dumps({'error': f'Unexpected error: {str(e)}'}))
        sys.exit(1)