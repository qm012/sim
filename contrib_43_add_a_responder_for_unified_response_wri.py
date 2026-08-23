"""Add a Responder for unified response writing (fix for issue #43)"""

responses = []

def add_responder(function):
    responses.append(function)
    return function

def respond(message, *args, **kwargs):
    return [f(__name__, message, *args, **kwargs) for f in responses]

@add_responder
def default_responder(name, message, *args, **kwargs):
    return f"[{name}] {message}"

