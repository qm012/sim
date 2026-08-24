"""Add a Responder for unified response writing (fix for issue #43)"""

import json
from typing import Any, Dict, Iterable, Optional, Union

class Responder:
    """Unified response writer for HTTP responses."""

    def __init__(self) -> None:
        self._status: int = 200
        self._headers: Dict[str, str] = {}
        self._body: bytes = b""
        self._started: bool = False

    def set_status(self, status: int) -> "Responder":
        """Set HTTP status code."""
        if self._started:
            raise RuntimeError("Response already started")
        self._status = status
        return self

    def set_header(self, key: str, value: str) -> "Responder":
        """Set a response header."""
        if self._started:
            raise RuntimeError("Response already started")
        self._headers[key] = value
        return self

    def set_content_type(self, content_type: str) -> "Responder":
        """Set Content-Type header."""
        return self.set_header("Content-Type", content_type)

    def write(self, data: Union[str, bytes]) -> "Responder":
        """Write data to response body."""
        if isinstance(data, str):
            data = data.encode("utf-8")
        self._body += data
        return self

    def write_json(self, data: Any) -> "Responder":
        """Write JSON data to response body."""
        self.set_content_type("application/json; charset=utf-8")
        return self.write(json.dumps(data))

    def start(self, send_headers: bool = True) -> "Responder":
        """Mark response as started (headers cannot be modified after this)."""
        self._started = True
        return self

    def to_wsgi(self) -> tuple[int, list[tuple[str, str]], Iterable[bytes]]:
        """Convert to WSGI-compatible response tuple."""
        return (
            self._status,
            list(self._headers.items()),
            [self._body] if self._body else [b""],
        )

    def to_asgi(self) -> dict:
        """Convert to ASGI response start event."""
        return {
            "type": "http.response.start",
            "status": self._status,
            "headers": [
                (k.encode("latin-1"), v.encode("latin-1"))
                for k, v in self._headers.items()
            ],
        }

    @property
    def body(self) -> bytes:
        return self._body

    @property
    def status(self) -> int:
        return self._status

    @property
    def headers(self) -> Dict[str, str]:
        return self._headers.copy()

