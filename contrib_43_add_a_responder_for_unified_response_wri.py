"""Add a Responder for unified response writing (fix for issue #43)"""

class Responder:
    """A unified responder for writing consistent HTTP-style responses."""

    def __init__(self):
        self.status_codes = {
            "ok": 200,
            "created": 201,
            "bad_request": 400,
            "unauthorized": 401,
            "forbidden": 403,
            "not_found": 404,
            "server_error": 500,
        }
        self.default_headers = {"Content-Type": "application/json"}

    def _status_line(self, status_code, status_text):
        return f"HTTP/1.1 {status_code} {status_text}\r\n"

    def _format_headers(self, headers):
        lines = [f"{key}: {value}" for key, value in headers.items()]
        return "\r\n".join(lines) + "\r\n\r\n"

    def _format_body(self, body):
        if body is None:
            return ""
        if isinstance(body, (dict, list)):
            import json
            return json.dumps(body)
        return str(body)

    def write(self, body=None, status="ok", headers=None, status_text=None):
        final_headers = self.default_headers.copy()
        if headers:
            final_headers.update(headers)

        if status in self.status_codes:
            code = self.status_codes[status]
        elif isinstance(status, int):
            code = status
        else:
            raise ValueError(f"Invalid status: {status}")

        text = status_text or status.upper()
        status_line = self._status_line(code, text)
        header_block = self._format_headers(final_headers)
        body_str = self._format_body(body)

        response = status_line + header_block + body_str
        return response.encode("utf-8")

