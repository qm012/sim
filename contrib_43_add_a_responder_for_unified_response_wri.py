"""Add a Responder for unified response writing (fix for issue #43)"""

class UnifiedResponder:
    def __init__(self):
        self.responses = [
            {'condition': lambda q: 'error' in q.lower(), 'response': {'type': 'error', 'message': 'An error occurred'}},
            {'condition': lambda q: 'status' in q.lower(), 'response': {'type': 'status', 'message': 'System operational'}},
            {'condition': lambda q: 'help' in q.lower(), 'response': {'type': 'help', 'message': 'Available commands: help, status, error'}},
        ]
    
    def generate_response(self, query):
        for rule in self.responses:
            if rule['condition'](query):
                return rule['response']
        return {'type': 'generic', 'message': 'Request not recognized'}

