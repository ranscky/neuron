import sys
import json
import os
from github import Github, Auth


def run(inputs: dict) -> dict | list:
    """
    Interact with GitHub repositories using the GitHub API
    
    Args:
        inputs: dict with action, repo, and optional fields
        
    Returns:
        dict: Result based on the action performed
    """
    try:
        # Get action and repo from inputs
        action = inputs.get('action')
        repo_name = inputs.get('repo')
        
        if not action:
            return {'error': 'Action is required'}
        
        if not repo_name:
            return {'error': 'Repo is required'}
        
        # Validate action
        valid_actions = ['get_repo', 'list_issues', 'get_file', 'open_issue']
        if action not in valid_actions:
            return {'error': f'Invalid action. Must be one of: {", ".join(valid_actions)}'}
        
        # Get GitHub token from environment
        github_token = os.environ.get('GITHUB_TOKEN')
        if not github_token:
            return {'error': 'GITHUB_TOKEN environment variable not set'}
        
        # Initialize GitHub client
        auth = Auth.Token(github_token)
        g = Github(auth=auth)
        
        # Get repository
        try:
            repo = g.get_repo(repo_name)
        except Exception as e:
            return {'error': f'Repository not found: {str(e)}'}
        
        # Perform action
        if action == 'get_repo':
            return {
                'name': repo.name,
                'full_name': repo.full_name,
                'description': repo.description,
                'stars': repo.stargazers_count,
                'language': repo.language,
                'url': repo.html_url,
                'forks': repo.forks_count,
                'open_issues': repo.open_issues_count
            }
        
        elif action == 'list_issues':
            try:
                issues = repo.get_issues(state='open')
                issue_list = []
                for issue in issues:
                    issue_list.append({
                        'number': issue.number,
                        'title': issue.title,
                        'url': issue.html_url,
                        'state': issue.state,
                        'created_at': issue.created_at.isoformat()
                    })
                return issue_list
            except Exception as e:
                return {'error': f'Failed to list issues: {str(e)}'}
        
        elif action == 'get_file':
            path = inputs.get('path')
            if not path:
                return {'error': 'Path is required for get_file action'}
            
            try:
                file_content = repo.get_contents(path)
                if isinstance(file_content, list):
                    # Handle directory case - return first file or error
                    if len(file_content) > 0:
                        content_item = file_content[0]
                        return {
                            'path': path,
                            'content': content_item.decoded_content.decode('utf-8') if hasattr(content_item, 'decoded_content') else '',
                            'sha': getattr(content_item, 'sha', ''),
                            'size': getattr(content_item, 'size', 0)
                        }
                    else:
                        return {'error': 'Path is a directory with no files'}
                else:
                    # Handle single file case
                    return {
                        'path': path,
                        'content': file_content.decoded_content.decode('utf-8'),
                        'sha': file_content.sha,
                        'size': file_content.size
                    }
            except Exception as e:
                return {'error': f'File not found: {str(e)}'}
        
        elif action == 'open_issue':
            title = inputs.get('title')
            body = inputs.get('body', '')
            
            if not title:
                return {'error': 'Title is required for open_issue action'}
            
            try:
                issue = repo.create_issue(title=title, body=body)
                return {
                    'number': issue.number,
                    'title': issue.title,
                    'url': issue.html_url,
                    'state': issue.state
                }
            except Exception as e:
                return {'error': f'Failed to create issue: {str(e)}'}
        else:
            return {'error': f'Unsupported action: {action}'}
        
    except Exception as e:
        return {'error': f'Unexpected error: {str(e)}'}


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